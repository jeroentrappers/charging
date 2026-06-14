package store

import (
	"context"
	"time"
)

// SourceHealth is one source's operational state for the status dashboard.
type SourceHealth struct {
	ID         string
	Name       string
	SourceType string
	Country    string
	Enabled    bool
	Chargers   int
	Priced     int
	Available  int        // chargers currently reporting >0 available connectors
	NewestStatus *time.Time // most recent availability update across the source
	NewestPrice  *time.Time // most recent current-tariff timestamp
}

// SourceHealthAll returns per-source health/staleness/availability for every
// CPO, busiest first. Works for polled and push sources alike (status/price
// freshness is read from the data, not the ingest log).
func (s *Store) SourceHealthAll(ctx context.Context) ([]SourceHealth, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, COALESCE(p.name,''), COALESCE(p.source_type,''), COALESCE(p.country,''), p.enabled,
		       count(c.id) AS chargers,
		       count(tv.charger_id) AS priced,
		       count(*) FILTER (WHERE COALESCE(st.available_count,0) > 0) AS available,
		       max(st.updated_at) AS newest_status,
		       max(tv.observed_from) AS newest_price
		FROM cpo p
		LEFT JOIN charger c            ON c.cpo_id = p.id
		LEFT JOIN charger_status st    ON st.charger_id = c.id
		LEFT JOIN tariff_version tv    ON tv.charger_id = c.id AND tv.observed_to IS NULL
		GROUP BY p.id, p.name, p.source_type, p.country, p.enabled
		ORDER BY chargers DESC, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceHealth
	for rows.Next() {
		var h SourceHealth
		if err := rows.Scan(&h.ID, &h.Name, &h.SourceType, &h.Country, &h.Enabled,
			&h.Chargers, &h.Priced, &h.Available, &h.NewestStatus, &h.NewestPrice); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
