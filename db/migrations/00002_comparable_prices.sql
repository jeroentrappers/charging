-- +goose Up
-- +goose StatementBegin
-- Per-session comparable prices, keyed by profile (e.g. "charge1080_dc150").
-- The single comparable_price_eur remains the headline used for default sort.
ALTER TABLE tariff_version ADD COLUMN comparable_prices jsonb NOT NULL DEFAULT '{}'::jsonb;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tariff_version DROP COLUMN comparable_prices;
-- +goose StatementEnd
