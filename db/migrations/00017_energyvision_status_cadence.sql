-- +goose Up
-- +goose StatementBegin
-- EnergyVision's status feed regenerates every 60s (per its /datex/metadata),
-- so poll it every minute instead of every 5. The engine's signature cache
-- only writes connectors whose availability actually changed, and the parsed
-- static table is cached in-process, so the extra polls cost one ~5 MB status
-- fetch per minute and little else.
UPDATE cpo SET status_cron = '* * * * *'
WHERE id = 'energyvision' AND status_cron = '*/5 * * * *';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE cpo SET status_cron = '*/5 * * * *'
WHERE id = 'energyvision' AND status_cron = '* * * * *';
-- +goose StatementEnd
