-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS postgis;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE cpo (
    id            text PRIMARY KEY,                 -- our slug / OCPI party id
    name          text NOT NULL,
    ocpi_base_url text NOT NULL,                    -- e.g. https://ocpi.energyvision.be/cpo/2.1.1/
    ocpi_version  text NOT NULL DEFAULT '2.1.1',
    token_env     text,                             -- name of env var holding the OCPI token
    poll_cron     text NOT NULL DEFAULT '0 4 * * *',
    enabled       boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE charger (
    id           bigserial PRIMARY KEY,
    cpo_id       text NOT NULL REFERENCES cpo(id),
    evse_uid     text NOT NULL,
    connector_id text NOT NULL,
    geom         geography(Point, 4326) NOT NULL,
    power_kw     numeric(6,2),
    plug_type    text,
    current_type text,                              -- AC / DC
    name         text,
    address      text,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (cpo_id, evse_uid, connector_id)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX charger_geom_gix ON charger USING gist (geom);
-- +goose StatementEnd

-- The historical record: append-only, one row per *change* (SCD Type 2).
-- +goose StatementBegin
CREATE TABLE tariff_version (
    id                   bigserial PRIMARY KEY,
    charger_id           bigint NOT NULL REFERENCES charger(id) ON DELETE CASCADE,
    tariff_hash          text NOT NULL,             -- content hash of the normalized tariff
    price_components     jsonb NOT NULL,            -- raw/normalized OCPI tariff elements
    comparable_price_eur numeric(8,4),              -- cost of a standard session (sortable)
    currency             text NOT NULL DEFAULT 'EUR',
    observed_from        timestamptz NOT NULL DEFAULT now(),
    observed_to          timestamptz,               -- NULL = current
    source_last_updated  timestamptz                -- OCPI tariff.last_updated when provided
);
-- +goose StatementEnd
-- Exactly one current version per charger + fast "current price" lookup.
-- +goose StatementBegin
CREATE UNIQUE INDEX tariff_current_ux ON tariff_version (charger_id) WHERE observed_to IS NULL;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX tariff_hist_ix ON tariff_version (charger_id, observed_from DESC);
-- +goose StatementEnd

-- Current availability only (overwritten each poll; not historized by design).
-- +goose StatementBegin
CREATE TABLE charger_status (
    charger_id      bigint PRIMARY KEY REFERENCES charger(id) ON DELETE CASCADE,
    status          text NOT NULL,                  -- AVAILABLE / CHARGING / OUTOFORDER / UNKNOWN ...
    available_count int,
    updated_at      timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- Honesty: one row per ingestion pass per CPO.
-- +goose StatementBegin
CREATE TABLE ingest_run (
    id          bigserial PRIMARY KEY,
    cpo_id      text NOT NULL REFERENCES cpo(id),
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    rows_seen   int NOT NULL DEFAULT 0,
    changes     int NOT NULL DEFAULT 0,
    error       text
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX ingest_run_cpo_ix ON ingest_run (cpo_id, started_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS ingest_run;
DROP TABLE IF EXISTS charger_status;
DROP TABLE IF EXISTS tariff_version;
DROP TABLE IF EXISTS charger;
DROP TABLE IF EXISTS cpo;
-- +goose StatementEnd
