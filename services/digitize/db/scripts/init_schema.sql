-- Database initialization script for digitize metadata
-- This script is idempotent and safe to run multiple times

CREATE TABLE IF NOT EXISTS jobs (
    job_id VARCHAR(255) PRIMARY KEY,
    job_name VARCHAR(500),
    operation VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    source VARCHAR(20) NOT NULL DEFAULT 'user',
    submitted_at TIMESTAMP WITH TIME ZONE NOT NULL,  -- When user submitted the job (UTC)
    completed_at TIMESTAMP WITH TIME ZONE,           -- When job finished processing (UTC)
    error TEXT,
    stats JSONB NOT NULL DEFAULT '{"total_documents": 0, "completed": 0, "failed": 0, "in_progress": 0}',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,  -- Last modification time (UTC)
    CONSTRAINT chk_job_status CHECK (status IN ('accepted', 'in_progress', 'completed', 'completed_with_errors', 'failed')),
    CONSTRAINT chk_job_operation CHECK (operation IN ('ingestion', 'digitization')),
    CONSTRAINT chk_job_source CHECK (source IN ('user', 'connector'))
);

CREATE TABLE IF NOT EXISTS documents (
    doc_id VARCHAR(255) PRIMARY KEY,
    job_id VARCHAR(255) REFERENCES jobs(job_id) ON DELETE SET NULL,
    name VARCHAR(500) NOT NULL,
    type VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    source VARCHAR(20) NOT NULL DEFAULT 'user',
    output_format VARCHAR(10) NOT NULL,
    submitted_at TIMESTAMP WITH TIME ZONE NOT NULL,  -- When user submitted the document as part of job (UTC)
    completed_at TIMESTAMP WITH TIME ZONE,           -- When document finished processing (UTC)
    error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,  -- Last modification time (UTC)
    CONSTRAINT chk_doc_status CHECK (status IN ('accepted', 'in_progress', 'digitized', 'processed', 'chunked', 'completed', 'completed_with_errors', 'failed', 'already_exists')),
    CONSTRAINT chk_doc_type CHECK (type IN ('ingestion', 'digitization')),
    CONSTRAINT chk_doc_source CHECK (source IN ('user', 'connector')),
    CONSTRAINT chk_output_format CHECK (output_format IN ('txt', 'md', 'json'))
);

-- Checksum registry — one row per unique file content, points to the
-- authoritative completed Document for duplicate detection.
-- ON DELETE CASCADE ensures stale registry entries are automatically removed
-- when the referenced document is deleted, preventing orphaned hash entries
-- from blocking future re-ingestion of the same file.
CREATE TABLE IF NOT EXISTS document_checksum (
    checksum      TEXT        PRIMARY KEY,
    doc_id        TEXT        NOT NULL UNIQUE REFERENCES documents(doc_id) ON DELETE CASCADE
);

-- Schema for APScheduler job store
-- APScheduler v3 SQLAlchemyJobStore attempts to auto-create its job store table
-- (default: 'apscheduler_jobs') under the 'scheduler' schema, but it cannot auto-create
-- the schema itself, which would cause an error on startup if the schema doesn't exist.
-- The job store is initialized in services/digitize/app.py via
-- SQLAlchemyJobStore(engine=db_engine, tableschema="scheduler")
CREATE SCHEMA IF NOT EXISTS scheduler;

-- Connector tables
CREATE TABLE IF NOT EXISTS connectors (
    id                      TEXT        PRIMARY KEY,
    name                    TEXT        NOT NULL UNIQUE,
    type                    TEXT        NOT NULL,
    connection_details      JSONB       NOT NULL DEFAULT '{}',
    allowed_extensions      JSONB       NOT NULL DEFAULT '[]',
    sync_interval_seconds   INTEGER     NOT NULL DEFAULT 300,
    attached_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_sync_at            TIMESTAMPTZ,
    status                  TEXT        NOT NULL DEFAULT 'up to date',
    total_files             INTEGER     NOT NULL DEFAULT 0,
    message                 TEXT,
    CONSTRAINT chk_connector_type CHECK (type IN ('file_system', 'object_storage'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_connectors_name
    ON connectors (name);

-- One row per (checksum, connector_id) pair — connector dedup and reference counting.
-- No FK constraints, no ON DELETE CASCADE — deletion is managed by application code.
CREATE TABLE IF NOT EXISTS connector_document_checksum (
    checksum     TEXT NOT NULL,
    connector_id TEXT NOT NULL,
    doc_id       TEXT NOT NULL,
    PRIMARY KEY (checksum, connector_id)
);

CREATE INDEX IF NOT EXISTS idx_cdc_connector_id
    ON connector_document_checksum (connector_id);

CREATE TABLE IF NOT EXISTS connector_sync_logs (
    connector_id     TEXT        NOT NULL,
    seq              INTEGER     NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    finished_at      TIMESTAMPTZ,
    total_files      INTEGER     NOT NULL DEFAULT 0,
    new_files        INTEGER     NOT NULL DEFAULT 0,
    completed_files   INTEGER     NOT NULL DEFAULT 0,
    removed_files    INTEGER     NOT NULL DEFAULT 0,
    status           TEXT        NOT NULL DEFAULT 'started',
    error            TEXT        NOT NULL DEFAULT '',
    CONSTRAINT pk_csl PRIMARY KEY (connector_id, seq),
    CONSTRAINT fk_csh_connector
        FOREIGN KEY (connector_id)
        REFERENCES connectors(id) ON DELETE CASCADE
);

-- conversion_tasks — Postgres-backed Docling conversion queue
-- Managed by Base.metadata.create_all on startup; this SQL entry keeps the
-- init_schema.sql reference in sync for direct DB initialisation.
CREATE TABLE IF NOT EXISTS conversion_tasks (
    task_id         VARCHAR(255)    PRIMARY KEY,
    -- link back to the digitize job that owns this task
    job_id          VARCHAR(255)    REFERENCES jobs(job_id) ON DELETE SET NULL,
    doc_id          VARCHAR(255),                           -- informational; no FK
    connector_id    VARCHAR(255),                           -- NULL for user jobs; connector UUID for connector jobs
    operation       VARCHAR(50)     NOT NULL,
    -- input
    cached_file     TEXT            NOT NULL,               -- absolute path at enqueue time
    output_format   VARCHAR(10)     NOT NULL,
    page_count      INTEGER,
    is_large        BOOLEAN         NOT NULL DEFAULT FALSE,
    -- lifecycle
    status          VARCHAR(50)     NOT NULL,
    result_path     TEXT,                                   -- written on completion
    error           TEXT,
    queued_at       TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    CONSTRAINT chk_ct_operation    CHECK (operation IN ('ingestion', 'digitization')),
    CONSTRAINT chk_ct_output_format CHECK (output_format IN ('json', 'md', 'txt')),
    CONSTRAINT chk_ct_status       CHECK (status IN ('pending', 'queued', 'running', 'completed', 'failed'))
);

-- Supports dispatcher pick query (ORDER BY queued_at per status + operation)
CREATE INDEX IF NOT EXISTS idx_ct_status_op_queued
    ON conversion_tasks (status, operation, queued_at);

-- Supports get_conversion_task_by_job_id — avoids full-table scans on poll
CREATE INDEX IF NOT EXISTS idx_ct_job_id
    ON conversion_tasks (job_id);

-- Supports dispatcher connector round-robin pick (turn 2: queued tasks per connector)
CREATE INDEX IF NOT EXISTS idx_ct_connector_queued
    ON conversion_tasks (connector_id, status, queued_at);

-- Create indexes with IF NOT EXISTS
CREATE INDEX IF NOT EXISTS idx_jobs_submitted_at_status ON jobs(submitted_at DESC, status);
CREATE INDEX IF NOT EXISTS idx_documents_job_id ON documents(job_id);
CREATE INDEX IF NOT EXISTS idx_documents_submitted_at_status ON documents(submitted_at DESC, status);
CREATE INDEX IF NOT EXISTS idx_csl_connector_started ON connector_sync_logs (connector_id, started_at DESC);

-- Create trigger function (OR REPLACE makes it idempotent)
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create triggers with IF NOT EXISTS (PostgreSQL 14+)
-- For PostgreSQL < 14, use DROP TRIGGER IF EXISTS first
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_jobs_updated_at') THEN
        CREATE TRIGGER update_jobs_updated_at BEFORE UPDATE ON jobs
            FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_documents_updated_at') THEN
        CREATE TRIGGER update_documents_updated_at BEFORE UPDATE ON documents
            FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
    END IF;
END
$$;

-- Note: Using postgres superuser, no additional grants needed
