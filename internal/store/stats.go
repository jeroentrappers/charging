package store

import (
	"context"
	"fmt"
	"time"
)

// Run is an ingestion-run record (admin view).
type Run struct {
	ID         int64      `json:"id"`
	CPOID      string     `json:"cpo_id"`
	Kind       string     `json:"kind"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	RowsSeen   int        `json:"rows_seen"`
	Changes    int        `json:"changes"`
	Error      *string    `json:"error"`
}

// RecentRuns returns recent ingestion runs, optionally filtered to one CPO.
func (s *Store) RecentRuns(ctx context.Context, cpoID string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, cpo_id, kind, started_at, finished_at, rows_seen, changes, error
		FROM ingest_run
		WHERE ($1 = '' OR cpo_id = $1)
		ORDER BY started_at DESC
		LIMIT $2`, cpoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.CPOID, &r.Kind, &r.StartedAt, &r.FinishedAt, &r.RowsSeen, &r.Changes, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PriceAgg is an aggregate of headline comparable prices for a group.
type PriceAgg struct {
	Group     string   `json:"group"`
	Count     int      `json:"count"`
	AvgEUR    *float64 `json:"avg_eur"`
	MedianEUR *float64 `json:"median_eur"`
	MinEUR    *float64 `json:"min_eur"`
	MaxEUR    *float64 `json:"max_eur"`
}

type Overview struct {
	Chargers       int        `json:"chargers"`
	PricedChargers int        `json:"priced_chargers"`
	ByCurrentType  []PriceAgg `json:"by_current_type"` // includes a "all" row
}

// Overview returns market-level counts and headline-price aggregates over the
// current (open) tariff versions, broken down by current type with an overall
// rollup row.
func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM charger`).Scan(&o.Chargers); err != nil {
		return o, err
	}
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM tariff_version WHERE observed_to IS NULL AND comparable_price_eur IS NOT NULL`).
		Scan(&o.PricedChargers); err != nil {
		return o, err
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT COALESCE(c.current_type, 'all') AS grp,
		       count(*),
		       avg(tv.comparable_price_eur)::float8,
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY tv.comparable_price_eur)::float8,
		       min(tv.comparable_price_eur)::float8,
		       max(tv.comparable_price_eur)::float8
		FROM charger c
		JOIN tariff_version tv ON tv.charger_id = c.id AND tv.observed_to IS NULL
		WHERE tv.comparable_price_eur IS NOT NULL
		GROUP BY ROLLUP (c.current_type)
		ORDER BY grp`)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var a PriceAgg
		if err := rows.Scan(&a.Group, &a.Count, &a.AvgEUR, &a.MedianEUR, &a.MinEUR, &a.MaxEUR); err != nil {
			return o, err
		}
		o.ByCurrentType = append(o.ByCurrentType, a)
	}
	return o, rows.Err()
}

// SessionStat aggregates one comparison-session profile across current tariffs.
type SessionStat struct {
	Session string  `json:"session"`
	Count   int     `json:"count"`
	AvgEUR  float64 `json:"avg_eur"`
	MinEUR  float64 `json:"min_eur"`
	MaxEUR  float64 `json:"max_eur"`
}

// SessionStats aggregates each session profile present in the current tariffs'
// comparable_prices maps.
func (s *Store) SessionStats(ctx context.Context) ([]SessionStat, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT kv.key, count(*),
		       avg(kv.value::float8), min(kv.value::float8), max(kv.value::float8)
		FROM tariff_version tv, jsonb_each_text(tv.comparable_prices) AS kv
		WHERE tv.observed_to IS NULL
		GROUP BY kv.key
		ORDER BY kv.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionStat
	for rows.Next() {
		var st SessionStat
		if err := rows.Scan(&st.Session, &st.Count, &st.AvgEUR, &st.MinEUR, &st.MaxEUR); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// RegionStats returns headline-price aggregates grouped by region (city or
// postal code), busiest first.
func (s *Store) RegionStats(ctx context.Context, by string, limit int) ([]PriceAgg, error) {
	col := "c.city"
	if by == "postal" {
		col = "c.postal_code"
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := fmt.Sprintf(`
		SELECT COALESCE(NULLIF(%s, ''), '?') AS grp,
		       count(*),
		       avg(tv.comparable_price_eur)::float8,
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY tv.comparable_price_eur)::float8,
		       min(tv.comparable_price_eur)::float8,
		       max(tv.comparable_price_eur)::float8
		FROM charger c
		JOIN tariff_version tv ON tv.charger_id = c.id AND tv.observed_to IS NULL
		WHERE tv.comparable_price_eur IS NOT NULL
		GROUP BY grp
		ORDER BY count(*) DESC, grp
		LIMIT $1`, col)
	rows, err := s.Pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PriceAgg
	for rows.Next() {
		var a PriceAgg
		if err := rows.Scan(&a.Group, &a.Count, &a.AvgEUR, &a.MedianEUR, &a.MinEUR, &a.MaxEUR); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// TrendPoint is the average headline price across all chargers for one month.
type TrendPoint struct {
	Month  string   `json:"month"` // YYYY-MM
	AvgEUR *float64 `json:"avg_eur"`
	Count  int      `json:"count"`
}

// PriceTrend returns the monthly average headline price over the last n months,
// using each tariff version's validity window (temporal aggregation over the
// SCD2 history). This is the core "are prices rising?" statistic.
//
// It reads the tariff_price_month rollup, so it is only as fresh as the last
// RefreshPriceMonths; a month the rollup has not seen yet reads as empty.
func (s *Store) PriceTrend(ctx context.Context, months int) ([]TrendPoint, error) {
	if months <= 0 || months > 60 {
		months = 12
	}
	rows, err := s.Pool.Query(ctx, `
		WITH m AS (
			SELECT generate_series(
				date_trunc('month', now()) - (($1::int - 1) * interval '1 month'),
				date_trunc('month', now()),
				interval '1 month') AS bucket
		)
		SELECT to_char(m.bucket, 'YYYY-MM') AS month,
		       (p.sum_eur / p.n)::float8,
		       coalesce(p.n, 0)
		FROM m
		LEFT JOIN tariff_price_month p ON p.month = m.bucket::date
		ORDER BY m.bucket`, months)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrendPoint
	for rows.Next() {
		var t TrendPoint
		if err := rows.Scan(&t.Month, &t.AvgEUR, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RefreshPriceMonths brings the tariff_price_month rollup up to date and returns
// the first month it recomputed plus how many months it wrote.
//
// Versions only open and close at now(), so the versions touching a month are
// fixed once the month is over: a month computed after it ended is final. Each
// call therefore recomputes only from the earliest non-final month (normally
// the current one, plus the previous one on the first refresh after a month
// rolls over), reading just the versions still open or closed since then. An
// empty rollup is backfilled from the whole history in one pass.
//
// One deliberate difference from aggregating live: FillDerivedPrices can later
// give an open version a euro price it lacked, and a final month does not pick
// that up. The month keeps the prices as they stood when it ended.
func (s *Store) RefreshPriceMonths(ctx context.Context) (from time.Time, months int64, err error) {
	var empty bool
	if err := s.Pool.QueryRow(ctx, `
		SELECT count(*) = 0,
		       coalesce(min(month) FILTER (WHERE computed_at < month + interval '1 month'),
		                date_trunc('month', now())::date)
		FROM tariff_price_month`).Scan(&empty, &from); err != nil {
		return time.Time{}, 0, err
	}
	if empty {
		from = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO tariff_price_month (month, sum_eur, n, computed_at)
		SELECT b.bucket::date, sum(tv.comparable_price_eur), count(*), now()
		FROM tariff_version tv,
		     generate_series(greatest(date_trunc('month', tv.observed_from), $1::date),
		                     date_trunc('month', coalesce(tv.observed_to, now())),
		                     interval '1 month') AS b(bucket)
		WHERE tv.comparable_price_eur IS NOT NULL
		  AND (tv.observed_to IS NULL OR tv.observed_to >= $1::date)
		GROUP BY b.bucket
		ON CONFLICT (month) DO UPDATE
		   SET sum_eur = excluded.sum_eur, n = excluded.n, computed_at = excluded.computed_at`, from)
	if err != nil {
		return from, 0, err
	}
	return from, tag.RowsAffected(), nil
}
