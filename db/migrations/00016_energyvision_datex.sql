-- +goose Up
-- +goose StatementBegin
-- EnergyVision finally shipped access — as DATEX II (AFIR v3.7) feeds, not the
-- OCPI 2.1.1 endpoint the row was seeded with. Point the source at the new
-- table|status pair and enable it (it only polls once ENERGYVISION_TOKEN is
-- set). Guarded on the old URL so an operator-tuned row is left alone.
UPDATE cpo SET
    ocpi_base_url = 'https://datex.cpo.energyvision.be/datex/energy-infrastructure-table|https://datex.cpo.energyvision.be/datex/energy-infrastructure-status',
    ocpi_version  = '',
    source_type   = 'datex_afir',
    poll_cron     = '0 4 * * *',
    status_cron   = '*/5 * * * *',
    enabled       = true
WHERE id = 'energyvision'
  AND ocpi_base_url = 'https://ocpi.energyvision.be/cpo/2.1.1/';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE cpo SET
    ocpi_base_url = 'https://ocpi.energyvision.be/cpo/2.1.1/',
    ocpi_version  = '2.1.1',
    source_type   = '',
    status_cron   = '*/3 * * * *',
    enabled       = false
WHERE id = 'energyvision'
  AND source_type = 'datex_afir';
-- +goose StatementEnd
