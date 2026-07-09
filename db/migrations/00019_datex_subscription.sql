-- +goose Up
-- +goose StatementBegin
-- Self-service DATEX II push subscriptions. External consumers register their
-- own callback URL (proving ownership via a challenge-echo handshake, done in
-- the API before the row is inserted), and the export snapshotter POSTs each
-- freshly generated publication to every active subscription. This is the
-- outbound mirror of the inbound Mobilithek push we consume.
CREATE TABLE datex_subscription (
    id            bigserial PRIMARY KEY,
    callback_url  text NOT NULL,
    -- Encoding the subscriber wants delivered: 'xml' or 'json'.
    encoding      text NOT NULL DEFAULT 'xml',
    -- Optional bearer WE send on each push (the subscriber's own inbound token).
    push_token    text NOT NULL DEFAULT '',
    -- Secret returned once at registration; required to delete the subscription.
    manage_secret text NOT NULL,
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    verified_at   timestamptz,
    -- One subscription per (url, encoding); re-registering refreshes it.
    UNIQUE (callback_url, encoding)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS datex_subscription;
-- +goose StatementEnd
