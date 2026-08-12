-- +goose Up
-- +goose StatementBegin
-- Add 'connector' value to the dependency_type enum.
-- IF NOT EXISTS is used defensively; Postgres does not allow removing enum values,
-- so this migration is intentionally irreversible.
ALTER TYPE dependency_type ADD VALUE IF NOT EXISTS 'connector';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- NOTE: Postgres does not support removing values from an enum type.
-- Rolling back this migration is a no-op; 'connector' will remain in dependency_type.
-- To fully revert, recreate the enum without 'connector' and migrate all dependent data,
-- which requires a bespoke manual process outside of goose.
-- +goose StatementEnd
