package ingest

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// keyedMutex provides per-key mutual exclusion (one lock per CPO id). lock(key)
// returns the unlock func. Per-key mutexes live for the process lifetime —
// bounded by the number of CPOs (dozens), so no eviction needed.
type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*sync.Mutex
}

func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = make(map[string]*sync.Mutex)
	}
	mu := k.m[key]
	if mu == nil {
		mu = &sync.Mutex{}
		k.m[key] = mu
	}
	k.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

// Durable push spool. The webhook handler enqueues each received push as a file;
// independent worker(s) drain it into the DB. This decouples receipt (fast +
// durable) from DB throughput, bounds memory, survives restarts, and lets us
// quarantine anything we can't handle. Layout under the spool dir:
//
//	incoming/    pending pushes, named <unixnano>-<hash>.<ext> (sorts = FIFO)
//	processing/  claimed by a worker (recovered to incoming on restart)
//	failed/      "to investigate": ingest errors + unhandled payloads, each with
//	             a <name>.reason sidecar
//
// Snapshot pushes are self-superseding, so losing throughput just means the next
// push catches up; nothing is lost as long as the file was written before 202.

// ErrSpoolFull signals backpressure: too many pending pushes, shed the request.
var ErrSpoolFull = errors.New("mobilithek spool full")

// maxSpoolDepth caps the incoming backlog; beyond it the handler returns 503 so
// the broker retries later instead of us buffering unboundedly.
const maxSpoolDepth = 20000

func spoolSub(dir, sub string) string { return filepath.Join(dir, sub) }

// SpoolPush durably enqueues a push body (already de-gzipped). Returns
// ErrSpoolFull when the backlog is too deep.
func SpoolPush(dir string, body []byte) error {
	inc := spoolSub(dir, "incoming")
	if err := os.MkdirAll(inc, 0o755); err != nil {
		return err
	}
	if ents, err := os.ReadDir(inc); err == nil && len(ents) >= maxSpoolDepth {
		return ErrSpoolFull
	}
	ext := "json"
	if firstNonSpace(body) == '<' {
		ext = "xml"
	}
	h := fnv.New64a()
	_, _ = h.Write(body)
	name := fmt.Sprintf("%020d-%016x.%s", time.Now().UnixNano(), h.Sum64(), ext)
	// Atomic publish: write to a hidden temp, then rename into place so a worker
	// never reads a partial file.
	tmp := filepath.Join(inc, "."+name+".tmp")
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(inc, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func firstNonSpace(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return c
		}
	}
	return 0
}

// spoolPerWorker is the backlog (incoming files) per worker before scaling up.
const spoolPerWorker = 80

// RunSpoolWorkers starts an autoscaling drainer: between minW and maxW workers,
// sized every 2s to the backlog so we keep up under bursts but never exceed maxW
// concurrent ingests (the cap that protects the DB). Per-CPO locking (mobLocks)
// lets the workers run different operators in parallel.
func (e *Engine) RunSpoolWorkers(ctx context.Context, dir string, minW, maxW int) {
	if minW < 1 {
		minW = 1
	}
	if maxW < minW {
		maxW = minW
	}
	for _, sub := range []string{"incoming", "processing", "failed"} {
		if err := os.MkdirAll(spoolSub(dir, sub), 0o755); err != nil {
			e.Log.Error("mobilithek spool: mkdir", "dir", sub, "err", err)
		}
	}
	e.recoverProcessing(dir) // re-queue anything a previous crash left mid-flight
	go e.spoolController(ctx, dir, minW, maxW)
	e.Log.Info("mobilithek spool autoscaler started", "min", minW, "max", maxW, "dir", dir)
}

// spoolController owns the worker set and resizes it to the backlog. Only this
// goroutine mutates the worker list, so it needs no locking.
func (e *Engine) spoolController(ctx context.Context, dir string, minW, maxW int) {
	var stops []chan struct{}
	add := func() {
		s := make(chan struct{})
		stops = append(stops, s)
		go e.spoolWorker(ctx, dir, s)
	}
	remove := func() {
		if len(stops) == 0 {
			return
		}
		close(stops[len(stops)-1]) // worker exits after its current item
		stops = stops[:len(stops)-1]
	}
	for i := 0; i < minW; i++ {
		add()
	}

	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	last := minW
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			depth := dirCount(spoolSub(dir, "incoming"))
			want := depth/spoolPerWorker + 1
			if want < minW {
				want = minW
			}
			if want > maxW {
				want = maxW
			}
			for len(stops) < want {
				add()
			}
			for len(stops) > want {
				remove()
			}
			if want != last {
				e.Log.Info("mobilithek spool autoscaled", "workers", want, "backlog", depth)
				last = want
			}
		}
	}
}

func dirCount(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, ent := range ents {
		if !strings.HasPrefix(ent.Name(), ".") {
			n++
		}
	}
	return n
}

func (e *Engine) recoverProcessing(dir string) {
	proc, inc := spoolSub(dir, "processing"), spoolSub(dir, "incoming")
	ents, err := os.ReadDir(proc)
	if err != nil {
		return
	}
	for _, ent := range ents {
		_ = os.Rename(filepath.Join(proc, ent.Name()), filepath.Join(inc, ent.Name()))
	}
	if len(ents) > 0 {
		e.Log.Info("mobilithek spool: recovered in-flight pushes", "n", len(ents))
	}
}

func (e *Engine) spoolWorker(ctx context.Context, dir string, stop <-chan struct{}) {
	inc, proc, failed := spoolSub(dir, "incoming"), spoolSub(dir, "processing"), spoolSub(dir, "failed")
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop: // scaled down
			return
		default:
		}
		name, ok := claimOldest(inc, proc)
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-time.After(time.Second):
			}
			continue
		}
		path := filepath.Join(proc, name)
		body, err := os.ReadFile(path)
		if err != nil {
			_ = os.Remove(path)
			continue
		}
		kind, _, ierr := e.IngestMobilithekPush(ctx, body)
		switch {
		case ierr != nil:
			e.toFailed(path, failed, name, "ingest error: "+ierr.Error())
		case kind == "":
			e.toFailed(path, failed, name, "unhandled: payload matched no known AFIR publication")
		default:
			_ = os.Remove(path) // done
		}
	}
}

// claimOldest atomically moves the oldest incoming file to processing/ and
// returns its name. The rename is the lock: with multiple workers, the loser
// gets ENOENT and tries the next.
func claimOldest(inc, proc string) (string, bool) {
	ents, err := os.ReadDir(inc)
	if err != nil {
		return "", false
	}
	names := make([]string, 0, len(ents))
	for _, ent := range ents {
		n := ent.Name()
		if ent.IsDir() || strings.HasPrefix(n, ".") {
			continue // skip temp files
		}
		names = append(names, n)
	}
	sort.Strings(names) // unixnano prefix → FIFO
	for _, n := range names {
		if os.Rename(filepath.Join(inc, n), filepath.Join(proc, n)) == nil {
			return n, true
		}
	}
	return "", false
}

// toFailed quarantines a push for investigation, with a reason sidecar.
func (e *Engine) toFailed(procPath, failedDir, name, reason string) {
	dst := filepath.Join(failedDir, name)
	if err := os.Rename(procPath, dst); err != nil {
		_ = os.Remove(procPath)
		return
	}
	_ = os.WriteFile(dst+".reason", []byte(time.Now().UTC().Format(time.RFC3339)+" "+reason+"\n"), 0o644)
	e.Log.Warn("mobilithek push quarantined", "file", name, "reason", reason)
}
