-- +goose Up
-- +goose StatementBegin
-- Create dependency_type enum for service_dependencies
CREATE TYPE dependency_type AS ENUM (
    'service',
    'component'
);

-- Create service_dependencies table
CREATE TABLE service_dependencies (
    service_id UUID NOT NULL,
    dependency_id UUID NOT NULL,
    dependency_type dependency_type NOT NULL,
    PRIMARY KEY (service_id, dependency_id),
    CONSTRAINT fk_service_id FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop table
DROP TABLE IF EXISTS service_dependencies;

-- Drop enum type
DROP TYPE IF EXISTS dependency_type;
-- +goose StatementEnd
