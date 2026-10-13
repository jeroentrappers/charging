package main

import (
	"context"
	"time"
)

// runPriceRollup refreshes the tariff_price_month rollup behind
// /stats/price-trend now and then every interval. The first run on an empty
// rollup scans the whole tariff history (a minute or two at ~30M versions);
// later runs only touch the open month and take seconds.
func (s *server) runPriceRollup(ctx context.Context, every time.Duration) {
	s.refreshPriceRollup(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refreshPriceRollup(ctx)
		}
	}
}

func (s *server) refreshPriceRollup(ctx context.Context) {
	// Generous: the backfill must be allowed to finish, or it never would.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	start := time.Now()
	from, months, err := s.st.RefreshPriceMonths(ctx)
	if err != nil {
		s.log.Error("price rollup", "err", err, "dur", time.Since(start))
		return
	}
	s.log.Info("price rollup", "from", from.Format("2006-01"), "months", months, "dur", time.Since(start))
}
