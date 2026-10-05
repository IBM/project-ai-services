-- Invoice Processing service — idempotent schema initialization
-- All statements use IF NOT EXISTS so this script is safe to re-run.

CREATE TABLE IF NOT EXISTS invoice_jobs (
    job_id              VARCHAR(255) PRIMARY KEY,
    filename            VARCHAR(500) NOT NULL,
    input_type          VARCHAR(50)  NOT NULL,
    pipeline_path       VARCHAR(50),
    status              VARCHAR(50)  NOT NULL,
    submitted_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at        TIMESTAMP WITH TIME ZONE,
    updated_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    error               TEXT,
    digitize_job_id     VARCHAR(255),
    extract_job_id      VARCHAR(255),
    staged_header       JSONB,
    staged_lines        JSONB,
    interface_ref       VARCHAR(255),
    metadata            JSONB,
    CONSTRAINT chk_invoice_job_status
        CHECK (status IN ('accepted', 'routing', 'digitizing', 'extracting', 'staging', 'review', 'loading', 'completed', 'rejected', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_invoice_jobs_submitted_at_status
    ON invoice_jobs(submitted_at DESC, status);

CREATE INDEX IF NOT EXISTS idx_invoice_jobs_status
    ON invoice_jobs(status);

-- updated_at auto-maintenance trigger
CREATE OR REPLACE FUNCTION update_invoice_jobs_updated_at_column()
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
        WHERE tgname = 'update_invoice_jobs_updated_at'
    ) THEN
        CREATE TRIGGER update_invoice_jobs_updated_at
            BEFORE UPDATE ON invoice_jobs
            FOR EACH ROW
            EXECUTE FUNCTION update_invoice_jobs_updated_at_column();
    END IF;
END
$$;

-- Grant permissions if invoice_user role exists
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'invoice_user') THEN
        EXECUTE 'GRANT SELECT, INSERT, UPDATE, DELETE ON invoice_jobs TO invoice_user';
    END IF;
END
$$;
