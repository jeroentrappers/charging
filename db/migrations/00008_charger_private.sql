-- +goose Up
-- +goose StatementBegin
-- Private (home / peer-to-peer-shared) chargers are excluded from the public
-- search by default. There is no structured public/private flag in the
-- AFIR/DATEX feeds, so it's derived from the operator-given name (kept in sync
-- by the ingester via model.IsPrivateName). This backfills existing rows.
ALTER TABLE charger ADD COLUMN private boolean NOT NULL DEFAULT false;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE charger SET private = (
  lower(name) NOT LIKE '%public%'
  AND (lower(name) LIKE '%private%' OR lower(name) LIKE '%· home%')
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE charger DROP COLUMN private;
-- +goose StatementEnd
