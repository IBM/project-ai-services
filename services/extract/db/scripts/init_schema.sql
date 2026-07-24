-- Extract Information service — idempotent schema initialization
-- All statements use IF NOT EXISTS so this script is safe to re-run.

CREATE TABLE IF NOT EXISTS schemas (
    schema_id             VARCHAR(255) PRIMARY KEY,
    name                  VARCHAR(200) NOT NULL UNIQUE,
    description           TEXT,
    json_schema           JSONB NOT NULL,
    examples              JSONB,
    custom_prompt         TEXT,
    schema_tokens         INTEGER NOT NULL,
    examples_tokens       INTEGER NOT NULL DEFAULT 0,
    custom_prompt_tokens  INTEGER NOT NULL DEFAULT 0,
    created_at            TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Name lookup and UNIQUE enforcement
CREATE INDEX IF NOT EXISTS idx_schemas_name ON schemas(name);

CREATE TABLE IF NOT EXISTS extract_jobs (
    job_id              VARCHAR(255) PRIMARY KEY,
    job_name            VARCHAR(500),
    schema_id           VARCHAR(255) NOT NULL
                            REFERENCES schemas(schema_id) ON DELETE RESTRICT,
    status              VARCHAR(50)  NOT NULL,
    submitted_at        TIMESTAMP WITH TIME ZONE NOT NULL,
    completed_at        TIMESTAMP WITH TIME ZONE,
    error               TEXT,
    document_name       VARCHAR(500) NOT NULL,
    source_type         VARCHAR(10)  NOT NULL,
    document_word_count INTEGER,
    digitize_job_id     VARCHAR(255),
    digitize_doc_id     VARCHAR(255),
    metadata            JSONB,
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_extract_job_status
        CHECK (status IN ('accepted', 'in_progress', 'completed', 'failed')),
    CONSTRAINT chk_extract_source_type
        CHECK (source_type IN ('txt', 'pdf'))
);

CREATE INDEX IF NOT EXISTS idx_extract_jobs_submitted_at_status
    ON extract_jobs(submitted_at DESC, status);

CREATE INDEX IF NOT EXISTS idx_extract_jobs_schema_id
    ON extract_jobs(schema_id);

-- updated_at auto-maintenance trigger (jobs only — schemas are immutable after INSERT)
CREATE OR REPLACE FUNCTION update_extract_jobs_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE 'plpgsql';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'update_extract_jobs_updated_at'
    ) THEN
        CREATE TRIGGER update_extract_jobs_updated_at
            BEFORE UPDATE ON extract_jobs
            FOR EACH ROW
            EXECUTE FUNCTION update_extract_jobs_updated_at_column();
    END IF;
END
$$;

-- Immutability defense-in-depth:
-- The application role (extract_user) is explicitly denied UPDATE on schemas.
-- Any accidental update path will fail at the database layer regardless of
-- what the application layer allows.
--
-- NOTE: These GRANT statements are no-ops if the role does not exist yet, and
-- safe to re-run (PostgreSQL GRANT is idempotent for already-granted privileges).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'extract_user') THEN
        EXECUTE 'GRANT SELECT, INSERT, DELETE ON schemas      TO extract_user';
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON extract_jobs TO extract_user';
    END IF;
END
$$;
