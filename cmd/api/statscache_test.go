package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestStatsCacheFreshHit(t *testing.T) {
	c := newStatsCache()
	var calls atomic.Int32
	fetch := func(ctx context.Context) (any, error) { calls.Add(1); return "v1", nil }

	for range 3 {
		v, err := c.get(context.Background(), "k", time.Minute, fetch)
		if err != nil || v != "v1" {
			t.Fatalf("got %v, %v", v, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("fetch called %d times, want 1 (fresh hits)", calls.Load())
	}
}

func TestStatsCacheStaleServesOldAndRefreshes(t *testing.T) {
	c := newStatsCache()
	c.put("k", "old")
	c.entries["k"].fetched = time.Now().Add(-time.Hour) // force stale

	done := make(chan struct{})
	fetch := func(ctx context.Context) (any, error) { defer close(done); return "new", nil }

	v, err := c.get(context.Background(), "k", time.Minute, fetch)
	if err != nil || v != "old" {
		t.Fatalf("stale hit should serve old value immediately, got %v, %v", v, err)
	}
	<-done // background refresh ran
	// Poll until the refresh landed (it updates after fetch returns).
	deadline := time.After(2 * time.Second)
	for {
		v, _ := c.get(context.Background(), "k", time.Minute, nil)
		if v == "new" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("refresh never landed, still %v", v)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestStatsCacheRefreshErrorKeepsStale(t *testing.T) {
	c := newStatsCache()
	c.put("k", "old")
	c.entries["k"].fetched = time.Now().Add(-time.Hour)

	done := make(chan struct{})
	fetch := func(ctx context.Context) (any, error) { defer close(done); return nil, errors.New("db down") }

	if v, _ := c.get(context.Background(), "k", time.Minute, fetch); v != "old" {
		t.Fatalf("want stale value during failed refresh, got %v", v)
	}
	<-done
	// Still serves the old value, and the entry is retryable (refreshing reset).
	if v, _ := c.get(context.Background(), "k", time.Minute, func(ctx context.Context) (any, error) { return nil, errors.New("x") }); v != "old" {
		t.Fatal("stale value lost after failed refresh")
	}
}

func TestStatsCacheColdFetchError(t *testing.T) {
	c := newStatsCache()
	_, err := c.get(context.Background(), "k", time.Minute, func(ctx context.Context) (any, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("cold fetch error must propagate")
	}
}
