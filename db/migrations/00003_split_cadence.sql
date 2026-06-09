-- +goose Up
-- +goose StatementBegin
-- Availability is polled frequently; price (tariff) is polled rarely. They get
-- separate schedules.
ALTER TABLE cpo ADD COLUMN status_cron text NOT NULL DEFAULT '*/3 * * * *';
-- +goose StatementEnd
-- +goose StatementBegin
-- Distinguish the two kinds of ingestion pass in the run log.
ALTER TABLE ingest_run ADD COLUMN kind text NOT NULL DEFAULT 'price';
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX ingest_run_kind_ix ON ingest_run (cpo_id, kind, started_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS ingest_run_kind_ix;
ALTER TABLE ingest_run DROP COLUMN kind;
ALTER TABLE cpo DROP COLUMN status_cron;
-- +goose StatementEnd
