-- +goose Up
-- +goose StatementBegin
ALTER TABLE components
    ADD COLUMN name VARCHAR(100),
    ADD COLUMN created_by VARCHAR(100) NOT NULL;

ALTER TYPE component_status ADD VALUE IF NOT EXISTS 'Downloading';
ALTER TYPE component_status ADD VALUE IF NOT EXISTS 'Deploying';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE components
    DROP COLUMN IF EXISTS name,
    DROP COLUMN IF EXISTS created_by;
-- +goose StatementEnd
