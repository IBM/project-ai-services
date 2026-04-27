-- +goose Up
-- +goose StatementBegin
-- Create custom ENUM types for the catalog database

-- Deployment type enum for applications
CREATE TYPE deployment_type AS ENUM (
    'Deployable Architecture',
    'Services'
);

-- Status enum for applications and services
CREATE TYPE status AS ENUM (
    'Downloading',
    'Deploying',
    'Running',
    'Deleting',
    'Error'
);

-- Service category enum for services table
CREATE TYPE service_category AS ENUM (
    'Deployable Service',
    'Infrastructure'
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop custom types in reverse order
DROP TYPE IF EXISTS service_category;
DROP TYPE IF EXISTS status;
DROP TYPE IF EXISTS deployment_type;
-- +goose StatementEnd

-- Made with Bob
