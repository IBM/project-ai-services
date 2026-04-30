-- +goose Up
-- +goose StatementBegin
-- Create token_type enum for tokens_blacklist table
CREATE TYPE token_type AS ENUM (
    'access',
    'refresh'
);

-- Create tokens_blacklist table
CREATE TABLE tokens_blacklist (
    id SERIAL PRIMARY KEY,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    token_type token_type NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop table and related type
DROP TABLE IF EXISTS tokens_blacklist;
DROP TYPE IF EXISTS token_type;
-- +goose StatementEnd
