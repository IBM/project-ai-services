-- +goose Up
-- +goose StatementBegin
-- Add a foreign key from service_dependencies.dependency_id to connectors(id)
-- for rows where dependency_type = 'connector'.
-- This enforces referential integrity so that deleting a connector that is still
-- referenced by at least one service_dependencies row raises error 23503, which
-- DeleteDatasource translates into a 409 Conflict response.
--
-- ON DELETE RESTRICT is intentional: deletion must be blocked, not cascaded.
ALTER TABLE service_dependencies
    ADD CONSTRAINT fk_dependency_connector
    FOREIGN KEY (dependency_id)
    REFERENCES connectors (id)
    ON DELETE RESTRICT
    NOT VALID;

-- Validate the constraint against existing rows separately so that the ALTER
-- TABLE above does not take an exclusive lock for a full table scan.
ALTER TABLE service_dependencies
    VALIDATE CONSTRAINT fk_dependency_connector;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE service_dependencies
    DROP CONSTRAINT IF EXISTS fk_dependency_connector;
-- +goose StatementEnd
