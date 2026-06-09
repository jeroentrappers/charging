-- +goose Up
-- +goose StatementBegin
-- Structured location for regional statistics (kept alongside the display address).
ALTER TABLE charger ADD COLUMN postal_code text;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE charger ADD COLUMN city text;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX charger_postal_ix ON charger (postal_code);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS charger_postal_ix;
ALTER TABLE charger DROP COLUMN city;
ALTER TABLE charger DROP COLUMN postal_code;
-- +goose StatementEnd
