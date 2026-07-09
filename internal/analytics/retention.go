package analytics

import (
	"context"
	"log/slog"
	"time"
)

// RetentionStore is the persistence dependency for the rollup/prune job.
type RetentionStore interface {
	RollupAnalyticsDay(ctx context.Context, day time.Time) error
	PruneAnalyticsEvents(ctx context.Context, before time.Time) (int64, error)
}

// Retention rolls raw events up into the daily archive and prunes raw events
// past the retention window. It runs an initial catch-up pass, then repeats
// every `every`.
type Retention struct {
	Store         RetentionStore
	Log           *slog.Logger
	RetentionDays int           // raw events older than this are pruned; <=0 → 90
	Every         time.Duration // rollup/prune cadence; <=0 → 6h
	Now           func() time.Time
}

func (r *Retention) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

// Run does one pass immediately, then every r.Every until ctx is cancelled.
func (r *Retention) Run(ctx context.Context) {
	days := r.RetentionDays
	if days <= 0 {
		days = 90
	}
	every := r.Every
	if every <= 0 {
		every = 6 * time.Hour
	}
	r.once(ctx, days)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.once(ctx, days)
		}
	}
}

func (r *Retention) once(ctx context.Context, days int) {
	now := r.now()
	// Roll up today + the last few days (covers restarts / late-arriving flushes).
	for i := 0; i < 3; i++ {
		day := now.AddDate(0, 0, -i)
		if err := r.Store.RollupAnalyticsDay(ctx, day); err != nil {
			r.Log.Warn("analytics rollup", "day", day.Format("2006-01-02"), "err", err)
		}
	}
	before := now.AddDate(0, 0, -days)
	if n, err := r.Store.PruneAnalyticsEvents(ctx, before); err != nil {
		r.Log.Warn("analytics prune", "err", err)
	} else if n > 0 {
		r.Log.Info("analytics prune", "removed", n, "older_than", before.Format("2006-01-02"))
	}
}
