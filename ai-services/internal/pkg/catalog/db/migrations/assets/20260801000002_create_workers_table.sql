-- +goose Up
-- +goose StatementBegin

-- Worker runtime type enum
-- 'unknown' is the initial value set at pre-registration time; it is
-- replaced by the real runtime ('podman' | 'openshift') when the worker
-- connects and calls Register via gRPC.
CREATE TYPE worker_runtime_type AS ENUM (
    'unknown',
    'podman',
    'openshift'
);

-- Worker status enum
CREATE TYPE worker_status AS ENUM (
    'pending',
    'ready',
    'disconnected'
);

-- ── workers ────────────────────────────────────────────────────────────────────
-- One row per registered worker.
--
-- runtime_type:   execution environment the worker runs on.
--                 'unknown' until the worker connects and declares its runtime.
-- status:         lifecycle state of the worker (pending | ready | disconnected).
-- message:        human-readable reason for the current status, set by the catalog
--                 on status transitions.
-- last_heartbeat: timestamp of the most recent heartbeat received from the worker;
--                 NULL until the first heartbeat arrives.
-- registered_at:  when the worker first registered with the system.
-- updated_at:     timestamp of the last status or metadata change.
-- ──────────────────────────────────────────────────────────────────────────────
CREATE TABLE workers (
    id             UUID                PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT                NOT NULL UNIQUE,
    runtime_type   worker_runtime_type NOT NULL DEFAULT 'unknown',
    status         worker_status       NOT NULL DEFAULT 'pending',
    message        TEXT,
    last_heartbeat TIMESTAMPTZ,
    metadata       JSONB,
    registered_at  TIMESTAMPTZ         NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ         NOT NULL DEFAULT NOW()
);

CREATE INDEX ON workers(status);
CREATE INDEX ON workers(runtime_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS workers;
DROP TYPE IF EXISTS worker_status;
DROP TYPE IF EXISTS worker_runtime_type;
-- +goose StatementEnd
