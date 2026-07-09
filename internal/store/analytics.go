package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// AnalyticsEvent is one recorded event, ready for insertion. Props is stored as
// jsonb; nil is fine.
type AnalyticsEvent struct {
	TS          time.Time
	Kind        string
	Event       string
	Path        string
	Status      int
	ClientHash  string
	IPHash      string
	UAClass     string
	RefererHost string
	Props       map[string]any
}

// InsertEvents batch-inserts events in a single multi-row statement. Called off
// the request path by the analytics recorder.
func (s *Store) InsertEvents(ctx context.Context, evs []AnalyticsEvent) error {
	if len(evs) == 0 {
		return nil
	}
	rows := make([][]any, len(evs))
	for i, e := range evs {
		var props []byte
		if e.Props != nil {
			props, _ = json.Marshal(e.Props)
		}
		ts := e.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		rows[i] = []any{ts, e.Kind, e.Event, e.Path, e.Status, e.ClientHash, e.IPHash, e.UAClass, e.RefererHost, props}
	}
	_, err := s.Pool.CopyFrom(ctx,
		pgx.Identifier{"analytics_event"},
		[]string{"ts", "kind", "event", "path", "status", "client_hash", "ip_hash", "ua_class", "referer_host", "props"},
		pgx.CopyFromRows(rows))
	return err
}

// ---- rollups (for the admin dashboard) ----

// AnalyticsSummary is a compact overview over a trailing window.
type AnalyticsSummary struct {
	Since          time.Time        `json:"since"`
	Events         int64            `json:"events"`
	UniqueVisitors int64            `json:"unique_visitors"`
	TopEvents      []AnalyticsCount `json:"top_events"`
	TopEndpoints   []AnalyticsCount `json:"top_endpoints"`
	FeedConsumers  int64            `json:"feed_consumers"`
	Downloads      []AnalyticsCount `json:"downloads_by_format"`
	PerDay         []AnalyticsDay   `json:"events_per_day"`
}

type AnalyticsCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

type AnalyticsDay struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// Analytics returns a summary over the trailing `window`.
func (s *Store) Analytics(ctx context.Context, window time.Duration) (AnalyticsSummary, error) {
	since := time.Now().UTC().Add(-window)
	out := AnalyticsSummary{Since: since}

	if err := s.Pool.QueryRow(ctx, `
		SELECT count(*),
		       count(DISTINCT client_hash) FILTER (WHERE ua_class='browser' AND client_hash<>''),
		       count(DISTINCT ip_hash) FILTER (WHERE kind='feed' AND ip_hash<>'')
		FROM analytics_event WHERE ts >= $1`, since).
		Scan(&out.Events, &out.UniqueVisitors, &out.FeedConsumers); err != nil {
		return out, err
	}

	var err error
	if out.TopEvents, err = s.analyticsCounts(ctx, since,
		`SELECT event, count(*) FROM analytics_event WHERE ts >= $1 GROUP BY event ORDER BY 2 DESC LIMIT 20`); err != nil {
		return out, err
	}
	if out.TopEndpoints, err = s.analyticsCounts(ctx, since,
		`SELECT path, count(*) FROM analytics_event WHERE ts >= $1 AND kind='api' AND path<>'' GROUP BY path ORDER BY 2 DESC LIMIT 20`); err != nil {
		return out, err
	}
	if out.Downloads, err = s.analyticsCounts(ctx, since,
		`SELECT COALESCE(props->>'format','?'), count(*) FROM analytics_event WHERE ts >= $1 AND kind='feed' GROUP BY 1 ORDER BY 2 DESC LIMIT 20`); err != nil {
		return out, err
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT to_char(date_trunc('day', ts), 'YYYY-MM-DD'), count(*)
		FROM analytics_event WHERE ts >= $1 GROUP BY 1 ORDER BY 1`, since)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d AnalyticsDay
		if err := rows.Scan(&d.Day, &d.Count); err != nil {
			return out, err
		}
		out.PerDay = append(out.PerDay, d)
	}
	return out, rows.Err()
}

// RollupAnalyticsDay upserts the daily aggregate rows for one UTC calendar day
// from the raw events, so the totals survive the raw-event prune. Idempotent.
func (s *Store) RollupAnalyticsDay(ctx context.Context, day time.Time) error {
	day = day.UTC().Truncate(24 * time.Hour)
	start, end := day, day.Add(24*time.Hour)
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO analytics_daily (day, metric, count)
		SELECT $3::date, metric, cnt FROM (
			SELECT event AS metric, count(*) AS cnt FROM analytics_event
			  WHERE ts >= $1 AND ts < $2 GROUP BY event
			UNION ALL SELECT '_events', count(*) FROM analytics_event WHERE ts >= $1 AND ts < $2
			UNION ALL SELECT '_visitors', count(DISTINCT client_hash) FROM analytics_event
			  WHERE ts >= $1 AND ts < $2 AND ua_class='browser' AND client_hash <> ''
			UNION ALL SELECT '_feed_consumers', count(DISTINCT ip_hash) FROM analytics_event
			  WHERE ts >= $1 AND ts < $2 AND kind='feed' AND ip_hash <> ''
		) s
		ON CONFLICT (day, metric) DO UPDATE SET count = EXCLUDED.count`,
		start, end, day)
	return err
}

// PruneAnalyticsEvents deletes raw events older than `before`. The daily rollup
// keeps their aggregates. Returns how many rows were removed.
func (s *Store) PruneAnalyticsEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM analytics_event WHERE ts < $1`, before)
	return tag.RowsAffected(), err
}

func (s *Store) analyticsCounts(ctx context.Context, since time.Time, q string) ([]AnalyticsCount, error) {
	rows, err := s.Pool.Query(ctx, q, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalyticsCount
	for rows.Next() {
		var c AnalyticsCount
		if err := rows.Scan(&c.Key, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
