-- +goose Up
-- +goose StatementBegin
-- Create connector_status enum for the connectors table
CREATE TYPE connector_status AS ENUM (
    'Connected',
    'Offline'
);

-- Create connectors table
CREATE TABLE connectors (
    id         UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(255)     NOT NULL,
    type       VARCHAR(64)      NOT NULL,
    provider   VARCHAR(64)      NOT NULL,
    status     connector_status NOT NULL DEFAULT 'Offline',
    message    TEXT,
    metadata   JSONB            NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

-- Indexes for common filter/lookup patterns
CREATE INDEX idx_connectors_type     ON connectors (type);
CREATE INDEX idx_connectors_provider ON connectors (provider);
CREATE INDEX idx_connectors_status   ON connectors (status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop indexes first, then the table, then the enum
DROP INDEX IF EXISTS idx_connectors_status;
DROP INDEX IF EXISTS idx_connectors_provider;
DROP INDEX IF EXISTS idx_connectors_type;
DROP TABLE IF EXISTS connectors;
DROP TYPE IF EXISTS connector_status;
-- +goose StatementEnd
