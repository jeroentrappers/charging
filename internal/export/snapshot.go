package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/appmire/charging/internal/store"
)

// License/attribution carried in every manifest. The dataset is derived from
// open AFIR / transportdata.be data; we re-publish it under the same open terms
// and pass through operator attribution.
const (
	datasetLicense     = "ODbL-1.0"
	datasetAttribution = "Derived from open EV charging data published via the Belgian National Access Point (transportdata.be) under AFIR Article 20; © the respective charge point operators."
)

// FileInfo describes one published file in the manifest.
type FileInfo struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Manifest is index.json: what's published, when, how big, and under what terms.
type Manifest struct {
	GeneratedAt      time.Time           `json:"generated_at"`
	AvailGeneratedAt time.Time           `json:"availability_generated_at"`
	NextFullRefresh  time.Time           `json:"next_full_refresh"`
	NextAvailRefresh time.Time           `json:"next_availability_refresh"`
	Chargers         int                 `json:"chargers"`
	PricedChargers   int                 `json:"priced_chargers"`
	License          string              `json:"license"`
	Attribution      string              `json:"attribution"`
	Files            map[string]FileInfo `json:"files"`
}

// Snapshotter regenerates the static dumps on a schedule and keeps index.json
// in sync. Writes are atomic (temp file + rename) so readers never see a
// partial file.
type Snapshotter struct {
	Store      *store.Store
	Dir        string
	Log        *slog.Logger
	FullEvery  time.Duration
	AvailEvery time.Duration
	Now        func() time.Time // injectable for tests

	mu       sync.Mutex
	manifest Manifest
}

func (s *Snapshotter) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Run generates an initial full + availability snapshot, then refreshes each on
// its own ticker until ctx is cancelled.
func (s *Snapshotter) Run(ctx context.Context) {
	if err := s.GenerateFull(ctx); err != nil {
		s.Log.Error("export: initial full snapshot", "err", err)
	}
	if err := s.GenerateAvailability(ctx); err != nil {
		s.Log.Error("export: initial availability snapshot", "err", err)
	}
	full := time.NewTicker(s.FullEvery)
	avail := time.NewTicker(s.AvailEvery)
	defer full.Stop()
	defer avail.Stop()
	s.Log.Info("export snapshotter started", "dir", s.Dir, "full_every", s.FullEvery, "avail_every", s.AvailEvery)
	for {
		select {
		case <-ctx.Done():
			return
		case <-full.C:
			if err := s.GenerateFull(ctx); err != nil {
				s.Log.Error("export: full snapshot", "err", err)
			}
		case <-avail.C:
			if err := s.GenerateAvailability(ctx); err != nil {
				s.Log.Error("export: availability snapshot", "err", err)
			}
		}
	}
}

// GenerateFull rebuilds the normalized, GeoJSON, and OCPI dumps.
func (s *Snapshotter) GenerateFull(ctx context.Context) error {
	rows, err := s.Store.ExportAll(ctx)
	if err != nil {
		return err
	}
	now := s.now()

	if err := s.ensureDir(); err != nil {
		return err
	}

	files := map[string]func(io.Writer) error{
		"chargers.ndjson":     func(w io.Writer) error { return WriteNDJSON(w, rows) },
		"chargers.geojson":    func(w io.Writer) error { return WriteGeoJSON(w, rows) },
		"ocpi/locations.json": nil, // built below (shares OCPI build)
		"ocpi/tariffs.json":   nil,
	}
	locs, tariffs := BuildOCPI(rows, now)
	files["ocpi/locations.json"] = func(w io.Writer) error { return WriteOCPILocations(w, locs, now) }
	files["ocpi/tariffs.json"] = func(w io.Writer) error { return WriteOCPITariffs(w, tariffs, now) }

	kinds := map[string]string{
		"chargers.ndjson":     "normalized-ndjson",
		"chargers.geojson":    "geojson",
		"ocpi/locations.json": "ocpi-locations",
		"ocpi/tariffs.json":   "ocpi-tariffs",
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest.Files == nil {
		s.manifest.Files = map[string]FileInfo{}
	}
	for name, writeFn := range files {
		info, err := s.writeAtomic(name, writeFn, now)
		if err != nil {
			return err
		}
		info.Kind = kinds[name]
		s.manifest.Files[name] = info
	}

	priced := 0
	for i := range rows {
		if rows[i].PriceEUR != nil {
			priced++
		}
	}
	s.manifest.GeneratedAt = now
	s.manifest.NextFullRefresh = now.Add(s.FullEvery)
	s.manifest.Chargers = len(rows)
	s.manifest.PricedChargers = priced
	return s.writeManifestLocked()
}

// GenerateAvailability rebuilds the small, frequently-rotated status delta.
func (s *Snapshotter) GenerateAvailability(ctx context.Context) error {
	avail, err := s.Store.ExportAvailability(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	if err := s.ensureDir(); err != nil {
		return err
	}

	payload := struct {
		GeneratedAt time.Time                    `json:"generated_at"`
		Count       int                          `json:"count"`
		Chargers    []store.AvailabilitySnapshot `json:"chargers"`
	}{GeneratedAt: now, Count: len(avail), Chargers: avail}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest.Files == nil {
		s.manifest.Files = map[string]FileInfo{}
	}
	info, err := s.writeAtomic("availability.json", func(w io.Writer) error { return writeJSON(w, payload) }, now)
	if err != nil {
		return err
	}
	info.Kind = "availability-delta"
	s.manifest.Files["availability.json"] = info
	s.manifest.AvailGeneratedAt = now
	s.manifest.NextAvailRefresh = now.Add(s.AvailEvery)
	return s.writeManifestLocked()
}

func (s *Snapshotter) ensureDir() error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(s.Dir, "ocpi"), 0o755)
}

// writeAtomic writes via a temp file in the same directory, hashing as it goes,
// then renames over the target so readers never observe a partial file.
func (s *Snapshotter) writeAtomic(name string, write func(io.Writer) error, now time.Time) (FileInfo, error) {
	final := filepath.Join(s.Dir, filepath.FromSlash(name))
	tmp, err := os.CreateTemp(filepath.Dir(final), ".tmp-*")
	if err != nil {
		return FileInfo{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	h := sha256.New()
	cw := &countWriter{}
	if err := write(io.MultiWriter(tmp, h, cw)); err != nil {
		tmp.Close()
		return FileInfo{}, err
	}
	if err := tmp.Close(); err != nil {
		return FileInfo{}, err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return FileInfo{}, err
	}
	return FileInfo{Name: name, Bytes: cw.n, SHA256: hex.EncodeToString(h.Sum(nil)), UpdatedAt: now}, nil
}

func (s *Snapshotter) writeManifestLocked() error {
	m := s.manifest
	m.License = datasetLicense
	m.Attribution = datasetAttribution
	// Stable ordering is handled by the map in JSON output; copy is fine.
	_, err := s.writeAtomicNoManifest("index.json", func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	})
	return err
}

// writeAtomicNoManifest is writeAtomic without recording into the manifest
// (used for index.json itself).
func (s *Snapshotter) writeAtomicNoManifest(name string, write func(io.Writer) error) (FileInfo, error) {
	return s.writeAtomic(name, write, s.now())
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// FileNames returns the published file names in a stable order (for docs/tests).
func FileNames() []string {
	names := []string{"index.json", "chargers.ndjson", "chargers.geojson", "ocpi/locations.json", "ocpi/tariffs.json", "availability.json"}
	sort.Strings(names)
	return names
}
