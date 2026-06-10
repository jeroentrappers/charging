-- +goose Up
-- +goose StatementBegin
-- Optional DB-stored token so sources can be managed via the admin API/CLI
-- (takes precedence over the token_env fallback). Treat as a secret: never
-- returned by the API.
ALTER TABLE cpo ADD COLUMN token text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cpo DROP COLUMN token;
-- +goose StatementEnd
