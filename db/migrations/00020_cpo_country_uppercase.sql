-- +goose Up
-- +goose StatementBegin
-- cpo.country is ISO 3166-1 alpha-2, stored uppercase. Sources auto-created from
-- a DATEX publicationCreator arrived lowercase (e.g. "de"), which split a single
-- country into two export region buckets (DE-00x and de-00x). Normalize existing
-- rows; new writes are uppercased in CPO.defaults().
UPDATE cpo SET country = upper(country)
WHERE country IS NOT NULL AND country <> upper(country);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Irreversible normalization; nothing to undo.
SELECT 1;
-- +goose StatementEnd
