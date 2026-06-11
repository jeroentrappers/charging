-- +goose Up
-- +goose StatementBegin
-- The token we issue to a CPO during the OCPI credentials handshake, which they
-- present to push to us (Token B). The token we use to call them (Token C) is
-- the existing cpo.token. Secret — never serialized by the API.
ALTER TABLE cpo ADD COLUMN ocpi_token_in text;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cpo DROP COLUMN ocpi_token_in;
-- +goose StatementEnd
