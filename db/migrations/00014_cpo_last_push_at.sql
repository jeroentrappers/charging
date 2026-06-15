-- +goose Up
-- Per-source push heartbeat. Delta push feeds (e.g. Tesla, EnBW) only send the
-- chargers whose status just changed, so an unchanged-but-healthy charger's
-- charger_status.updated_at ages out and it wrongly reads as stale (and drops
-- from "available now" results) even while its source is actively pushing.
-- last_push_at records when we last heard from the source; the read-path
-- freshness check treats a charger as live when its source pushed recently,
-- provided we have at least one real status reading for it.
-- +goose StatementBegin
ALTER TABLE cpo ADD COLUMN last_push_at timestamptz;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cpo DROP COLUMN last_push_at;
-- +goose StatementEnd
