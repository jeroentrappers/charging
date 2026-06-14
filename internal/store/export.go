package store

import (
	"context"
	"encoding/json"
	"time"
)

// ExportCharger is a full normalized record for the bulk dataset dump: one row
// per connector with its current open tariff and latest availability.
type ExportCharger struct {
	ID             int64              `json:"id"`
	CPOID          string             `json:"cpo_id"`
	Country        string             `json:"country"`
	EVSEUID        string             `json:"evse_uid"`
	ConnectorID    string             `json:"connector_id"`
	Name           string             `json:"name"`
	Address        string             `json:"address"`
	PostalCode     string             `json:"postal_code"`
	City           string             `json:"city"`
	Lat            float64            `json:"lat"`
	Lon            float64            `json:"lon"`
	PowerKW        float64            `json:"power_kw"`
	PlugType       string             `json:"plug_type"`
	CurrentType    string             `json:"current_type"`
	Status         string             `json:"status"`
	AvailableCount int                `json:"available_count"`
	StatusAt       *time.Time         `json:"status_updated_at"`
	PriceEUR       *float64           `json:"comparable_price_eur"`
	Prices         map[string]float64 `json:"comparable_prices,omitempty"`
	Currency       string             `json:"currency"`
	Private        bool               `json:"private"` // home / peer-to-peer charger
	Components     json.RawMessage    `json:"-"`       // structured tariff (parsed by the exporter)
}

// ExportAll streams every charger with its current open tariff version and
// latest status, ordered by id. Used to build the static bulk dumps.
func (s *Store) ExportAll(ctx context.Context) ([]ExportCharger, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.cpo_id, c.evse_uid, c.connector_id,
		       COALESCE(c.name,''), COALESCE(c.address,''),
		       COALESCE(c.postal_code,''), COALESCE(c.city,''),
		       ST_Y(c.geom::geometry), ST_X(c.geom::geometry),
		       COALESCE(c.power_kw,0)::float8, COALESCE(c.plug_type,''), COALESCE(c.current_type,''),
		       COALESCE(st.status,''), COALESCE(st.available_count,0), st.updated_at,
		       tv.comparable_price_eur::float8, COALESCE(tv.comparable_prices,'{}'::jsonb),
		       COALESCE(tv.currency,'EUR'), c.private, tv.price_components
		FROM charger c
		LEFT JOIN charger_status st ON st.charger_id = c.id
		LEFT JOIN tariff_version tv ON tv.charger_id = c.id AND tv.observed_to IS NULL
		ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ExportCharger
	for rows.Next() {
		var e ExportCharger
		if err := rows.Scan(
			&e.ID, &e.CPOID, &e.EVSEUID, &e.ConnectorID,
			&e.Name, &e.Address, &e.PostalCode, &e.City,
			&e.Lat, &e.Lon, &e.PowerKW, &e.PlugType, &e.CurrentType,
			&e.Status, &e.AvailableCount, &e.StatusAt,
			&e.PriceEUR, &e.Prices, &e.Currency, &e.Private, &e.Components,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ExportStream streams every charger (with current tariff + status + source
// country) to fn, ordered so each region's connectors are contiguous. No slice.
func (s *Store) ExportStream(ctx context.Context, fn func(ExportCharger) error) error {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.cpo_id, COALESCE(p.country,''), c.evse_uid, c.connector_id,
		       COALESCE(c.name,''), COALESCE(c.address,''),
		       COALESCE(c.postal_code,''), COALESCE(c.city,''),
		       ST_Y(c.geom::geometry), ST_X(c.geom::geometry),
		       COALESCE(c.power_kw,0)::float8, COALESCE(c.plug_type,''), COALESCE(c.current_type,''),
		       COALESCE(st.status,''), COALESCE(st.available_count,0), st.updated_at,
		       tv.comparable_price_eur::float8, COALESCE(tv.comparable_prices,'{}'::jsonb),
		       COALESCE(tv.currency,'EUR'), c.private, tv.price_components
		FROM charger c
		LEFT JOIN charger_status st ON st.charger_id = c.id
		LEFT JOIN tariff_version tv ON tv.charger_id = c.id AND tv.observed_to IS NULL
		LEFT JOIN cpo p ON p.id = c.cpo_id
		-- Cross-source dedup: drop register chargers that a richer source covers
		-- (precomputed by RecomputeSuperseded, refreshed before each full export —
		-- the per-row spatial subquery is too slow over the whole table).
		WHERE NOT c.superseded
		ORDER BY p.country, c.postal_code, c.cpo_id, c.evse_uid, c.connector_id`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var e ExportCharger
		if err := rows.Scan(
			&e.ID, &e.CPOID, &e.Country, &e.EVSEUID, &e.ConnectorID,
			&e.Name, &e.Address, &e.PostalCode, &e.City,
			&e.Lat, &e.Lon, &e.PowerKW, &e.PlugType, &e.CurrentType,
			&e.Status, &e.AvailableCount, &e.StatusAt,
			&e.PriceEUR, &e.Prices, &e.Currency, &e.Private, &e.Components,
		); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// AvailabilitySnapshot is the small, frequently-rotated availability delta.
type AvailabilitySnapshot struct {
	ID             int64      `json:"id"`
	Status         string     `json:"status"`
	AvailableCount int        `json:"available_count"`
	UpdatedAt      *time.Time `json:"updated_at"`
}

// ExportAvailability returns just the live status of every charger that has one,
// ordered by id — cheap to regenerate frequently.
func (s *Store) ExportAvailability(ctx context.Context) ([]AvailabilitySnapshot, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT charger_id, status, COALESCE(available_count,0), updated_at
		FROM charger_status
		ORDER BY charger_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AvailabilitySnapshot
	for rows.Next() {
		var a AvailabilitySnapshot
		if err := rows.Scan(&a.ID, &a.Status, &a.AvailableCount, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
