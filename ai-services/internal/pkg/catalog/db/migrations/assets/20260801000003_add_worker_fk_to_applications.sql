-- +goose Up
-- +goose StatementBegin

-- Add worker_id FK to applications, referencing the workers table created in
-- migration 20260801000002. Nullable so existing applications without an
-- assigned worker remain valid.
ALTER TABLE applications
    ADD COLUMN worker_id UUID REFERENCES workers(id) ON DELETE SET NULL;

CREATE INDEX ON applications(worker_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS applications_worker_id_idx;

ALTER TABLE applications
    DROP COLUMN IF EXISTS worker_id;
-- +goose StatementEnd
