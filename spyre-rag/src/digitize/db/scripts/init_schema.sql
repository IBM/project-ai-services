-- Database initialization script for digitize metadata
-- This script is idempotent and safe to run multiple times

CREATE TABLE IF NOT EXISTS jobs (
    job_id VARCHAR(255) PRIMARY KEY,
    job_name VARCHAR(500),
    operation VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    submitted_at TIMESTAMP NOT NULL,  -- When user submitted the job
    completed_at TIMESTAMP,           -- When job finished processing
    error TEXT,
    stats JSONB NOT NULL DEFAULT '{"total_documents": 0, "completed": 0, "failed": 0, "in_progress": 0}',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,  -- Last modification time
    CONSTRAINT chk_job_status CHECK (status IN ('accepted', 'in_progress', 'completed', 'failed')),
    CONSTRAINT chk_job_operation CHECK (operation IN ('ingestion', 'digitization'))
);

CREATE TABLE IF NOT EXISTS documents (
    doc_id VARCHAR(255) PRIMARY KEY,
    job_id VARCHAR(255) REFERENCES jobs(job_id) ON DELETE CASCADE,
    name VARCHAR(500) NOT NULL,
    type VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    output_format VARCHAR(10) NOT NULL,
    submitted_at TIMESTAMP NOT NULL,  -- When user submitted the document as part of job
    completed_at TIMESTAMP,           -- When document finished processing
    error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,  -- Last modification time
    CONSTRAINT chk_doc_status CHECK (status IN ('accepted', 'in_progress', 'digitized', 'processed', 'chunked', 'completed', 'failed')),
    CONSTRAINT chk_doc_type CHECK (type IN ('ingestion', 'digitization')),
    CONSTRAINT chk_output_format CHECK (output_format IN ('txt', 'md', 'json'))
);

-- Create indexes with IF NOT EXISTS
CREATE INDEX IF NOT EXISTS idx_jobs_submitted_at_status ON jobs(submitted_at DESC, status);
CREATE INDEX IF NOT EXISTS idx_documents_job_id ON documents(job_id);
CREATE INDEX IF NOT EXISTS idx_documents_submitted_at_status ON documents(submitted_at DESC, status);

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
