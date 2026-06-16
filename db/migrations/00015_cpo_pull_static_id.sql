-- +goose Up
-- Mobilithek static-reconcile pull. Push delivery is delta-based and can drop the
-- static/locations snapshot entirely (a source then receives only status deltas
-- and has zero chargers to attach them to). pull_static_id holds the Mobilithek
-- subscription id of a source's STATIC ("table") publication; the ingester pulls
-- it on a slow cadence over mutual-TLS and ingests it like a push, so locations +
-- ad-hoc prices stay complete regardless of what the push feed sends. Status is
-- left to push (a slow status pull would only be stale). NULL = don't reconcile.
-- +goose StatementBegin
ALTER TABLE cpo ADD COLUMN pull_static_id text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cpo DROP COLUMN pull_static_id;
-- +goose StatementEnd
