-- +goose Up
-- +goose StatementBegin
-- Community feedback on chargers: structured (typed) reports only, never free
-- text. One row per (charger, type, client) — so a client can hold at most one
-- active report of each type per charger; re-submitting refreshes it. Counts of
-- distinct clients give corroboration; recency + per-type TTL (applied in code)
-- give the active set. This is a community signal shown ALONGSIDE operator data,
-- never overwriting it.
CREATE TABLE charger_report (
    charger_id  bigint      NOT NULL REFERENCES charger(id) ON DELETE CASCADE,
    type        text        NOT NULL,             -- registry key (internal/report)
    client_hash text        NOT NULL,             -- hashed client id + IP (no raw PII)
    value       jsonb,                            -- type-specific: {"close":"22:00"} | {"kw":50} | {"price":0.55}
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (charger_id, type, client_hash)
);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX charger_report_lookup ON charger_report (charger_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE charger_report;
-- +goose StatementEnd
