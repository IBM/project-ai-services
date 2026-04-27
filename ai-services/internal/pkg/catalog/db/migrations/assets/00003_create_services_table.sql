-- +goose Up
-- +goose StatementBegin
-- Create services table
CREATE TABLE services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_id VARCHAR(100) NOT NULL,
    type VARCHAR(100),
    category service_category,
    status status,
    endpoints JSONB,
    version TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    CONSTRAINT fk_app_id FOREIGN KEY (app_id) REFERENCES applications(id) ON DELETE CASCADE
);

-- Create trigger to automatically update updated_at timestamp
CREATE TRIGGER update_services_updated_at
    BEFORE UPDATE ON services
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Drop trigger
DROP TRIGGER IF EXISTS update_services_updated_at ON services;

-- Drop table
DROP TABLE IF EXISTS services;
-- +goose StatementEnd

-- Made with Bob
