// Package analytics provides a non-blocking, first-party event recorder. Events
// are buffered in a bounded channel and flushed to the store in batches by a
// background goroutine; if the buffer is full, events are dropped rather than
// ever blocking or failing a request. Nothing here touches raw PII — callers
// pass already-anonymized hashes.
package analytics

import (
	"context"
	"log/slog"
	"time"

	"github.com/appmire/charging/internal/store"
)

// Sink is the persistence dependency (satisfied by *store.Store).
type Sink interface {
	InsertEvents(ctx context.Context, evs []store.AnalyticsEvent) error
}

// Recorder buffers events and flushes them in batches.
type Recorder struct {
	ch      chan store.AnalyticsEvent
	sink    Sink
	log     *slog.Logger
	flushN  int
	flushIv time.Duration
	dropped uint64
}

// New returns a recorder with a bounded buffer. bufSize events may be queued
// before Record starts dropping. A nil recorder is safe: Record is a no-op.
func New(sink Sink, log *slog.Logger, bufSize int) *Recorder {
	if bufSize <= 0 {
		bufSize = 4096
	}
	return &Recorder{
		ch:      make(chan store.AnalyticsEvent, bufSize),
		sink:    sink,
		log:     log,
		flushN:  200,
		flushIv: 5 * time.Second,
	}
}

// Record enqueues an event without blocking. If the buffer is full the event is
// dropped (counted) — the request path is never slowed. Safe on a nil Recorder.
func (r *Recorder) Record(e store.AnalyticsEvent) {
	if r == nil {
		return
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	select {
	case r.ch <- e:
	default:
		r.dropped++ // buffer full: shed load rather than block
	}
}

// Run drains the buffer, flushing on a size or time trigger, until ctx is done.
func (r *Recorder) Run(ctx context.Context) {
	if r == nil {
		return
	}
	batch := make([]store.AnalyticsEvent, 0, r.flushN)
	t := time.NewTicker(r.flushIv)
	defer t.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := r.sink.InsertEvents(fctx, batch); err != nil {
			r.log.Warn("analytics: flush", "err", err, "n", len(batch))
		}
		cancel()
		batch = batch[:0]
	}
	for {
		select {
		case <-ctx.Done():
			// Drain what's buffered, then stop.
			for {
				select {
				case e := <-r.ch:
					batch = append(batch, e)
					if len(batch) >= r.flushN {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case e := <-r.ch:
			batch = append(batch, e)
			if len(batch) >= r.flushN {
				flush()
			}
		case <-t.C:
			flush()
			if r.dropped > 0 {
				r.log.Warn("analytics: dropped events (buffer full)", "dropped", r.dropped)
				r.dropped = 0
			}
		}
	}
}
