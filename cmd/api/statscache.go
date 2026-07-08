package main

import (
	"context"
	"sync"
	"time"
)

// statsCache memoizes the slow /stats/* analytics queries with
// stale-while-revalidate semantics: a fresh entry is served directly; a stale
// entry is served immediately while ONE background refresh recomputes it; only
// a cold key blocks on the query. The insights numbers move on ingest cadence
// (minutes to daily), so serving a slightly stale aggregate is free accuracy-
// wise and turns the ~8s price-trend scan into a hit for every visitor.
type statsCache struct {
	mu      sync.Mutex
	entries map[string]*statsEntry
}

type statsEntry struct {
	val        any
	fetched    time.Time
	refreshing bool
}

// maxStatsEntries bounds the key space (params are user-controlled, e.g.
// ?limit=): past the cap the oldest entry is evicted.
const maxStatsEntries = 256

func newStatsCache() *statsCache {
	return &statsCache{entries: map[string]*statsEntry{}}
}

// get returns the cached value for key, refreshing per the policy above.
// fetch runs with a background context when triggered asynchronously, so a
// refresh isn't cancelled when the triggering request disconnects.
func (c *statsCache) get(ctx context.Context, key string, ttl time.Duration, fetch func(ctx context.Context) (any, error)) (any, error) {
	c.mu.Lock()
	e, ok := c.entries[key]
	if ok && time.Since(e.fetched) < ttl { // fresh hit
		v := e.val
		c.mu.Unlock()
		return v, nil
	}
	if ok { // stale hit: serve it, refresh once in the background
		v := e.val
		if !e.refreshing {
			e.refreshing = true
			go c.refresh(key, fetch)
		}
		c.mu.Unlock()
		return v, nil
	}
	c.mu.Unlock()

	// Cold key: compute synchronously so the caller gets real data.
	v, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	c.put(key, v)
	return v, nil
}

func (c *statsCache) refresh(key string, fetch func(ctx context.Context) (any, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	v, err := fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[key]; ok {
		e.refreshing = false
		if err == nil {
			e.val, e.fetched = v, time.Now()
		}
		// On error keep serving the stale value; the next stale hit retries.
	}
}

func (c *statsCache) put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxStatsEntries {
		oldest, oldestAt := "", time.Now()
		for k, e := range c.entries {
			if e.fetched.Before(oldestAt) {
				oldest, oldestAt = k, e.fetched
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = &statsEntry{val: v, fetched: time.Now()}
}

// warmStats precomputes the stats keys the insights page requests with its
// default parameters, so post-deploy first loads hit a warm cache.
func (s *server) warmStats(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for _, in := range []struct{ by string }{{"city"}, {"postal"}} {
		if _, err := s.opStatsRegions(ctx, &regionsIn{By: in.by}); err != nil {
			s.log.Warn("stats warmup", "key", "regions/"+in.by, "err", err)
		}
	}
	if _, err := s.opStatsOverview(ctx, nil); err != nil {
		s.log.Warn("stats warmup", "key", "overview", "err", err)
	}
	if _, err := s.opStatsSessions(ctx, nil); err != nil {
		s.log.Warn("stats warmup", "key", "sessions", "err", err)
	}
	if _, err := s.opStatsTrend(ctx, &trendIn{Months: 12}); err != nil {
		s.log.Warn("stats warmup", "key", "trend/12", "err", err)
	}
}
