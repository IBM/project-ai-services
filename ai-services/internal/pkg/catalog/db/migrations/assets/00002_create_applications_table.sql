-- +goose Up
-- +goose StatementBegin
-- Create applications table
CREATE TABLE applications (
    id VARCHAR(100) PRIMARY KEY,
    deployment_name VARCHAR(100),
    type VARCHAR(100),
    deployment_type deployment_type,
    status status,
    message TEXT,
    createdby VARCHAR(100),
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
-- +goose StatementEnd

