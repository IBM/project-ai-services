-- +goose Up
-- +goose StatementBegin
-- Create custom ENUM types for the catalog database

-- Status enum for applications
CREATE TYPE status AS ENUM (
    'Downloading',
    'Deploying',
    'Running',
    'Deleting',
    'Error'
);

-- Service status enum
CREATE TYPE service_status AS ENUM (
    'Initializing',
    'Running',
    'Error'
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop custom types in reverse order
DROP TYPE IF EXISTS service_status;
DROP TYPE IF EXISTS status;
-- +goose StatementEnd
