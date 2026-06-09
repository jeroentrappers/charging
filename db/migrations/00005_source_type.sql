-- +goose Up
-- +goose StatementBegin
-- A source feed is either OCPI (default) or DATEX II.
ALTER TABLE cpo ADD COLUMN source_type text NOT NULL DEFAULT 'ocpi';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE cpo DROP COLUMN source_type;
-- +goose StatementEnd
