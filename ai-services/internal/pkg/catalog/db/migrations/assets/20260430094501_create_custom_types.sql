-- +goose Up
-- +goose StatementBegin
-- Create custom ENUM types for the catalog database

-- Status enum for applications and services
CREATE TYPE status AS ENUM (
    'Downloading',
    'Deploying',
    'Running',
    'Deleting',
    'Error'
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop custom types in reverse order
DROP TYPE IF EXISTS status;
-- +goose StatementEnd
