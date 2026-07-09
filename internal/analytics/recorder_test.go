package analytics

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/appmire/charging/internal/store"
)

type memSink struct {
	mu sync.Mutex
	n  int
}

func (m *memSink) InsertEvents(_ context.Context, evs []store.AnalyticsEvent) error {
	m.mu.Lock()
	m.n += len(evs)
	m.mu.Unlock()
	return nil
}
func (m *memSink) count() int { m.mu.Lock(); defer m.mu.Unlock(); return m.n }

func testLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRecorderFlushes(t *testing.T) {
	sink := &memSink{}
	r := New(sink, testLog(), 100)
	r.flushIv = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	for i := 0; i < 50; i++ {
		r.Record(store.AnalyticsEvent{Event: "t"})
	}
	// wait for a flush tick
	deadline := time.Now().Add(2 * time.Second)
	for sink.count() < 50 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if sink.count() != 50 {
		t.Fatalf("flushed %d, want 50", sink.count())
	}
}

func TestRecorderNeverBlocks(t *testing.T) {
	// Tiny buffer, no drainer running: Record must not block, just drop.
	r := New(&memSink{}, testLog(), 1)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			r.Record(store.AnalyticsEvent{Event: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Record blocked when buffer was full")
	}
}

func TestNilRecorderSafe(t *testing.T) {
	var r *Recorder
	r.Record(store.AnalyticsEvent{Event: "x"}) // must not panic
}
