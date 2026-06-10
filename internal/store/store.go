// Package store is the persistence layer. It uses pgx directly (rather than a
// query generator) because the schema leans on PostGIS geography and numeric
// types, which are cleaner to handle with explicit geo expressions and casts.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Token       string // DB-stored token (secret); preferred over TokenEnv. Never serialized.
	PollCron    string // price (tariff) poll schedule
	StatusCron  string // availability poll schedule
	SourceType  string // "ocpi" (default) | "datex"
	Enabled     bool
}

const cpoCols = `id, name, ocpi_base_url, ocpi_version, COALESCE(token_env,''), COALESCE(token,''), poll_cron, status_cron, source_type, enabled`

func scanCPO(row interface{ Scan(...any) error }) (CPO, error) {
	var c CPO
	err := row.Scan(&c.ID, &c.Name, &c.OCPIBaseURL, &c.OCPIVersion, &c.TokenEnv, &c.Token, &c.PollCron, &c.StatusCron, &c.SourceType, &c.Enabled)
	return c, err
}

// SeedCPO inserts a source only if it does not already exist, so restarts never
// clobber operator-managed fields (enabled, token, schedules).
func (s *Store) SeedCPO(ctx context.Context, c CPO) error {
	c.defaults()
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO cpo (id, name, ocpi_base_url, ocpi_version, token_env, poll_cron, status_cron, source_type, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO NOTHING`,
		c.ID, c.Name, c.OCPIBaseURL, c.OCPIVersion, c.TokenEnv, c.PollCron, c.StatusCron, c.SourceType, c.Enabled)
	return err
}

// UpsertCPO fully creates or replaces a source (admin "add/replace"). It does
// not touch the token (use SetToken) so callers can't accidentally wipe it.
func (s *Store) UpsertCPO(ctx context.Context, c CPO) error {
	c.defaults()
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO cpo (id, name, ocpi_base_url, ocpi_version, token_env, poll_cron, status_cron, source_type, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name, ocpi_base_url=EXCLUDED.ocpi_base_url,
			ocpi_version=EXCLUDED.ocpi_version, token_env=EXCLUDED.token_env,
			poll_cron=EXCLUDED.poll_cron, status_cron=EXCLUDED.status_cron,
			source_type=EXCLUDED.source_type, enabled=EXCLUDED.enabled`,
		c.ID, c.Name, c.OCPIBaseURL, c.OCPIVersion, c.TokenEnv, c.PollCron, c.StatusCron, c.SourceType, c.Enabled)
	return err
}

func (c *CPO) defaults() {
	if c.StatusCron == "" {
		c.StatusCron = "*/3 * * * *"
	}
	if c.PollCron == "" {
		c.PollCron = "0 4 * * *"
	}
	if c.SourceType == "" {
		c.SourceType = "ocpi"
	}
}

func (s *Store) ListEnabledCPOs(ctx context.Context) ([]CPO, error) {
	return s.listCPOs(ctx, "WHERE enabled")
}

func (s *Store) ListAllCPOs(ctx context.Context) ([]CPO, error) {
	return s.listCPOs(ctx, "")
}

func (s *Store) listCPOs(ctx context.Context, where string) ([]CPO, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+cpoCols+` FROM cpo `+where+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CPO
	for rows.Next() {
		c, err := scanCPO(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCPO(ctx context.Context, id string) (CPO, bool, error) {
	c, err := scanCPO(s.Pool.QueryRow(ctx, `SELECT `+cpoCols+` FROM cpo WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return CPO{}, false, nil
	}
	return c, err == nil, err
}

func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `UPDATE cpo SET enabled=$2 WHERE id=$1`, id, enabled)
	return tag.RowsAffected() > 0, err
}

func (s *Store) SetToken(ctx context.Context, id, token string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `UPDATE cpo SET token=NULLIF($2,'') WHERE id=$1`, id, token)
	return tag.RowsAffected() > 0, err
}

func (s *Store) DeleteCPO(ctx context.Context, id string) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM cpo WHERE id=$1`, id)
	return tag.RowsAffected() > 0, err
}

// ---- Chargers + status ----

// UpsertCharger inserts or updates a connector by its stable identity and
// returns its internal id. Geography is built from lon/lat in SQL.
func (s *Store) UpsertCharger(ctx context.Context, c model.Connector) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO charger
			(cpo_id, evse_uid, connector_id, geom, power_kw, plug_type, current_type, name, address, postal_code, city, last_seen_at)
		VALUES
			($1,$2,$3, ST_SetSRID(ST_MakePoint($4,$5),4326)::geography, $6,$7,$8,$9,$10,$11,$12, now())
		ON CONFLICT (cpo_id, evse_uid, connector_id) DO UPDATE SET
			geom=EXCLUDED.geom, power_kw=EXCLUDED.power_kw, plug_type=EXCLUDED.plug_type,
			current_type=EXCLUDED.current_type, name=EXCLUDED.name, address=EXCLUDED.address,
			postal_code=EXCLUDED.postal_code, city=EXCLUDED.city, last_seen_at=now()
		RETURNING id`,
		c.CPOID, c.EVSEUID, c.ConnectorID, c.Lon, c.Lat,
		c.PowerKW, c.PlugType, c.CurrentType, c.Name, c.Address, c.PostalCode, c.City).Scan(&id)
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

// RefreshTarget is a charger the Monta crawl should refresh next.
type RefreshTarget struct {
	ID          int64
	EVSEUID     string
	PowerKW     float64
	CurrentType string
}

// ChargersToRefresh returns a CPO's chargers ordered by least-recently-statused
// first (NULLs first), so a paced crawl cycles through all of them — whether or
// not they ended up with a price last time (status is always written).
func (s *Store) ChargersToRefresh(ctx context.Context, cpoID string, limit int) ([]RefreshTarget, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.evse_uid, COALESCE(c.power_kw,0)::float8, COALESCE(c.current_type,'')
		FROM charger c
		LEFT JOIN charger_status st ON st.charger_id = c.id
		WHERE c.cpo_id = $1
		ORDER BY st.updated_at ASC NULLS FIRST
		LIMIT $2`, cpoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefreshTarget
	for rows.Next() {
		var t RefreshTarget
		if err := rows.Scan(&t.ID, &t.EVSEUID, &t.PowerKW, &t.CurrentType); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
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

// TariffWrite is the payload for a new tariff version.
type TariffWrite struct {
	Hash              string
	Components        []byte // price_components jsonb (the normalized tariff)
	Comparable        *float64
	Prices            []byte // comparable_prices jsonb ({profile_key: price})
	Currency          string
	SourceLastUpdated *time.Time
}

// ReplaceTariff closes the current open version (if any) and inserts a new one,
// atomically. Call only when the hash has actually changed.
func (s *Store) ReplaceTariff(ctx context.Context, chargerID int64, w TariffWrite) error {
	prices := w.Prices
	if len(prices) == 0 {
		prices = []byte("{}")
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE tariff_version SET observed_to=now() WHERE charger_id=$1 AND observed_to IS NULL`,
			chargerID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO tariff_version
				(charger_id, tariff_hash, price_components, comparable_price_eur, comparable_prices, currency, observed_from, source_last_updated)
			VALUES ($1,$2,$3,$4,$5,$6, now(), $7)`,
			chargerID, w.Hash, w.Components, w.Comparable, prices, w.Currency, w.SourceLastUpdated)
		return err
	})
}

// ---- Ingestion run log ----

func (s *Store) StartRun(ctx context.Context, cpoID, kind string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO ingest_run (cpo_id, kind, started_at) VALUES ($1,$2, now()) RETURNING id`,
		cpoID, kind).Scan(&id)
	return id, err
}

// LastSuccess returns the finish time of the most recent error-free run of the
// given kind for a CPO. found is false when there has been no successful run.
func (s *Store) LastSuccess(ctx context.Context, cpoID, kind string) (t time.Time, found bool, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT finished_at FROM ingest_run
		WHERE cpo_id=$1 AND kind=$2 AND error IS NULL AND finished_at IS NOT NULL
		ORDER BY finished_at DESC LIMIT 1`, cpoID, kind).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
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
	ID           int64              `json:"id"`
	CPOID        string             `json:"cpo_id"`
	Name         string             `json:"name"`
	Address      string             `json:"address"`
	Lat          float64            `json:"lat"`
	Lon          float64            `json:"lon"`
	PowerKW      float64            `json:"power_kw"`
	PlugType     string             `json:"plug_type"`
	CurrentType  string             `json:"current_type"`
	DistanceM    float64            `json:"distance_m"`
	Available    int                `json:"available_count"`
	PriceEUR     *float64           `json:"comparable_price_eur"` // headline (10–80% at this charger's power)
	SessionPrice *float64           `json:"session_price_eur,omitempty"`
	Prices       map[string]float64 `json:"comparable_prices"` // per-session profile prices
	Currency     string             `json:"currency"`
	StatusAt     *time.Time         `json:"status_updated_at"`
	Stale        bool               `json:"availability_stale"` // status older than the freshness window
	Components   json.RawMessage    `json:"-"`                  // structured tariff, for request-time (time-of-day) pricing
}

type NearbyQuery struct {
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	RadiusM    float64 `json:"radius_m"`
	MinPowerKW float64 `json:"min_power_kw"` // 0 = no filter
	PlugType   string  `json:"plug_type"`    // "" = no filter
	OnlyAvail  bool    `json:"only_available"`
	Session    string  `json:"session"` // profile key to sort/return by; "" = headline
	// StaleAfter: availability older than this counts as unknown (excluded when
	// OnlyAvail) and flagged Stale. Zero disables staleness handling.
	StaleAfter time.Duration `json:"stale_after"`
	Limit      int           `json:"limit"`
}

// CheapestNearby returns candidate chargers within radius (and matching the
// filters), ordered by distance, including the structured tariff so the caller
// can compute a request-time (time-of-day-aware) price and re-rank. Limit caps
// the candidate pool. Each result carries the stored noon comparable + the
// per-session map as fallbacks.
func (s *Store) CheapestNearby(ctx context.Context, q NearbyQuery) ([]NearbyCharger, error) {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 300
	}
	staleSecs := q.StaleAfter.Seconds() // 0 -> staleness checks are no-ops

	// A status is fresh when staleness is disabled ($8<=0) or it was updated
	// within the window.
	const freshExpr = `($8 <= 0 OR (st.updated_at IS NOT NULL AND st.updated_at > now() - make_interval(secs => $8)))`

	query := fmt.Sprintf(`
		SELECT c.id, c.cpo_id, COALESCE(c.name,''), COALESCE(c.address,''),
		       ST_Y(c.geom::geometry), ST_X(c.geom::geometry),
		       COALESCE(c.power_kw,0)::float8, COALESCE(c.plug_type,''), COALESCE(c.current_type,''),
		       ST_Distance(c.geom, ST_SetSRID(ST_MakePoint($2,$1),4326)::geography) AS dist,
		       COALESCE(st.available_count,0),
		       tv.comparable_price_eur::float8, COALESCE(tv.comparable_prices,'{}'::jsonb), COALESCE(tv.currency,'EUR'),
		       st.updated_at,
		       (NOT %s) AS stale,
		       tv.price_components
		FROM charger c
		LEFT JOIN charger_status st ON st.charger_id = c.id
		LEFT JOIN tariff_version tv ON tv.charger_id = c.id AND tv.observed_to IS NULL
		WHERE ST_DWithin(c.geom, ST_SetSRID(ST_MakePoint($2,$1),4326)::geography, $3)
		  AND ($4 = 0 OR COALESCE(c.power_kw,0) >= $4)
		  AND ($5 = '' OR c.plug_type = $5)
		  AND (NOT $6 OR (COALESCE(st.available_count,0) > 0 AND %s))
		ORDER BY dist ASC
		LIMIT $7`, freshExpr, freshExpr)

	rows, err := s.Pool.Query(ctx, query,
		q.Lat, q.Lon, q.RadiusM, q.MinPowerKW, q.PlugType, q.OnlyAvail, q.Limit, staleSecs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NearbyCharger
	for rows.Next() {
		var n NearbyCharger
		var pricesJSON, componentsJSON []byte
		if err := rows.Scan(&n.ID, &n.CPOID, &n.Name, &n.Address, &n.Lat, &n.Lon,
			&n.PowerKW, &n.PlugType, &n.CurrentType, &n.DistanceM, &n.Available,
			&n.PriceEUR, &pricesJSON, &n.Currency, &n.StatusAt, &n.Stale, &componentsJSON); err != nil {
			return nil, err
		}
		n.Prices = decodePrices(pricesJSON)
		n.Components = json.RawMessage(componentsJSON)
		if q.Session != "" {
			if v, ok := n.Prices[q.Session]; ok {
				n.SessionPrice = &v
			}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func decodePrices(b []byte) map[string]float64 {
	m := map[string]float64{}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

type PricePoint struct {
	PriceEUR          *float64           `json:"comparable_price_eur"`
	Prices            map[string]float64 `json:"comparable_prices"`
	Components        json.RawMessage    `json:"price_components"` // the structured tariff (€/kWh, €/min, fees, restrictions)
	Currency          string             `json:"currency"`
	ObservedFrom      time.Time          `json:"observed_from"`
	ObservedTo        *time.Time         `json:"observed_to"`
	SourceLastUpdated *time.Time         `json:"source_last_updated"`
}

// PriceHistory returns every recorded tariff version for a charger, newest first.
func (s *Store) PriceHistory(ctx context.Context, chargerID int64) ([]PricePoint, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT comparable_price_eur::float8, COALESCE(comparable_prices,'{}'::jsonb), price_components, currency, observed_from, observed_to, source_last_updated
		FROM tariff_version WHERE charger_id=$1
		ORDER BY observed_from DESC`, chargerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PricePoint
	for rows.Next() {
		var p PricePoint
		var pricesJSON, componentsJSON []byte
		if err := rows.Scan(&p.PriceEUR, &pricesJSON, &componentsJSON, &p.Currency, &p.ObservedFrom, &p.ObservedTo, &p.SourceLastUpdated); err != nil {
			return nil, err
		}
		p.Prices = decodePrices(pricesJSON)
		p.Components = json.RawMessage(componentsJSON)
		out = append(out, p)
	}
	return out, rows.Err()
}
