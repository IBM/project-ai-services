-- +goose Up
-- +goose StatementBegin
ALTER TABLE components
    ADD COLUMN worker_id UUID REFERENCES workers(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE components
    DROP COLUMN IF EXISTS worker_id;
-- +goose StatementEnd
