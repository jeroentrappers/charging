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
	"unicode"

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
	Regions          []string            `json:"regions"`
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

// regionKey derives the per-region bucket key from a charger's country and
// postal code: "C-P" where C is the country (or "XX" if empty) and P is the
// first 2 alphanumeric characters of the postal code uppercased (or "XX" if
// there are fewer than 2). e.g. ("NL","1011 AB")->"NL-10", ("FR","75001")->"FR-75",
// ("BE","")->"BE-XX", ("","x")->"XX-XX".
func regionKey(country, postal string) string {
	c := country
	if c == "" {
		c = "XX"
	}
	var p []rune
	for _, r := range postal {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			p = append(p, unicode.ToUpper(r))
			if len(p) == 2 {
				break
			}
		}
	}
	if len(p) < 2 {
		return c + "-XX"
	}
	return c + "-" + string(p)
}

// GenerateFull rebuilds the normalized, GeoJSON, and OCPI dumps, streaming from
// the DB and splitting output into per-region files so only one region's rows
// are ever held in memory at once.
func (s *Snapshotter) GenerateFull(ctx context.Context) error {
	now := s.now()
	if err := s.ensureDir(); err != nil {
		return err
	}

	files, regions, total, priced, err := s.writeRegions(now, func(yield func(store.ExportCharger) error) error {
		return s.Store.ExportStream(ctx, yield)
	})
	if err != nil {
		return err
	}

	// Prune stale per-region files left over from a previous run (regions that
	// disappeared, or renamed buckets), so the published set matches the manifest.
	if err := s.pruneRegions(files); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manifest.Files == nil {
		s.manifest.Files = map[string]FileInfo{}
	}
	// Drop old full-export (non-availability) file entries, then add the new set.
	for name, info := range s.manifest.Files {
		if info.Kind != "availability-delta" {
			delete(s.manifest.Files, name)
		}
	}
	for name, info := range files {
		s.manifest.Files[name] = info
	}
	s.manifest.GeneratedAt = now
	s.manifest.NextFullRefresh = now.Add(s.FullEvery)
	s.manifest.Chargers = total
	s.manifest.PricedChargers = priced
	s.manifest.Regions = regions
	return s.writeManifestLocked()
}

// regionSubdirs are the subdirectories holding the per-region export files.
var regionSubdirs = []string{"ndjson", "geojson", "ocpi"}

// writeRegions consumes a stream of rows (already ordered so a region's rows
// are contiguous) and writes per-region ndjson/geojson/ocpi files, returning
// the FileInfos, total + priced counts, and the sorted region list. Only one
// region's rows are held in memory at a time.
func (s *Snapshotter) writeRegions(now time.Time, stream func(func(store.ExportCharger) error) error) (files map[string]FileInfo, regions []string, total, priced int, err error) {
	files = map[string]FileInfo{}
	regionSet := map[string]struct{}{}

	var curKey string
	var buf []store.ExportCharger

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		if err := s.writeRegionFiles(curKey, buf, now, files); err != nil {
			return err
		}
		regionSet[curKey] = struct{}{}
		buf = buf[:0]
		return nil
	}

	streamErr := stream(func(e store.ExportCharger) error {
		total++
		if e.PriceEUR != nil {
			priced++
		}
		key := regionKey(e.Country, e.PostalCode)
		if len(buf) > 0 && key != curKey {
			if err := flush(); err != nil {
				return err
			}
		}
		curKey = key
		buf = append(buf, e)
		return nil
	})
	if streamErr != nil {
		return nil, nil, 0, 0, streamErr
	}
	if err := flush(); err != nil {
		return nil, nil, 0, 0, err
	}

	regions = make([]string, 0, len(regionSet))
	for k := range regionSet {
		regions = append(regions, k)
	}
	sort.Strings(regions)
	return files, regions, total, priced, nil
}

// writeRegionFiles writes the four per-region files for one region's rows and
// records their FileInfos (keyed by relative name) into files.
func (s *Snapshotter) writeRegionFiles(region string, rows []store.ExportCharger, now time.Time, files map[string]FileInfo) error {
	locs, tariffs := BuildOCPI(rows, now)
	type entry struct {
		name  string
		kind  string
		write func(io.Writer) error
	}
	entries := []entry{
		{"ndjson/" + region + ".ndjson", "normalized-ndjson", func(w io.Writer) error { return WriteNDJSON(w, rows) }},
		{"geojson/" + region + ".geojson", "geojson", func(w io.Writer) error { return WriteGeoJSON(w, rows) }},
		{"ocpi/" + region + "-locations.json", "ocpi-locations", func(w io.Writer) error { return WriteOCPILocations(w, locs, now) }},
		{"ocpi/" + region + "-tariffs.json", "ocpi-tariffs", func(w io.Writer) error { return WriteOCPITariffs(w, tariffs, now) }},
	}
	for _, e := range entries {
		info, err := s.writeAtomic(e.name, e.write, now)
		if err != nil {
			return err
		}
		info.Kind = e.kind
		files[e.name] = info
	}
	return nil
}

// pruneRegions removes any *.ndjson/*.geojson/*.json files under the region
// subdirs that aren't part of the freshly-written set.
func (s *Snapshotter) pruneRegions(keep map[string]FileInfo) error {
	for _, sub := range regionSubdirs {
		dir := filepath.Join(s.Dir, sub)
		ents, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, ent := range ents {
			if ent.IsDir() {
				continue
			}
			name := ent.Name()
			ext := filepath.Ext(name)
			if ext != ".ndjson" && ext != ".geojson" && ext != ".json" {
				continue
			}
			rel := sub + "/" + name
			if _, ok := keep[rel]; ok {
				continue
			}
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
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
	for _, sub := range regionSubdirs {
		if err := os.MkdirAll(filepath.Join(s.Dir, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
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

// FileNames returns the static published file names in a stable order (for
// docs/tests). The per-region full-export files (ndjson/<R>.ndjson,
// geojson/<R>.geojson, ocpi/<R>-{locations,tariffs}.json) are dynamic and
// enumerated via the manifest's Regions + Files instead.
func FileNames() []string {
	names := []string{"index.json", "availability.json"}
	sort.Strings(names)
	return names
}
