-- +goose Up
-- +goose StatementBegin
-- The country a CPO's chargers belong to, used to split the bulk export into
-- per-region files. Backfilled from the known seed sources.
ALTER TABLE cpo ADD COLUMN country text NOT NULL DEFAULT '';
UPDATE cpo SET country='NL' WHERE id='dotnl';
UPDATE cpo SET country='DE' WHERE id='bnetza';
UPDATE cpo SET country='FR' WHERE id='irve';
UPDATE cpo SET country='BE' WHERE id IN ('road','monta','energyvision','tesla','ecomovement');
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cpo DROP COLUMN country;
-- +goose StatementEnd
