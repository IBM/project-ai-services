-- +goose Up
-- +goose StatementBegin
-- Create deployment_type enum
CREATE TYPE deployment_type AS ENUM (
    'architectures',
    'services'
);

-- Create applications table
CREATE TABLE applications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100),
    catalog_id VARCHAR(100),
    deployment_type deployment_type,
    status status,
    message TEXT,
    version VARCHAR(50),
    created_by VARCHAR(100),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create trigger to automatically update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_applications_updated_at
    BEFORE UPDATE ON applications
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop trigger and function
DROP TRIGGER IF EXISTS update_applications_updated_at ON applications;
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop table
DROP TABLE IF EXISTS applications;

-- Drop deployment_type enum
DROP TYPE IF EXISTS deployment_type;
-- +goose StatementEnd

