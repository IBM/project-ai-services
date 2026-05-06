# PostgreSQL Migration for Digitize Service Metadata Storage

## Executive Summary

This document outlines the migration plan from JSON file-based metadata storage to PostgreSQL **specifically for the digitize service**. PostgreSQL has been selected as the database solution to address current scalability challenges while providing ACID guarantees, excellent concurrent write performance, and flexible JSON support for evolving metadata schemas.

**Scope**: This proposal covers only the digitize service. Other services (summarize, similarity, etc.) will have their own separate databases and schemas, following similar patterns but with service-specific requirements.

## Current Implementation Analysis

### Data Models

#### 1. DocumentMetadata Structure
```python
{
    "id": str,                    # Unique document identifier
    "name": str,                  # Original filename
    "type": str,                  # Operation type (ingestion/digitization)
    "status": DocStatus,          # Enum: accepted, in_progress, digitized, processed, chunked, completed, failed
    "output_format": OutputFormat, # Enum: txt, md, json
    "submitted_at": str,          # ISO 8601 timestamp
    "completed_at": str | None,   # ISO 8601 timestamp
    "error": str | None,          # Error message if failed
    "job_id": str | None,         # Parent job reference
    "metadata": {                 # Nested metadata object
        "pages": int,
        "tables": int,
        "chunks": int,
        "timing_in_secs": {
            "digitizing": float | None,
            "processing": float | None,
            "chunking": float | None,
            "indexing": float | None
        }
    }
}
```

**Current Storage**: `{doc_id}_metadata.json` in `/var/cache/docs/`

#### 2. JobState Structure
```python
{
    "job_id": str,                # Unique job identifier
    "job_name": str | None,       # Optional human-readable name
    "operation": str,             # Operation type
    "status": JobStatus,          # Enum: accepted, in_progress, completed, failed
    "submitted_at": str,          # ISO 8601 timestamp
    "completed_at": str | None,   # ISO 8601 timestamp
    "documents": [                # Array of document summaries
        {
            "id": str,
            "name": str,
            "status": str
        }
    ],
    "stats": {                    # Aggregated statistics
        "total_documents": int,
        "completed": int,
        "failed": int,
        "in_progress": int
    },
    "error": str | None           # Error message if failed
}
```

**Current Storage**: `{job_id}_status.json` in `/var/cache/jobs/`

### Current Limitations

1. **Scalability Issues**
   - File system I/O bottlenecks with high document volumes
   - No built-in indexing for queries
   - Linear scan required for filtering/searching
   - File locking contention in concurrent scenarios

2. **Query Limitations**
   - Cannot efficiently query by status, date ranges, or metadata fields
   - Pagination requires loading all files

3. **Reliability Concerns**
   - Atomic writes require temp file + rename pattern
   - Retry logic needed for transient failures
   - No ACID guarantees across multiple documents
   - Risk of partial updates on crashes

4. **Operational Overhead**
   - Manual file cleanup required
   - No built-in backup/restore mechanisms
   - Difficult to monitor storage usage
   - No query optimization capabilities

### Access Patterns

Based on existing code analysis, the system requires:

1. **Write Operations** (High Frequency)
   - Create document metadata on job submission
   - Update document status through pipeline stages
   - Update timing metrics incrementally
   - Update job statistics after each document change

2. **Read Operations** (Medium Frequency)
   - Fetch single document metadata by ID
   - Fetch single job status by ID
   - List documents with pagination and filtering
   - List jobs with pagination and filtering
   - Aggregate job statistics

3. **Concurrency Requirements**
   - Multiple documents processed in parallel (4-32 workers)
   - Concurrent updates to different documents
   - Thread-safe updates to job statistics
   - Real-time status tracking

---

## PostgreSQL as Database Solution

### Why PostgreSQL?

1. **Strong ACID Guarantees**
   - Transactional consistency across related updates
   - Reliable concurrent access with row-level locking
   - No data loss on crashes

2. **Excellent Query Performance**
   - B-tree indexes for fast lookups by ID, status, dates
   - GIN indexes for efficient JSONB queries
   - Query planner optimizes complex queries automatically

3. **Flexible Schema Evolution**
   - JSONB columns allow schema flexibility within structured tables
   - Can add new fields without migrations
   - Supports complex nested queries on JSON data

4. **Rich Querying Capabilities**
   - SQL aggregations for statistics
   - Complex filtering and sorting
   - Efficient pagination with OFFSET/LIMIT
   - Full-text search capabilities

5. **Mature Ecosystem**
   - Excellent Python libraries (psycopg2, SQLAlchemy, asyncpg)
   - Built-in replication and backup tools
   - Extensive monitoring and optimization tools
   - Large community and documentation

6. **Data Integrity**
   - Foreign key constraints ensure referential integrity
   - Check constraints for data validation
   - Triggers for automatic updates

7. **Architecture Support**
   - **Native ppc64le support**: PostgreSQL is fully supported on ppc64le architecture
   - **IBM Power Systems compatibility**: Regularly updated PostgreSQL images available in the `icr.io/ppc64le-oss` registry

### Performance Characteristics

- **Write Latency**: 1-5ms per document update (with proper indexing)
- **Read Latency**: <1ms for single document lookup
- **Concurrent Writes**: Excellent (row-level locking)
- **Scalability**: Handles millions of documents efficiently
- **Query Complexity**: Supports complex aggregations and joins

---

## Database Schema Design

### Entity-Relationship Model

```
┌─────────────────────────────────────────────────────────────────┐
│                         JOBS TABLE                              │
├─────────────────────────────────────────────────────────────────┤
│ PK  job_id          VARCHAR(255)                                │
│     job_name        VARCHAR(500)         NULL                   │
│     operation       VARCHAR(50)          NOT NULL               │
│     status          VARCHAR(50)          NOT NULL               │
│     submitted_at    TIMESTAMP WITH TZ    NOT NULL               │
│     completed_at    TIMESTAMP WITH TZ    NULL                   │
│     error           TEXT                 NULL                   │
│     stats           JSONB                NOT NULL               │
│     created_at      TIMESTAMP WITH TZ    DEFAULT NOW()          │
│     updated_at      TIMESTAMP WITH TZ    DEFAULT NOW()          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ 1
                              │
                              │ has many
                              │
                              │ N
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      DOCUMENTS TABLE                            │
├─────────────────────────────────────────────────────────────────┤
│ PK  doc_id          VARCHAR(255)                                │
│ FK  job_id          VARCHAR(255)         NULL → jobs.job_id     │
│     name            VARCHAR(500)         NOT NULL               │
│     type            VARCHAR(50)          NOT NULL               │
│     status          VARCHAR(50)          NOT NULL               │
│     output_format   VARCHAR(10)          NOT NULL               │
│     submitted_at    TIMESTAMP WITH TZ    NOT NULL               │
│     completed_at    TIMESTAMP WITH TZ    NULL                   │
│     error           TEXT                 NULL                   │
│     metadata        JSONB                NOT NULL               │
│     created_at      TIMESTAMP WITH TZ    DEFAULT NOW()          │
│     updated_at      TIMESTAMP WITH TZ    DEFAULT NOW()          │
└─────────────────────────────────────────────────────────────────┘

RELATIONSHIP:
• One Job can have MANY Documents (1:N)
• Each Document belongs to ONE Job
• Foreign Key: documents.job_id → jobs.job_id
• Cascade: ON DELETE CASCADE (deleting a job deletes its documents)

INDEXES:
• idx_jobs_submitted_at_status (jobs.submitted_at, jobs.status)
• idx_documents_job_id (documents.job_id)
• idx_documents_submitted_at_status (documents.submitted_at, documents.status)
```

### Relationship Details

**Benefits of This Design**:
1. **Data Integrity**: Foreign key ensures documents always reference valid jobs
2. **Efficient Queries**: Can fetch all documents for a job via indexed `job_id`
3. **Automatic Cleanup**: Cascade delete prevents orphaned documents
4. **Bidirectional Navigation**: SQLAlchemy relationships enable easy traversal in both directions

---

## ORM Layer: SQLAlchemy

### Why SQLAlchemy?

**SQLAlchemy** is the recommended ORM for this project because:

1. ✅ **Industry Standard**: Most widely adopted Python ORM (used by Flask, FastAPI, etc.)
2. ✅ **Database Agnostic**: Easy to switch databases if needed (PostgreSQL → MySQL → SQLite)
3. ✅ **Type Safety**: Works well with Pydantic models already in use
4. ✅ **Flexible**: Supports both ORM and raw SQL when needed
5. ✅ **Connection Pooling**: Built-in connection pool management
6. ✅ **Migration Support**: Works with Alembic for schema migrations
7. ✅ **Active Development**: Well-maintained with excellent documentation

### ORM Layer Implementation

**SQLAlchemy Models** (`models.py`):
- `Job` model mapping to `jobs` table
- `Document` model mapping to `documents` table
- Bidirectional relationship: `Job.documents` ↔ `Document.job`
- Cascade delete: Deleting job removes all its documents
- Check constraints for status/type validation
- Indexes for query optimization

**Database Session Management** (`database.py`):
- SQLAlchemy engine with connection pooling (QueuePool)
- Scoped session factory for thread-safety
- Environment-based configuration
- Connection pool settings: pool_size, max_overflow, pool_pre_ping

**Key Configuration:**
- Database name: `digitize_metadata` (service-specific)
- Pool size: 5 connections (configurable via `DB_POOL_SIZE`)
- Max overflow: 5 additional connections (configurable via `DB_MAX_OVERFLOW`)
- Pool pre-ping: Verify connections before use
- No `init_db()` function needed - schema created by init container

**Service Isolation:**
- Each service (digitize, summarize, similarity) uses its own database
- Database naming convention: `{service_name}_metadata`
- Separate schemas and ORM models per service
- No shared tables between services

### Benefits of ORM Layer:

1. **Type Safety**: SQLAlchemy models provide type hints and validation
2. **Automatic SQL Generation**: No need to write raw SQL queries
3. **Relationship Management**: Automatic handling of foreign keys and joins
4. **Connection Pooling**: Built-in pool management
5. **Database Agnostic**: Easy to switch databases if needed
6. **Migration Support**: Alembic integration for schema changes
7. **Query Building**: Pythonic query construction
8. **Transaction Management**: Automatic commit/rollback

---

## Database Schema Design

### Tables

```sql
-- Jobs table
CREATE TABLE jobs (
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

-- Documents table
CREATE TABLE documents (
    doc_id VARCHAR(255) PRIMARY KEY,
    job_id VARCHAR(255) REFERENCES jobs(job_id) ON DELETE CASCADE,
    name VARCHAR(500) NOT NULL,
    type VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    output_format VARCHAR(10) NOT NULL,
    submitted_at TIMESTAMP NOT NULL,  -- When user submitted the document
    completed_at TIMESTAMP,           -- When document finished processing
    error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,  -- Last modification time
    CONSTRAINT chk_doc_status CHECK (status IN ('accepted', 'in_progress', 'digitized', 'processed', 'chunked', 'completed', 'failed')),
    CONSTRAINT chk_doc_type CHECK (type IN ('ingestion', 'digitization')),
    CONSTRAINT chk_output_format CHECK (output_format IN ('txt', 'md', 'json'))
);
```

**Timestamp Design Decision:**

**Removed `created_at`, kept `submitted_at` and `updated_at`:**

- ✅ `submitted_at` - Business timestamp (when user submitted job/document)
  - Meaningful for business logic and reporting
  - Used for sorting, filtering, and analytics
  - Set once at creation, never changes

- ✅ `updated_at` - Technical timestamp (last modification time)
  - Tracks when record was last modified
  - Useful for debugging and audit trails
  - Automatically updated by trigger on every UPDATE

- ❌ `created_at` - REMOVED (redundant)
  - Would be identical to `submitted_at` in our use case
  - No delay between submission and record creation
  - Adds confusion without value
  - Wastes storage space

**Why this works:**
- Job/document submission and database record creation happen atomically
- No queuing or delay between user action and database insert
- `submitted_at` serves both business and technical purposes
- `updated_at` tracks subsequent modifications

### Indexes

**Index Strategy**: Start with essential indexes only. Add more indexes based on actual query patterns and performance monitoring.

```sql
-- Essential Job indexes
-- Primary lookup: Get job by ID (covered by PRIMARY KEY)
-- Common query: List jobs ordered by submission time with optional status filter
CREATE INDEX idx_jobs_submitted_at_status ON jobs(submitted_at DESC, status);

-- Essential Document indexes
-- Primary lookup: Get document by ID (covered by PRIMARY KEY)
-- Critical: Find all documents for a job (foreign key relationship)
CREATE INDEX idx_documents_job_id ON documents(job_id);

-- Common query: List documents ordered by submission time with optional status filter
CREATE INDEX idx_documents_submitted_at_status ON documents(submitted_at DESC, status);

-- Optional indexes (add only if needed based on query patterns)
-- CREATE INDEX idx_documents_status ON documents(status);

-- GIN indexes for JSONB queries (only if you query metadata fields frequently)
-- These are expensive to maintain - add only when needed
-- CREATE INDEX idx_documents_metadata ON documents USING GIN (metadata);
-- CREATE INDEX idx_jobs_stats ON jobs USING GIN (stats);
```

**Index Trade-offs:**
- **Write Performance**: Each index adds ~5-10% overhead to INSERT/UPDATE operations
- **Storage**: Each index consumes additional disk space (typically 20-30% of table size)
- **Read Performance**: Proper indexes can improve query speed by 10-1000x

**Recommendation**: Start with the 3 essential indexes above. Monitor query performance using `pg_stat_statements` and add indexes only when:
1. A specific query is slow (>100ms)
2. The query is executed frequently (>100 times/day)
3. Adding an index provides measurable improvement

### Triggers

**Purpose**: Automatically maintain audit timestamps without requiring application code changes.

```sql
-- Trigger for automatic updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_jobs_updated_at BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_documents_updated_at BEFORE UPDATE ON documents
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
```

**How It Works:**
1. **Trigger Function**: `update_updated_at_column()` is a reusable function that sets `updated_at` to the current timestamp
2. **Trigger Activation**: Fires automatically BEFORE any UPDATE operation on jobs or documents tables
3. **Automatic Execution**: No application code needed - the database handles it transparently

**Benefits:**
- ✅ **Consistency**: Every update is guaranteed to have correct timestamp, regardless of which code path updates the record
- ✅ **Simplicity**: Application code doesn't need to remember to set `updated_at` field
- ✅ **Audit Trail**: Provides reliable tracking of when records were last modified
- ✅ **Debugging**: Helps identify stale data or troubleshoot update issues

**Use Cases:**
```sql
-- Application code just updates the fields it cares about
UPDATE documents SET status = 'completed' WHERE doc_id = 'abc123';
-- Trigger automatically sets updated_at = CURRENT_TIMESTAMP

-- Even bulk updates get timestamps
UPDATE documents SET status = 'failed' WHERE job_id = 'job_456';
-- Each affected row gets its updated_at set automatically
```

---

## Migration Strategy

### Phase 1: Preparation (Week 1)

#### 1.1 Database Setup

- Provision PostgreSQL instance (version 13+)
- Configure connection pooling (recommended: pgBouncer or built-in pooling)
- Configure automated backups

**Configuration:**
```python
# Database connection settings
DB_HOST = os.getenv("POSTGRES_HOST", "localhost")
DB_PORT = int(os.getenv("POSTGRES_PORT", "5432"))
DB_NAME = os.getenv("POSTGRES_DB", "digitize_metadata")
DB_USER = os.getenv("POSTGRES_USER", "digitize_user")
DB_PASSWORD = os.getenv("POSTGRES_PASSWORD")

# Connection pool settings - Recommended but not critical
DB_POOL_SIZE = int(os.getenv("DB_POOL_SIZE", "5"))
DB_MAX_OVERFLOW = int(os.getenv("DB_MAX_OVERFLOW", "5"))
```

**Connection Pooling Analysis:**

**Actual Database Access Pattern:**
- Workers (4-32 concurrent) do **NOT** directly access database
- All database updates go through **single StatusManager instance per job**
- StatusManager uses **locks to serialize database access**
- Result: Database updates are **sequential, not concurrent**

**Do We Need Connection Pooling?**

✅ **Yes, but minimal** - Here's why:

**Without Connection Pool:**
- Each status update creates new connection (~50-100ms overhead)
- Frequent updates (every pipeline stage: digitized → processed → chunked → completed)
- Connection creation overhead adds up over many documents

**With Small Connection Pool (5-10 connections):**
- Connections reused across updates (<1ms to acquire)
- Eliminates connection creation overhead
- Minimal resource usage

**Recommended Settings:**

```python
# Minimal pool (sufficient for most cases)
DB_POOL_SIZE = 5      # Keep 5 connections ready
DB_MAX_OVERFLOW = 5   # Allow 5 more if needed (total max: 10)
```

**Why Small Pool is Sufficient:**

1. **Sequential Updates**: StatusManager serializes database access with locks
2. **Low Concurrency**: Even with 32 workers, only 1-2 database operations happen simultaneously per job
3. **Multiple Jobs**: If running multiple jobs concurrently, pool handles them efficiently

**Sizing for Multiple Concurrent Jobs:**

```python
# Single job at a time
DB_POOL_SIZE = 5

# 2-3 concurrent jobs
DB_POOL_SIZE = 10

# 5+ concurrent jobs (rare)
DB_POOL_SIZE = 20
```

#### 1.2 Schema Creation via Init Container

**When Do CREATE TABLE Statements Get Executed?**

The database schema (tables, indexes, triggers) is created by an **init container** that runs before the main digitize-api container starts. This ensures the database is properly initialized before any application code runs.

**Execution Flow:**

```
Podman/OCP Deployment
    ↓
Init Container Starts (digitize-db-init)
    ↓
Execute init_db.sh script
    ↓
Connect to PostgreSQL
    ↓
Run init_schema.sql with IF NOT EXISTS clauses
    ↓
CREATE TABLE IF NOT EXISTS jobs (...)
CREATE TABLE IF NOT EXISTS documents (...)
CREATE INDEX IF NOT EXISTS idx_jobs_submitted_at_status (...)
CREATE INDEX IF NOT EXISTS idx_documents_job_id (...)
CREATE INDEX IF NOT EXISTS idx_documents_submitted_at_status (...)
CREATE OR REPLACE FUNCTION update_updated_at_column() (...)
CREATE TRIGGER IF NOT EXISTS update_jobs_updated_at (...)
CREATE TRIGGER IF NOT EXISTS update_documents_updated_at (...)
    ↓
Init Container Completes Successfully
    ↓
Main Application Container (digitize-api) Starts
    ↓
Application Ready
```

**Script: `spyre-rag/src/digitize/scripts/init_schema.sql`**

```sql
-- Database initialization script for digitize metadata
-- This script is idempotent and safe to run multiple times
-- All CREATE statements use IF NOT EXISTS

-- Create tables with IF NOT EXISTS
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
    submitted_at TIMESTAMP NOT NULL,  -- When user submitted the document
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

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO digitize_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO digitize_user;
```

**Script: `spyre-rag/src/digitize/scripts/init_db.sh`**

```bash
#!/bin/bash
# Database initialization script for digitize service
# This script is executed by the init container

set -e  # Exit on error

echo "Starting database initialization..."

# Wait for PostgreSQL to be ready (connect to default 'postgres' database)
echo "Waiting for PostgreSQL to be ready..."
until PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d postgres -c '\q' 2>/dev/null; do
  echo "PostgreSQL is unavailable - sleeping"
  sleep 2
done

echo "PostgreSQL is ready!"

# Create database if it doesn't exist (must use 'postgres' database to create new databases)
echo "Creating database '$POSTGRES_DB' if not exists..."
PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d postgres -tc \
  "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'" | grep -q 1 || \
  PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d postgres \
  -c "CREATE DATABASE $POSTGRES_DB"

# Verify target database is accessible
echo "Verifying database '$POSTGRES_DB' is accessible..."
until PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c '\q' 2>/dev/null; do
  echo "Database '$POSTGRES_DB' not yet accessible - sleeping"
  sleep 1
done

echo "Database '$POSTGRES_DB' is accessible!"

# Run schema initialization on target database
# Note: psql automatically closes the connection when the command completes
echo "Initializing database schema..."
PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -f /scripts/init_schema.sql

echo "✅ Database initialization completed successfully!"
# Connection is automatically closed when psql process exits
```

**OCP/Podman Deployment Changes:**

Add init container section:

```yaml
spec:
  template:
    spec:
      # Init container to initialize database schema
      initContainers:
        - name: digitize-db-init
          image: "icr.io/ai-services-private/postgres:18"  # ppc64le compatible UBI based Postgres image
          command: ["/bin/sh", "/scripts/init_db.sh"]
          env:
            - name: POSTGRES_HOST
              value: "{{ .Values.postgres.host }}"
            - name: POSTGRES_PORT
              value: "{{ .Values.postgres.port }}"
            - name: POSTGRES_DB
              value: "{{ .Values.postgres.database }}"
            - name: POSTGRES_USER
              value: "{{ .Values.postgres.user }}"
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: "{{ .Values.postgres.secretName }}"
                  key: password
          volumeMounts:
            - name: db-init-scripts
              mountPath: /scripts
              readOnly: true

      # Main application container
      containers:
        - name: digitize-api
          # ... existing container configuration ...
          env:
            # Add PostgreSQL connection environment variables
            - name: POSTGRES_HOST
              value: "{{ .Values.postgres.host }}"
            - name: POSTGRES_PORT
              value: "{{ .Values.postgres.port }}"
            - name: POSTGRES_DB
              value: "{{ .Values.postgres.database }}"
            - name: POSTGRES_USER
              value: "{{ .Values.postgres.user }}"
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: "{{ .Values.postgres.secretName }}"
                  key: password
            # ... existing env vars ...

      volumes:
        - name: db-init-scripts
          configMap:
            name: digitize-db-init-scripts
            defaultMode: 0755
        # ... existing volumes ...
```

**ConfigMap for Init Scripts:**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: digitize-db-init-scripts
  labels:
    ai-services.io/application: {{ .Release.Name }}
    ai-services.io/template: {{ .Chart.Name }}
data:
  init_db.sh: |
{{ .Files.Get "spyre-rag/src/digitize/scripts/init_db.sh" | indent 4 }}
  init_schema.sql: |
{{ .Files.Get "spyre-rag/src/digitize/scripts/init_schema.sql" | indent 4 }}
```

**Why Init Container Approach?**

- ✅ **Separation of Concerns**: Schema management separate from application code
- ✅ **Idempotent**: Safe to run on every pod restart (IF NOT EXISTS clauses)
- ✅ **Fail-Fast**: Application won't start if database initialization fails
- ✅ **Auditability**: Clear SQL scripts that can be reviewed and version controlled
- ✅ **No Application Code Changes**: Application doesn't handle schema creation
- ✅ **Kubernetes Native**: Leverages init container pattern for setup tasks
- ✅ **Rerun Safe**: Can be rerun without issues due to IF NOT EXISTS

#### 1.3 Data Migration Script (Using SQLAlchemy ORM)

**Script: `spyre-rag/src/digitize/scripts/migrate_json_to_postgres.py`**
```python
import json
from pathlib import Path
from datetime import datetime
from sqlalchemy.dialects.postgresql import insert
import digitize.config as config
from digitize.database import SessionLocal, init_db
from digitize.models import Job, Document

def migrate_documents():
    """Migrate document metadata from JSON files to PostgreSQL using SQLAlchemy."""
    session = SessionLocal()
    docs_dir = Path(config.DOCS_DIR)
    migrated = 0
    failed = 0
    
    try:
        for json_file in docs_dir.glob("*_metadata.json"):
            try:
                with open(json_file, 'r') as f:
                    doc_data = json.load(f)
                
                # Use PostgreSQL's INSERT ... ON CONFLICT for upsert
                stmt = insert(Document).values(
                    doc_id=doc_data['id'],
                    job_id=doc_data.get('job_id'),
                    name=doc_data['name'],
                    type=doc_data['type'],
                    status=doc_data['status'],
                    output_format=doc_data['output_format'],
                    submitted_at=datetime.fromisoformat(doc_data['submitted_at'].replace('Z', '+00:00')),
                    completed_at=datetime.fromisoformat(doc_data['completed_at'].replace('Z', '+00:00')) if doc_data.get('completed_at') else None,
                    error=doc_data.get('error'),
                    metadata=doc_data.get('metadata', {})
                ).on_conflict_do_update(
                    index_elements=['doc_id'],
                    set_=dict(
                        job_id=doc_data.get('job_id'),
                        name=doc_data['name'],
                        type=doc_data['type'],
                        status=doc_data['status'],
                        output_format=doc_data['output_format'],
                        submitted_at=datetime.fromisoformat(doc_data['submitted_at'].replace('Z', '+00:00')),
                        completed_at=datetime.fromisoformat(doc_data['completed_at'].replace('Z', '+00:00')) if doc_data.get('completed_at') else None,
                        error=doc_data.get('error'),
                        metadata=doc_data.get('metadata', {})
                    )
                )
                session.execute(stmt)
                migrated += 1
            except Exception as e:
                print(f"Failed to migrate {json_file}: {e}")
                failed += 1
        
        session.commit()
        print(f"Document migration complete: {migrated} documents migrated, {failed} failed")
    finally:
        session.close()

def migrate_jobs():
    """Migrate job status from JSON files to PostgreSQL using SQLAlchemy."""
    session = SessionLocal()
    jobs_dir = Path(config.JOBS_DIR)
    migrated = 0
    failed = 0
    
    try:
        for json_file in jobs_dir.glob("*_status.json"):
            try:
                with open(json_file, 'r') as f:
                    job_data = json.load(f)
                
                # Use PostgreSQL's INSERT ... ON CONFLICT for upsert
                stmt = insert(Job).values(
                    job_id=job_data['job_id'],
                    job_name=job_data.get('job_name'),
                    operation=job_data['operation'],
                    status=job_data['status'],
                    submitted_at=datetime.fromisoformat(job_data['submitted_at'].replace('Z', '+00:00')),
                    completed_at=datetime.fromisoformat(job_data['completed_at'].replace('Z', '+00:00')) if job_data.get('completed_at') else None,
                    error=job_data.get('error'),
                    stats=job_data.get('stats', {})
                ).on_conflict_do_update(
                    index_elements=['job_id'],
                    set_=dict(
                        job_name=job_data.get('job_name'),
                        operation=job_data['operation'],
                        status=job_data['status'],
                        submitted_at=datetime.fromisoformat(job_data['submitted_at'].replace('Z', '+00:00')),
                        completed_at=datetime.fromisoformat(job_data['completed_at'].replace('Z', '+00:00')) if job_data.get('completed_at') else None,
                        error=job_data.get('error'),
                        stats=job_data.get('stats', {})
                    )
                )
                session.execute(stmt)
                migrated += 1
            except Exception as e:
                print(f"Failed to migrate {json_file}: {e}")
                failed += 1
        
        session.commit()
        print(f"Job migration complete: {migrated} jobs migrated, {failed} failed")
    finally:
        session.close()

if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser(description='Migrate metadata from JSON files to PostgreSQL')
    parser.add_argument('--cleanup', action='store_true',
                       help='Delete JSON files after successful migration')
    args = parser.parse_args()

    print("Initializing database schema...")
    init_db()
    print("Starting migration...")
    migrate_jobs()
    migrate_documents()
    print("Migration complete!")

    if args.cleanup:
        print("\nCleaning up JSON files...")
        jobs_dir = Path(config.JOBS_DIR)
        docs_dir = Path(config.DOCS_DIR)

        # Remove job status files
        for json_file in jobs_dir.glob("*_status.json"):
            json_file.unlink()
            print(f"Deleted: {json_file}")

        # Remove document metadata files
        for json_file in docs_dir.glob("*_metadata.json"):
            json_file.unlink()
            print(f"Deleted: {json_file}")

        print("Cleanup complete!")
    else:
        print("\nJSON files retained. Use --cleanup flag to remove them after migration.")
```

**How to Execute the Migration:**

The migration runs automatically as an **Init Container** before the digitize service starts. This approach works for both Kubernetes and Podman environments.

**Deployment Configuration:**

Add to digitize deployment spec (works for both Kubernetes and Podman):
```yaml
initContainers:
- name: migrate-metadata
  image: <digitize-image>
  command: ["python", "-m", "digitize.scripts.migrate_json_to_postgres"]
  env:
    - name: POSTGRES_HOST
      value: "postgresql-service"
    - name: POSTGRES_DB
      value: "digitize_metadata"
    - name: POSTGRES_USER
      valueFrom:
        secretKeyRef:
          name: postgresql-credentials
          key: username
    - name: POSTGRES_PASSWORD
      valueFrom:
        secretKeyRef:
          name: postgresql-credentials
          key: password
    - name: RUN_MIGRATION
      value: "true"  # Set to "false" to skip migration
  volumeMounts:
    - name: digitize-data
      mountPath: /data
```

**Migration Control:**

The migration is controlled via the `RUN_MIGRATION` environment variable:
- `RUN_MIGRATION=true` - Runs migration on container startup (default for init container)
- `RUN_MIGRATION=false` - Skips migration (useful after initial migration is complete)

**Benefits of Init Container Approach:**
- ✅ Works for both Kubernetes and Podman deployments
- ✅ Runs automatically before main application starts
- ✅ Ensures database is ready before application processes requests
- ✅ No manual intervention required
- ✅ Idempotent - safe to run multiple times (uses upsert logic)
- ✅ Can be disabled via environment variable after initial migration

**Benefits of SQLAlchemy Migration:**
- Type-safe operations with ORM models
- Automatic connection management
- Built-in transaction handling
- Cleaner, more maintainable code
- Database-agnostic (easy to switch databases)

---

### Phase 2: Code Implementation (Week 2)

#### 2.1 Dependencies

Add to `spyre-rag/requirements.txt`:
```
sqlalchemy>=2.0.0
psycopg2-binary>=2.9.0
```

Add to `spyre-rag/requirements-test.txt`:
```
pytest>=7.0.0
testcontainers[postgres]>=3.7.0
```

#### 2.2 Implementation Components

**Core Files to Create/Modify:**

1. **`database.py`** - Database connection and session management
   - SQLAlchemy engine with connection pooling
   - Scoped session factory
   - Environment-based configuration

2. **`db_store.py`** - Metadata storage abstraction layer
   - CRUD operations for jobs and documents
   - Transaction management with context managers
   - Deep merge support for JSONB metadata updates
   - Eager loading with `selectinload` for efficient queries

3. **`status.py`** - Update StatusManager
   - Replace file I/O with database operations
   - Use MetadataStore for all persistence
   - Maintain thread-safety

**Key Implementation Patterns:**

**Transaction Management:**
```python
# Atomic multi-operation updates
with db_store.transaction():
    db_store.update_document_metadata(doc_id, updates)
    db_store.save_job(job)
```

**Deep Merge for JSONB:**
```python
# Preserves nested structures like timing_in_secs
def _deep_merge_jsonb(existing: Dict, updates: Dict) -> Dict:
    result = existing.copy()
    for key, value in updates.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = self._deep_merge_jsonb(result[key], value)
        else:
            result[key] = value
    return result
```

**Eager Loading:**
```python
# Avoid N+1 queries when fetching jobs with documents
job = session.query(Job).options(selectinload(Job.documents)).filter(Job.job_id == job_id).first()
```

#### 2.4 Metadata Store Implementation

**Metadata Store** (`db_store.py`):

    **Core Operations:**
    - `save_document()` - Upsert document using INSERT ... ON CONFLICT DO UPDATE
    - `get_document()` - Retrieve single document by ID
    - `update_document_metadata()` - Update document fields with deep merge for JSONB metadata
    - `save_job()` - Upsert job state
    - `get_job()` - Retrieve job with eager-loaded documents (selectinload)
    - `update_job_stats()` - Update job statistics with deep merge for JSONB stats
    - `list_documents()` - Paginated document listing with optional status filter
    - `list_jobs()` - Paginated job listing with eager-loaded documents

    **Transaction Management:**
    - Context manager: `with db_store.transaction():`
    - Auto-commit mode for single operations
    - Manual transaction control for atomic multi-operation updates
    - External session support for fine-grained control

    **Deep Merge for JSONB:**
    - `_deep_merge_jsonb()` - Recursive merge preserving nested structures
    - Critical for `timing_in_secs` where pipeline stages update independently
    - Prevents data loss from PostgreSQL's shallow `||` operator

    **Query Optimization:**
    - Uses `selectinload` for one-to-many relationships (Job → Documents)
    - Executes 2 queries total instead of N+1
    - More efficient than `joinedload` for collections with many items

**Usage Examples:**

```python
# Example 1: Single operation (auto-commit)
db_store = MetadataStore()
db_store.save_document(doc)  # Commits automatically

# Example 2: Atomic multi-operation using transaction context manager
db_store = MetadataStore()
with db_store.transaction():
    db_store.update_document_metadata(doc_id, {"status": DocStatus.COMPLETED})
    db_store.save_job(job)  # Both operations committed together
# Commits here, or rolls back on exception

# Example 3: Manual transaction control
db_store = MetadataStore()
try:
    db_store.save_document(doc, auto_commit=False)
    db_store.update_document_metadata(doc_id, updates, auto_commit=False)
    db_store.save_job(job, auto_commit=False)
    db_store.commit()  # Explicit commit
except Exception:
    db_store.rollback()  # Explicit rollback
    raise

# Example 4: Using external session for fine-grained control
from digitize.database import SessionLocal

session = SessionLocal()
try:
    db_store = MetadataStore(session=session)

    # Multiple operations in same transaction
    db_store.save_document(doc, auto_commit=False)
    db_store.save_job(job, auto_commit=False)

    # Commit when ready
    session.commit()
except Exception:
    session.rollback()
    raise
finally:
    session.close()
```


#### 2.5 Implementation Strategies

**Eager Loading Strategy: `selectinload` vs `joinedload`**

SQLAlchemy provides two main eager loading strategies:

1. **`joinedload`** - Uses SQL JOIN in a single query
   ```sql
   SELECT jobs.*, documents.*
   FROM jobs
   LEFT OUTER JOIN documents ON jobs.job_id = documents.job_id
   WHERE jobs.job_id = ?
   ```
   - ✅ Single query
   - ❌ Can return duplicate rows (one per document)
   - ❌ Less efficient for one-to-many with many related records
   - ✅ Good for one-to-one or small one-to-many relationships

2. **`selectinload`** - Uses separate optimized query with IN clause
   ```sql
   -- Query 1: Get jobs
   SELECT * FROM jobs WHERE job_id = ?

   -- Query 2: Get all related documents in one query
   SELECT * FROM documents WHERE job_id IN (?, ?, ...)
   ```
   - ✅ Two queries total (not N+1)
   - ✅ No duplicate rows
   - ✅ More efficient for one-to-many with many related records
   - ✅ Better for collections with many items
   - ✅ **Recommended for most one-to-many relationships**

**For our use case (Job → Documents):**
- Each job can have many documents (one-to-many)
- `selectinload` is more efficient and preferred
- Avoids row duplication in result set
- Better performance when jobs have many documents

**JSONB Metadata Updates: Deep Merge Strategy**

PostgreSQL's native `||` operator performs **shallow merge** on JSONB:

```python
# PROBLEM: Shallow merge loses nested data
existing = {"pages": 5, "timing_in_secs": {"digitizing": 10, "processing": 20}}
update = {"timing_in_secs": {"chunking": 15}}
result = existing || update  # PostgreSQL || operator
# Result: {"pages": 5, "timing_in_secs": {"chunking": 15}}
# ❌ Lost digitizing and processing times!
```

**Our Solution: Python-based Deep Merge**

The `_deep_merge_jsonb()` helper function performs recursive deep merge:

```python
def _deep_merge_jsonb(existing: Dict, updates: Dict) -> Dict:
    """Recursively merge dictionaries, preserving nested structures."""
    result = existing.copy()
    for key, value in updates.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = self._deep_merge_jsonb(result[key], value)  # Recurse
        else:
            result[key] = value
    return result
```

**Example: Updating timing_in_secs incrementally**

```python
# Initial state
doc.metadata = {
    "pages": 10,
    "tables": 3,
    "timing_in_secs": {
        "digitizing": 5.2,
        "processing": 3.1
    }
}

# Update 1: Add chunking time
db_store.update_document_metadata(doc_id, {
    "metadata": {"timing_in_secs": {"chunking": 2.5}}
})
# Result: All previous timing fields preserved + chunking added
# {"pages": 10, "tables": 3, "timing_in_secs": {"digitizing": 5.2, "processing": 3.1, "chunking": 2.5}}

# Update 2: Add indexing time
db_store.update_document_metadata(doc_id, {
    "metadata": {"timing_in_secs": {"indexing": 1.8}}
})
# Result: All timing fields preserved + indexing added
# {"pages": 10, "tables": 3, "timing_in_secs": {"digitizing": 5.2, "processing": 3.1, "chunking": 2.5, "indexing": 1.8}}
```

**Why Deep Merge Matters:**

- ✅ **Incremental Updates**: Can update individual timing fields as pipeline stages complete
- ✅ **Data Preservation**: Never lose existing nested data during updates
- ✅ **Pipeline Compatibility**: Each stage (digitizing, processing, chunking, indexing) can update its own timing independently
- ✅ **Backward Compatible**: Works with existing code that updates metadata incrementally

**Performance Consideration:**

Deep merge is performed in Python (not SQL) because:
- PostgreSQL's `jsonb_set()` requires explicit paths for each nested key
- Python recursive merge is simpler and more maintainable
- Metadata updates are infrequent (per document, not per query)
- The overhead is negligible compared to document processing time

- ✅ **Eager Loading Optimization**: Uses `selectinload()` to avoid N+1 query problems when fetching related documents
- ✅ **Query Efficiency**: Single query fetches job and all related documents instead of separate queries per job


### Phase 3: Testing & Validation (Week 3)

#### 3.1 Test Strategy

**Unit Tests:**
- Test MetadataStore CRUD operations
- Test deep merge functionality for nested JSONB
- Test transaction management (commit/rollback)
- Test concurrent updates with threading

**Integration Tests:**
- Test StatusManager with real PostgreSQL (via testcontainers)
- Test end-to-end document processing workflow
- Test job progress tracking

**Performance Tests:**
- Concurrent write performance
- Query performance with pagination
- Connection pool behavior under load

**Testing Infrastructure:**
- Use testcontainers to spin up real PostgreSQL instances for tests
- Ensures tests run against actual PostgreSQL features (JSONB, ON CONFLICT DO UPDATE)
- Automatic cleanup after test completion

### Phase 4: Deployment (Week 4)

#### 4.1 Deployment Checklist

- [ ] PostgreSQL instance provisioned and configured
- [ ] Database schema created
- [ ] Indexes created
- [ ] Existing JSON data migrated
- [ ] Application code updated
- [ ] Environment variables configured
- [ ] Connection pooling configured
- [ ] Monitoring set up
- [ ] Backup strategy implemented

#### 4.2 Handling Migration Failures

If migration issues arise, they must be fixed in place:
1. Review migration logs for specific errors
2. Fix data inconsistencies or schema issues
3. Rerun migration script (idempotent with upsert logic)
4. Verify data integrity after fixes

**Note:** There is no rollback to JSON file storage. Once migrated, the system operates exclusively with PostgreSQL. Ensure thorough testing in non-production environments before production migration.

---

## Future Enhancements

### 1. Advanced Querying

```sql
-- Find slow documents
SELECT doc_id, name, 
       (metadata->'timing_in_secs'->>'processing')::float as processing_time
FROM documents
WHERE (metadata->'timing_in_secs'->>'processing')::float > 300
ORDER BY processing_time DESC;

-- Job success rate by operation type
SELECT operation, 
       COUNT(*) as total,
       SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed,
       ROUND(100.0 * SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate
FROM jobs
GROUP BY operation;

-- Average processing time by document type
SELECT type,
       AVG((metadata->'timing_in_secs'->>'digitizing')::float) as avg_digitizing,
       AVG((metadata->'timing_in_secs'->>'processing')::float) as avg_processing,
       AVG((metadata->'timing_in_secs'->>'chunking')::float) as avg_chunking,
       AVG((metadata->'timing_in_secs'->>'indexing')::float) as avg_indexing
FROM documents
WHERE status = 'completed'
GROUP BY type;
```

### 2. Audit Trail

```sql
-- Add audit columns
ALTER TABLE documents ADD COLUMN version INT DEFAULT 1;
ALTER TABLE documents ADD COLUMN modified_by VARCHAR(255);

-- Create audit log table
CREATE TABLE document_audit_log (
    id SERIAL PRIMARY KEY,
    doc_id VARCHAR(255) NOT NULL,
    changed_fields JSONB NOT NULL,
    changed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    changed_by VARCHAR(255)
);

-- Trigger for audit logging
CREATE OR REPLACE FUNCTION log_document_changes()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO document_audit_log (doc_id, changed_fields, changed_by)
    VALUES (
        NEW.doc_id,
        jsonb_build_object(
            'old', to_jsonb(OLD),
            'new', to_jsonb(NEW)
        ),
        current_user
    );
    NEW.version = OLD.version + 1;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER document_audit_trigger
BEFORE UPDATE ON documents
FOR EACH ROW EXECUTE FUNCTION log_document_changes();
```

### 3. Retention Policies

```sql
-- Archive old completed jobs (older than 90 days)
CREATE TABLE archived_jobs (LIKE jobs INCLUDING ALL);
CREATE TABLE archived_documents (LIKE documents INCLUDING ALL);

-- Archive function
CREATE OR REPLACE FUNCTION archive_old_jobs()
RETURNS void AS $$
BEGIN
    -- Move old jobs to archive
    INSERT INTO archived_jobs
    SELECT * FROM jobs
    WHERE status IN ('completed', 'failed')
    AND completed_at < NOW() - INTERVAL '90 days';
    
    -- Move associated documents
    INSERT INTO archived_documents
    SELECT d.* FROM documents d
    INNER JOIN archived_jobs aj ON d.job_id = aj.job_id;
    
    -- Delete from main tables
    DELETE FROM documents
    WHERE job_id IN (SELECT job_id FROM archived_jobs);
    
    DELETE FROM jobs
    WHERE job_id IN (SELECT job_id FROM archived_jobs);
END;
$$ language 'plpgsql';

-- Schedule via cron or pg_cron
SELECT cron.schedule('archive-old-jobs', '0 2 * * *', 'SELECT archive_old_jobs()');
```

### 4. Real-Time Notifications

```sql
-- Enable PostgreSQL LISTEN/NOTIFY for real-time updates
CREATE OR REPLACE FUNCTION notify_status_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify(
        'document_status_change',
        json_build_object(
            'doc_id', NEW.doc_id,
            'job_id', NEW.job_id,
            'old_status', OLD.status,
            'new_status', NEW.status
        )::text
    );
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER document_status_notify
AFTER UPDATE OF status ON documents
FOR EACH ROW EXECUTE FUNCTION notify_status_change();
```

### 5. Metadata Extensions

Easy to add new fields:
```sql
-- Add new metadata fields without breaking existing code
ALTER TABLE documents ADD COLUMN language VARCHAR(10);
ALTER TABLE documents ADD COLUMN quality_score FLOAT;
ALTER TABLE documents ADD COLUMN priority INT DEFAULT 0;

-- Add to metadata JSONB for flexible fields
UPDATE documents
SET metadata = metadata || '{"classification": "technical", "confidence": 0.95}'::jsonb
WHERE doc_id = 'example_doc';
```

---

## Conclusion

Migrating from JSON file-based storage to PostgreSQL will provide:

1. **Scalability**: Handle millions of documents with consistent performance
2. **Reliability**: ACID guarantees prevent data loss and corruption
3. **Performance**: Sub-5ms write latency with excellent concurrent access
4. **Flexibility**: JSONB support allows schema evolution without migrations
5. **Queryability**: Rich SQL capabilities for analytics and reporting
6. **Operational Excellence**: Mature tooling for monitoring, backup, and recovery

---
