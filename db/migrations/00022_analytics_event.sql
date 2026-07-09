-- +goose Up
-- +goose StatementBegin
-- First-party, privacy-preserving analytics. One row per recorded event (API
-- request, feed/export pull, or client-originated UI event). No raw PII: the
-- visitor is a salted daily hash (client_hash / ip_hash), so there is nothing to
-- consent-banner over. Written asynchronously by the API's analytics recorder.
CREATE TABLE analytics_event (
    id           bigserial PRIMARY KEY,
    ts           timestamptz NOT NULL DEFAULT now(),
    kind         text NOT NULL,              -- 'api' | 'feed' | 'client'
    event        text NOT NULL,              -- e.g. 'search.cheapest', 'charger.view', 'feed.pull', 'client.map_move'
    path         text NOT NULL DEFAULT '',   -- matched route template, never a raw id-bearing path
    status       int  NOT NULL DEFAULT 0,    -- HTTP status for api/feed events
    client_hash  text NOT NULL DEFAULT '',   -- salted daily hash of client id + ip (unique-visitor key)
    ip_hash      text NOT NULL DEFAULT '',   -- salted daily hash of ip only (consumer key)
    ua_class     text NOT NULL DEFAULT '',   -- 'browser' | 'bot' | 'api' | 'other'
    referer_host text NOT NULL DEFAULT '',
    props        jsonb                       -- event-specific: filters, result_count, region, format, …
);
CREATE INDEX analytics_event_ts_ix       ON analytics_event (ts DESC);
CREATE INDEX analytics_event_event_ts_ix ON analytics_event (event, ts DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS analytics_event;
-- +goose StatementEnd
