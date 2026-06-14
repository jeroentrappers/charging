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
	"time"
)

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

// RunSpoolWorkers starts n workers draining the spool until ctx is cancelled.
func (e *Engine) RunSpoolWorkers(ctx context.Context, dir string, n int) {
	if n < 1 {
		n = 1
	}
	for _, sub := range []string{"incoming", "processing", "failed"} {
		if err := os.MkdirAll(spoolSub(dir, sub), 0o755); err != nil {
			e.Log.Error("mobilithek spool: mkdir", "dir", sub, "err", err)
		}
	}
	e.recoverProcessing(dir) // re-queue anything a previous crash left mid-flight
	for i := 0; i < n; i++ {
		go e.spoolWorker(ctx, dir)
	}
	e.Log.Info("mobilithek spool workers started", "n", n, "dir", dir)
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

func (e *Engine) spoolWorker(ctx context.Context, dir string) {
	inc, proc, failed := spoolSub(dir, "incoming"), spoolSub(dir, "processing"), spoolSub(dir, "failed")
	for {
		if ctx.Err() != nil {
			return
		}
		name, ok := claimOldest(inc, proc)
		if !ok {
			select {
			case <-ctx.Done():
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
