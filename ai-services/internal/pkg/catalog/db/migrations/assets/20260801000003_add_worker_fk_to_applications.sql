-- +goose Up
-- +goose StatementBegin

-- Add worker_id FK to applications, referencing the workers table created in
-- migration 20260801000002. NOT NULL because every application is deployed
-- through a worker (the Local worker is used for local deployments).
-- ON DELETE RESTRICT prevents deregistering a worker that still has
-- applications assigned to it.
ALTER TABLE applications
    ADD COLUMN worker_id UUID NOT NULL REFERENCES workers(id) ON DELETE RESTRICT;

CREATE INDEX ON applications(worker_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS applications_worker_id_idx;

ALTER TABLE applications
    DROP COLUMN IF EXISTS worker_id;
-- +goose StatementEnd
