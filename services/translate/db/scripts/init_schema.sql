-- Translation service — idempotent schema initialization
-- Safe to run multiple times (CREATE IF NOT EXISTS / CREATE OR REPLACE throughout).

CREATE TABLE IF NOT EXISTS translate_jobs (
    job_id              VARCHAR(255) PRIMARY KEY,
    job_name            VARCHAR(500),
    source_language     VARCHAR(100) NOT NULL DEFAULT 'auto',
    target_language     VARCHAR(100) NOT NULL,
    input_type          VARCHAR(20)  NOT NULL,
    document_name       VARCHAR(500),
    document_word_count INTEGER,
    status              VARCHAR(50)  NOT NULL,
    submitted_at        TIMESTAMP WITH TIME ZONE NOT NULL,
    completed_at        TIMESTAMP WITH TIME ZONE,
    error               TEXT,
    job_metadata        JSONB,
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_translate_job_status
        CHECK (status IN ('accepted','in_progress','completed','failed')),
    CONSTRAINT chk_translate_input_type
        CHECK (input_type IN ('text','txt','md'))
);

-- Composite index: job listing with status filter and boot-time zombie scan
CREATE INDEX IF NOT EXISTS idx_translate_jobs_submitted_at_status
    ON translate_jobs(submitted_at DESC, status);

-- Trigger function to keep updated_at current on every UPDATE
CREATE OR REPLACE FUNCTION update_translate_jobs_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Conditionally create the trigger (idempotent; requires PostgreSQL 14+ for IF NOT EXISTS on triggers)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger WHERE tgname = 'update_translate_jobs_updated_at'
    ) THEN
        CREATE TRIGGER update_translate_jobs_updated_at
            BEFORE UPDATE ON translate_jobs
            FOR EACH ROW
            EXECUTE FUNCTION update_translate_jobs_updated_at_column();
    END IF;
END
$$;
