-- +goose Up
-- +goose StatementBegin
-- Create dependency_type enum for service_dependencies
CREATE TYPE dependency_type AS ENUM (
    'service',
    'component'
);

-- Create components table
CREATE TABLE components (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type VARCHAR(100),
    provider VARCHAR(100),
    endpoints JSONB,
    version TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create trigger to automatically update updated_at timestamp
CREATE TRIGGER update_components_updated_at
    BEFORE UPDATE ON components
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop trigger
DROP TRIGGER IF EXISTS update_components_updated_at ON components;

-- Drop table
DROP TABLE IF EXISTS components;

-- Drop enum type
DROP TYPE IF EXISTS dependency_type;
-- +goose StatementEnd

-- Made with Bob
