// Package store is the persistence layer. It uses pgx directly (rather than a
// query generator) because the schema leans on PostGIS geography and numeric
// types, which are cleaner to handle with explicit geo expressions and casts.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/appmire/charging/internal/model"
)

type Store struct{ Pool *pgxpool.Pool }

func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// ---- CPO registry ----

type CPO struct {
	ID          string
	Name        string
	OCPIBaseURL string
	OCPIVersion string
	TokenEnv    string
	PollCron    string
	Enabled     bool
}

func (s *Store) UpsertCPO(ctx context.Context, c CPO) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO cpo (id, name, ocpi_base_url, ocpi_version, token_env, poll_cron, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name, ocpi_base_url=EXCLUDED.ocpi_base_url,
			ocpi_version=EXCLUDED.ocpi_version, token_env=EXCLUDED.token_env,
			poll_cron=EXCLUDED.poll_cron, enabled=EXCLUDED.enabled`,
		c.ID, c.Name, c.OCPIBaseURL, c.OCPIVersion, c.TokenEnv, c.PollCron, c.Enabled)
	return err
}

func (s *Store) ListEnabledCPOs(ctx context.Context) ([]CPO, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, name, ocpi_base_url, ocpi_version, COALESCE(token_env,''), poll_cron, enabled
		FROM cpo WHERE enabled ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CPO
	for rows.Next() {
		var c CPO
		if err := rows.Scan(&c.ID, &c.Name, &c.OCPIBaseURL, &c.OCPIVersion, &c.TokenEnv, &c.PollCron, &c.Enabled); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- Chargers + status ----

// UpsertCharger inserts or updates a connector by its stable identity and
// returns its internal id. Geography is built from lon/lat in SQL.
func (s *Store) UpsertCharger(ctx context.Context, c model.Connector) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO charger
			(cpo_id, evse_uid, connector_id, geom, power_kw, plug_type, current_type, name, address, last_seen_at)
		VALUES
			($1,$2,$3, ST_SetSRID(ST_MakePoint($4,$5),4326)::geography, $6,$7,$8,$9,$10, now())
		ON CONFLICT (cpo_id, evse_uid, connector_id) DO UPDATE SET
			geom=EXCLUDED.geom, power_kw=EXCLUDED.power_kw, plug_type=EXCLUDED.plug_type,
			current_type=EXCLUDED.current_type, name=EXCLUDED.name, address=EXCLUDED.address,
			last_seen_at=now()
		RETURNING id`,
		c.CPOID, c.EVSEUID, c.ConnectorID, c.Lon, c.Lat,
		c.PowerKW, c.PlugType, c.CurrentType, c.Name, c.Address).Scan(&id)
	return id, err
}

func (s *Store) UpsertStatus(ctx context.Context, chargerID int64, status string, availableCount int) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO charger_status (charger_id, status, available_count, updated_at)
		VALUES ($1,$2,$3, now())
		ON CONFLICT (charger_id) DO UPDATE SET
			status=EXCLUDED.status, available_count=EXCLUDED.available_count, updated_at=now()`,
		chargerID, status, availableCount)
	return err
}

// ---- Tariff versioning (SCD Type 2) ----

// CurrentTariffHash returns the hash of the charger's current (open) tariff
// version. found is false when there is no open version yet.
func (s *Store) CurrentTariffHash(ctx context.Context, chargerID int64) (hash string, found bool, err error) {
	err = s.Pool.QueryRow(ctx,
		`SELECT tariff_hash FROM tariff_version WHERE charger_id=$1 AND observed_to IS NULL`,
		chargerID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// ReplaceTariff closes the current open version (if any) and inserts a new one,
// atomically. Call only when the hash has actually changed.
func (s *Store) ReplaceTariff(ctx context.Context, chargerID int64, hash string, components []byte, comparable *float64, currency string, sourceLastUpdated *time.Time) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE tariff_version SET observed_to=now() WHERE charger_id=$1 AND observed_to IS NULL`,
			chargerID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO tariff_version
				(charger_id, tariff_hash, price_components, comparable_price_eur, currency, observed_from, source_last_updated)
			VALUES ($1,$2,$3,$4,$5, now(), $6)`,
			chargerID, hash, components, comparable, currency, sourceLastUpdated)
		return err
	})
}

// ---- Ingestion run log ----

func (s *Store) StartRun(ctx context.Context, cpoID string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO ingest_run (cpo_id, started_at) VALUES ($1, now()) RETURNING id`,
		cpoID).Scan(&id)
	return id, err
}

func (s *Store) FinishRun(ctx context.Context, runID int64, rowsSeen, changes int, runErr error) error {
	var errStr *string
	if runErr != nil {
		e := runErr.Error()
		errStr = &e
	}
	_, err := s.Pool.Exec(ctx,
		`UPDATE ingest_run SET finished_at=now(), rows_seen=$2, changes=$3, error=$4 WHERE id=$1`,
		runID, rowsSeen, changes, errStr)
	return err
}

// ---- Query side (serves the app) ----

type NearbyCharger struct {
	ID          int64    `json:"id"`
	CPOID       string   `json:"cpo_id"`
	Name        string   `json:"name"`
	Address     string   `json:"address"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	PowerKW     float64  `json:"power_kw"`
	PlugType    string   `json:"plug_type"`
	CurrentType string   `json:"current_type"`
	DistanceM   float64  `json:"distance_m"`
	Available   int      `json:"available_count"`
	PriceEUR    *float64 `json:"comparable_price_eur"`
	Currency    string   `json:"currency"`
}

type NearbyQuery struct {
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	RadiusM    float64 `json:"radius_m"`
	MinPowerKW float64 `json:"min_power_kw"` // 0 = no filter
	PlugType   string  `json:"plug_type"`    // "" = no filter
	OnlyAvail  bool    `json:"only_available"`
	Limit      int     `json:"limit"`
}

// CheapestNearby returns chargers within radius, optionally only those with a
// free connector, ordered by comparable price (nulls last) then distance.
func (s *Store) CheapestNearby(ctx context.Context, q NearbyQuery) ([]NearbyCharger, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.cpo_id, COALESCE(c.name,''), COALESCE(c.address,''),
		       ST_Y(c.geom::geometry), ST_X(c.geom::geometry),
		       COALESCE(c.power_kw,0)::float8, COALESCE(c.plug_type,''), COALESCE(c.current_type,''),
		       ST_Distance(c.geom, ST_SetSRID(ST_MakePoint($2,$1),4326)::geography) AS dist,
		       COALESCE(st.available_count,0),
		       tv.comparable_price_eur::float8, COALESCE(tv.currency,'EUR')
		FROM charger c
		LEFT JOIN charger_status st ON st.charger_id = c.id
		LEFT JOIN tariff_version tv ON tv.charger_id = c.id AND tv.observed_to IS NULL
		WHERE ST_DWithin(c.geom, ST_SetSRID(ST_MakePoint($2,$1),4326)::geography, $3)
		  AND ($4 = 0 OR COALESCE(c.power_kw,0) >= $4)
		  AND ($5 = '' OR c.plug_type = $5)
		  AND (NOT $6 OR COALESCE(st.available_count,0) > 0)
		ORDER BY tv.comparable_price_eur ASC NULLS LAST, dist ASC
		LIMIT $7`,
		q.Lat, q.Lon, q.RadiusM, q.MinPowerKW, q.PlugType, q.OnlyAvail, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NearbyCharger
	for rows.Next() {
		var n NearbyCharger
		if err := rows.Scan(&n.ID, &n.CPOID, &n.Name, &n.Address, &n.Lat, &n.Lon,
			&n.PowerKW, &n.PlugType, &n.CurrentType, &n.DistanceM, &n.Available,
			&n.PriceEUR, &n.Currency); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

type PricePoint struct {
	PriceEUR          *float64   `json:"comparable_price_eur"`
	Currency          string     `json:"currency"`
	ObservedFrom      time.Time  `json:"observed_from"`
	ObservedTo        *time.Time `json:"observed_to"`
	SourceLastUpdated *time.Time `json:"source_last_updated"`
}

// PriceHistory returns every recorded tariff version for a charger, newest first.
func (s *Store) PriceHistory(ctx context.Context, chargerID int64) ([]PricePoint, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT comparable_price_eur::float8, currency, observed_from, observed_to, source_last_updated
		FROM tariff_version WHERE charger_id=$1
		ORDER BY observed_from DESC`, chargerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PricePoint
	for rows.Next() {
		var p PricePoint
		if err := rows.Scan(&p.PriceEUR, &p.Currency, &p.ObservedFrom, &p.ObservedTo, &p.SourceLastUpdated); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
