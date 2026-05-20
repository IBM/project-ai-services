-- Database initialization script for summarize_jobs metadata
-- This script is idempotent and safe to run multiple times
-- Note: Column 'job_metadata' is used instead of 'metadata' to avoid SQLAlchemy reserved word conflict

CREATE TABLE IF NOT EXISTS summarize_jobs (
    job_id VARCHAR(255) PRIMARY KEY,
    job_name VARCHAR(500),
    status VARCHAR(50) NOT NULL,
    submitted_at TIMESTAMP WITH TIME ZONE NOT NULL,
    completed_at TIMESTAMP WITH TIME ZONE,
    error TEXT,
    document_name VARCHAR(500) NOT NULL,
    document_word_count INTEGER,
    level VARCHAR(20) NOT NULL DEFAULT 'standard',
    job_type VARCHAR(20) NOT NULL DEFAULT 'direct',
    job_metadata JSONB,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_summarize_job_status CHECK (status IN ('accepted', 'in_progress', 'completed', 'failed')),
    CONSTRAINT chk_summarize_job_level CHECK (level IN ('brief', 'standard', 'detailed')),
    CONSTRAINT chk_summarize_job_type CHECK (job_type IN ('direct', 'chunked'))
);

-- Create composite index for listing and filtering jobs
-- Supports queries like: ORDER BY submitted_at DESC WHERE status = 'completed'
CREATE INDEX IF NOT EXISTS idx_summarize_jobs_submitted_at_status 
    ON summarize_jobs(submitted_at DESC, status);

-- Create trigger function for auto-updating updated_at column (OR REPLACE makes it idempotent)
CREATE OR REPLACE FUNCTION update_summarize_jobs_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger with IF NOT EXISTS (PostgreSQL 14+)
-- For PostgreSQL < 14, use DROP TRIGGER IF EXISTS first
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_summarize_jobs_updated_at') THEN
        CREATE TRIGGER update_summarize_jobs_updated_at 
            BEFORE UPDATE ON summarize_jobs
            FOR EACH ROW 
            EXECUTE FUNCTION update_summarize_jobs_updated_at_column();
    END IF;
END
$$;

-- Note: Using postgres superuser, no additional grants needed

