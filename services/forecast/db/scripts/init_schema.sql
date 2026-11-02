-- Database initialization script for forecast metadata
-- This script is idempotent and safe to run multiple times.
-- The restricted forecast_worker role and its grants are created by init_db.py,
-- because the role name and password come from the environment.

-- Schema for the APScheduler job store.
-- SQLAlchemyJobStore creates its own table under this schema but cannot create
-- the schema itself, so it must exist before the API starts.
CREATE SCHEMA IF NOT EXISTS scheduler;

-- Connectors: connection details of one source database.
-- Safe fields are stored in plaintext inside config, secret fields are encrypted.
CREATE TABLE IF NOT EXISTS connectors (
    id          UUID         PRIMARY KEY,
    name        VARCHAR(255) NOT NULL UNIQUE,
    type        TEXT         NOT NULL,
    config      JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,  -- Last modification time (UTC)
    status      TEXT         NOT NULL,
    message     TEXT
);

-- Training details given when the connector was created. Removed with the connector.
CREATE TABLE IF NOT EXISTS training_configs (
    connector_id   UUID        PRIMARY KEY REFERENCES connectors(id) ON DELETE CASCADE,
    source_table   TEXT        NOT NULL,
    target_column  TEXT        NOT NULL,
    input_columns  JSONB       NOT NULL,  -- [{"name": ..., "type": ...}, ...]
    created_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Training jobs, also the run history. seq is the forecaster version: the database
-- assigns it (GENERATED ALWAYS), so neither the API nor the worker can pick one.
-- Deleting a connector sets connector_id to NULL on its jobs. For a queued or
-- running job that violates chk_jobs_active_has_connector, so the delete fails.
CREATE TABLE IF NOT EXISTS jobs (
    id            UUID        PRIMARY KEY,
    seq           BIGINT      GENERATED ALWAYS AS IDENTITY,
    connector_id  UUID        REFERENCES connectors(id) ON DELETE SET NULL,
    status        TEXT        NOT NULL,
    message       TEXT,
    result        JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,  -- When the job was queued (UTC)
    started_at    TIMESTAMPTZ,                                     -- When the job started running (UTC)
    finished_at   TIMESTAMPTZ,                                     -- When the job completed or failed (UTC)
    CONSTRAINT chk_jobs_status CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    CONSTRAINT chk_jobs_active_has_connector CHECK (status NOT IN ('queued', 'running') OR connector_id IS NOT NULL)
);

-- At most one running job: a second row with status 'running' violates the unique index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_single_running
    ON jobs (status) WHERE status = 'running';

-- Supports the per-connector active-job check and run-history pruning.
CREATE INDEX IF NOT EXISTS idx_jobs_connector_created
    ON jobs (connector_id, created_at DESC);

-- Forecasters: one row per forecaster loaded into Triton.
-- name is fc_<connector_id>_<version>, the directory on the volume and the Triton model name.
-- The row outlives its job: deleting an old job only clears job_id.
CREATE TABLE IF NOT EXISTS forecasters (
    id          UUID        PRIMARY KEY,
    job_id      UUID        REFERENCES jobs(id) ON DELETE SET NULL,
    version     BIGINT      NOT NULL,
    name        TEXT        NOT NULL UNIQUE,
    status      TEXT        NOT NULL,
    metrics     JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_forecasters_status CHECK (status IN ('ready', 'retiring', 'deleted'))
);

-- Trigger function to keep connectors.updated_at current on every UPDATE
CREATE OR REPLACE FUNCTION update_connectors_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_connectors_updated_at') THEN
        CREATE TRIGGER update_connectors_updated_at BEFORE UPDATE ON connectors
            FOR EACH ROW EXECUTE FUNCTION update_connectors_updated_at_column();
    END IF;
END
$$;
