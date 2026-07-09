-- +goose Up
-- +goose StatementBegin
-- Daily rollup archive for analytics. Raw analytics_event rows are pruned after
-- a retention window (default 90d); this table preserves per-day aggregates
-- indefinitely so long-range trends survive the prune. metric is either an
-- event name or a synthetic key: '_events' (total), '_visitors' (distinct
-- browser visitors), '_feed_consumers' (distinct feed IPs).
CREATE TABLE analytics_daily (
    day    date   NOT NULL,
    metric text   NOT NULL,
    count  bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (day, metric)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS analytics_daily;
-- +goose StatementEnd
