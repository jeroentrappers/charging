-- +goose Up
-- +goose StatementBegin
-- "Price last confirmed" vs "price last changed". observed_from marks when the
-- current tariff version was OPENED (i.e. last changed). last_confirmed_at marks
-- the last time an ingest pass re-observed that same price in the feed, changed
-- or not. The status dashboard's price freshness should reflect confirmation,
-- so a stable-but-still-published price reads fresh instead of decaying to red.
ALTER TABLE tariff_version ADD COLUMN last_confirmed_at timestamptz NOT NULL DEFAULT now();
-- Backfill: the best evidence we have for existing rows is when they opened.
UPDATE tariff_version SET last_confirmed_at = observed_from;
-- +goose StatementEnd
-- +goose StatementBegin
-- Source-level "newest price" reads max(last_confirmed_at) over open versions.
CREATE INDEX tariff_confirmed_ix ON tariff_version (charger_id, last_confirmed_at DESC)
    WHERE observed_to IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS tariff_confirmed_ix;
ALTER TABLE tariff_version DROP COLUMN last_confirmed_at;
-- +goose StatementEnd
