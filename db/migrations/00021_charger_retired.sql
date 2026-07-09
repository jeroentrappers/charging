-- +goose Up
-- +goose StatementBegin
-- Soft-retire: a charger that a full-snapshot source stops publishing (e.g.
-- EnergyVision removing non-operational stations) is flagged retired instead of
-- deleted, so it drops out of the feeds and app but keeps its price history
-- (tariff_version) for analytics, and can be revived if the source lists it
-- again. Set by RetireAbsentChargers after a full ingest pass.
ALTER TABLE charger ADD COLUMN retired boolean NOT NULL DEFAULT false;
ALTER TABLE charger ADD COLUMN retired_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE charger DROP COLUMN retired_at;
ALTER TABLE charger DROP COLUMN retired;
-- +goose StatementEnd
