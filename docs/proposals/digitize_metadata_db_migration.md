# PostgreSQL Migration for Digitize Service Metadata Storage

## Executive Summary

This document outlines the migration plan from JSON file-based metadata storage to PostgreSQL for the digitize service. PostgreSQL has been selected as the database solution to address current scalability challenges while providing ACID guarantees, excellent concurrent write performance, and flexible JSON support for evolving metadata schemas.

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

### SQLAlchemy Models

**File: `spyre-rag/src/digitize/models.py`**

```python
from datetime import datetime
from typing import Optional
from sqlalchemy import Column, String, Text, DateTime, ForeignKey, Index, CheckConstraint
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import declarative_base, relationship
from sqlalchemy.sql import func

Base = declarative_base()

class Job(Base):
    """SQLAlchemy model for jobs table."""
    __tablename__ = 'jobs'
    
    job_id = Column(String(255), primary_key=True)
    job_name = Column(String(500), nullable=True)
    operation = Column(String(50), nullable=False)
    status = Column(String(50), nullable=False)
    submitted_at = Column(DateTime(timezone=True), nullable=False)
    completed_at = Column(DateTime(timezone=True), nullable=True)
    error = Column(Text, nullable=True)
    stats = Column(JSONB, nullable=False, default={"total_documents": 0, "completed": 0, "failed": 0, "in_progress": 0})
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())
    
    # Relationship to documents
    documents = relationship("Document", back_populates="job", cascade="all, delete-orphan")
    
    # Constraints
    __table_args__ = (
        CheckConstraint("status IN ('accepted', 'in_progress', 'completed', 'failed')", name='chk_job_status'),
        CheckConstraint("operation IN ('ingestion', 'digitization')", name='chk_job_operation'),
        Index('idx_jobs_submitted_at_status', 'submitted_at', 'status'),
    )

class Document(Base):
    """SQLAlchemy model for documents table."""
    __tablename__ = 'documents'
    
    doc_id = Column(String(255), primary_key=True)
    job_id = Column(String(255), ForeignKey('jobs.job_id', ondelete='CASCADE'), nullable=True)
    name = Column(String(500), nullable=False)
    type = Column(String(50), nullable=False)
    status = Column(String(50), nullable=False)
    output_format = Column(String(10), nullable=False)
    submitted_at = Column(DateTime(timezone=True), nullable=False)
    completed_at = Column(DateTime(timezone=True), nullable=True)
    error = Column(Text, nullable=True)
    metadata = Column(JSONB, nullable=False, default={})
    created_at = Column(DateTime(timezone=True), server_default=func.now())
    updated_at = Column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())
    
    # Relationship to job
    job = relationship("Job", back_populates="documents")
    
    # Constraints
    __table_args__ = (
        CheckConstraint("status IN ('accepted', 'in_progress', 'digitized', 'processed', 'chunked', 'completed', 'failed')", name='chk_doc_status'),
        CheckConstraint("type IN ('ingestion', 'digitization')", name='chk_doc_type'),
        CheckConstraint("output_format IN ('txt', 'md', 'json')", name='chk_output_format'),
        Index('idx_documents_job_id', 'job_id'),
        Index('idx_documents_submitted_at_status', 'submitted_at', 'status'),
    )
```

### Database Session Management

**File: `spyre-rag/src/digitize/database.py`**

```python
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker, scoped_session
from sqlalchemy.pool import QueuePool
import os
from digitize.models import Base

# Database configuration
DB_HOST = os.getenv("POSTGRES_HOST", "localhost")
DB_PORT = int(os.getenv("POSTGRES_PORT", "5432"))
DB_NAME = os.getenv("POSTGRES_DB", "digitize_metadata")
DB_USER = os.getenv("POSTGRES_USER", "digitize_user")
DB_PASSWORD = os.getenv("POSTGRES_PASSWORD")
DB_POOL_SIZE = int(os.getenv("DB_POOL_SIZE", "5"))
DB_MAX_OVERFLOW = int(os.getenv("DB_MAX_OVERFLOW", "5"))

# Create database URL
DATABASE_URL = f"postgresql://{DB_USER}:{DB_PASSWORD}@{DB_HOST}:{DB_PORT}/{DB_NAME}"

# Create engine with connection pooling
engine = create_engine(
    DATABASE_URL,
    poolclass=QueuePool,
    pool_size=DB_POOL_SIZE,
    max_overflow=DB_MAX_OVERFLOW,
    pool_pre_ping=True,  # Verify connections before using
    echo=False  # Set to True for SQL query logging during development
)

# Create session factory
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)

# Thread-safe session
SessionScoped = scoped_session(SessionLocal)

def init_db():
    """Initialize database tables."""
    Base.metadata.create_all(bind=engine)

def get_session():
    """Get database session with automatic cleanup."""
    session = SessionScoped()
    try:
        yield session
        session.commit()
    except Exception:
        session.rollback()
        raise
    finally:
        session.close()
```

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
    submitted_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    error TEXT,
    stats JSONB NOT NULL DEFAULT '{"total_documents": 0, "completed": 0, "failed": 0, "in_progress": 0}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
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
    submitted_at TIMESTAMP NOT NULL,
    completed_at TIMESTAMP,
    error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_doc_status CHECK (status IN ('accepted', 'in_progress', 'digitized', 'processed', 'chunked', 'completed', 'failed')),
    CONSTRAINT chk_doc_type CHECK (type IN ('ingestion', 'digitization')),
    CONSTRAINT chk_output_format CHECK (output_format IN ('txt', 'md', 'json'))
);
```

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

#### 1.2 Schema Creation

**Script: `scripts/db/init_schema.sql`**
```sql
-- Create database
CREATE DATABASE digitize_metadata;

-- Connect to database
\c digitize_metadata

-- Create tables (use schema from above)
-- Create indexes (use indexes from above)
-- Create triggers (use triggers from above)

-- Grant permissions
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO digitize_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO digitize_user;
```

#### 1.3 Data Migration Script (Using SQLAlchemy ORM)

**Script: `scripts/db/migrate_json_to_postgres.py`**
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
    print("Initializing database schema...")
    init_db()
    print("Starting migration...")
    migrate_jobs()
    migrate_documents()
    print("Migration complete!")
```

**Benefits of SQLAlchemy Migration:**
- Type-safe operations with ORM models
- Automatic connection management
- Built-in transaction handling
- Cleaner, more maintainable code
- Database-agnostic (easy to switch databases)
    cursor.close()
    conn.close()
    
    print(f"Migration complete: {migrated} jobs migrated, {failed} failed")

if __name__ == "__main__":
    print("Starting migration...")
    migrate_jobs()
    migrate_documents()
    print("Migration complete!")
```

### Phase 2: Implementation (Week 2)

#### 2.1 Database Abstraction Layer (Using SQLAlchemy ORM)

**File: `spyre-rag/src/digitize/db_store.py`**
```python
from typing import Optional, List, Dict, Any
from datetime import datetime
from sqlalchemy import update as sql_update, select
from sqlalchemy.dialects.postgresql import insert
from digitize.database import SessionScoped
from digitize.models import Job, Document
from digitize.document import DocumentMetadata
from digitize.job import JobState, JobDocumentSummary
from digitize.types import DocStatus, JobStatus

class MetadataStore:
    """SQLAlchemy ORM-backed metadata storage for documents and jobs."""
    
    def __init__(self):
        """Initialize metadata store with SQLAlchemy session."""
        self.session = SessionScoped()
    
    # Document operations
    def save_document(self, doc: DocumentMetadata) -> None:
        """Save or update document metadata using ORM."""
        stmt = insert(Document).values(
            doc_id=doc.id,
            job_id=doc.job_id,
            name=doc.name,
            type=doc.type,
            status=doc.status.value if isinstance(doc.status, DocStatus) else doc.status,
            output_format=doc.output_format.value if hasattr(doc.output_format, 'value') else doc.output_format,
            submitted_at=datetime.fromisoformat(doc.submitted_at.replace('Z', '+00:00')) if isinstance(doc.submitted_at, str) else doc.submitted_at,
            completed_at=datetime.fromisoformat(doc.completed_at.replace('Z', '+00:00')) if doc.completed_at and isinstance(doc.completed_at, str) else doc.completed_at,
            error=doc.error,
            metadata=doc.metadata
        ).on_conflict_do_update(
            index_elements=['doc_id'],
            set_=dict(
                job_id=doc.job_id,
                name=doc.name,
                type=doc.type,
                status=doc.status.value if isinstance(doc.status, DocStatus) else doc.status,
                output_format=doc.output_format.value if hasattr(doc.output_format, 'value') else doc.output_format,
                submitted_at=datetime.fromisoformat(doc.submitted_at.replace('Z', '+00:00')) if isinstance(doc.submitted_at, str) else doc.submitted_at,
                completed_at=datetime.fromisoformat(doc.completed_at.replace('Z', '+00:00')) if doc.completed_at and isinstance(doc.completed_at, str) else doc.completed_at,
                error=doc.error,
                metadata=doc.metadata
            )
        )
        self.session.execute(stmt)
        self.session.commit()
    
    def get_document(self, doc_id: str) -> Optional[DocumentMetadata]:
        """Retrieve document metadata by ID using ORM."""
        doc = self.session.query(Document).filter(Document.doc_id == doc_id).first()
        if doc:
            return DocumentMetadata(
                id=doc.doc_id,
                job_id=doc.job_id,
                name=doc.name,
                type=doc.type,
                status=doc.status,
                output_format=doc.output_format,
                submitted_at=doc.submitted_at.isoformat() if doc.submitted_at else None,
                completed_at=doc.completed_at.isoformat() if doc.completed_at else None,
                error=doc.error,
                metadata=doc.metadata
            )
        return None
    
    def update_document_metadata(self, doc_id: str, updates: Dict[str, Any]) -> None:
        """Update specific fields of document metadata using ORM."""
        # Prepare update values
        update_values = {}
        for key, value in updates.items():
            if key == 'metadata':
                # Merge with existing metadata using PostgreSQL JSONB operator
                stmt = sql_update(Document).where(Document.doc_id == doc_id).values(
                    metadata=Document.metadata + value  # JSONB concatenation
                )
                self.session.execute(stmt)
                continue
            elif key == 'status':
                update_values[key] = value.value if hasattr(value, 'value') else value
            else:
                update_values[key] = value
        
        if update_values:
            stmt = sql_update(Document).where(Document.doc_id == doc_id).values(**update_values)
            self.session.execute(stmt)
        
        self.session.commit()
    
    # Job operations
    def save_job(self, job: JobState) -> None:
        """Save or update job state using ORM."""
        stmt = insert(Job).values(
            job_id=job.job_id,
            job_name=job.job_name,
            operation=job.operation,
            status=job.status.value if isinstance(job.status, JobStatus) else job.status,
            submitted_at=datetime.fromisoformat(job.submitted_at.replace('Z', '+00:00')) if isinstance(job.submitted_at, str) else job.submitted_at,
            completed_at=datetime.fromisoformat(job.completed_at.replace('Z', '+00:00')) if job.completed_at and isinstance(job.completed_at, str) else job.completed_at,
            error=job.error,
            stats=job.stats.model_dump() if hasattr(job.stats, 'model_dump') else job.stats
        ).on_conflict_do_update(
            index_elements=['job_id'],
            set_=dict(
                job_name=job.job_name,
                operation=job.operation,
                status=job.status.value if isinstance(job.status, JobStatus) else job.status,
                submitted_at=datetime.fromisoformat(job.submitted_at.replace('Z', '+00:00')) if isinstance(job.submitted_at, str) else job.submitted_at,
                completed_at=datetime.fromisoformat(job.completed_at.replace('Z', '+00:00')) if job.completed_at and isinstance(job.completed_at, str) else job.completed_at,
                error=job.error,
                stats=job.stats.model_dump() if hasattr(job.stats, 'model_dump') else job.stats
            )
        )
        self.session.execute(stmt)
        self.session.commit()
    
    def get_job(self, job_id: str) -> Optional[JobState]:
        """Retrieve job state by ID using ORM."""
        job = self.session.query(Job).filter(Job.job_id == job_id).first()
        if not job:
            return None
        
        # Get documents for this job
        documents = self.session.query(Document).filter(Document.job_id == job_id).all()
        doc_summaries = [
            JobDocumentSummary(id=doc.doc_id, name=doc.name, status=doc.status)
            for doc in documents
        ]
        
        return JobState(
            job_id=job.job_id,
            job_name=job.job_name,
            operation=job.operation,
            status=job.status,
            submitted_at=job.submitted_at.isoformat() if job.submitted_at else None,
            completed_at=job.completed_at.isoformat() if job.completed_at else None,
            error=job.error,
            documents=doc_summaries,
            stats=job.stats
        )
    
    def list_documents(self, limit: int = 100, offset: int = 0,
                      status: Optional[str] = None) -> List[DocumentMetadata]:
        """List documents with pagination and optional filtering using ORM."""
        query = self.session.query(Document)
        
        if status:
            query = query.filter(Document.status == status)
        
        query = query.order_by(Document.submitted_at.desc()).limit(limit).offset(offset)
        
        documents = []
        for doc in query.all():
            documents.append(DocumentMetadata(
                id=doc.doc_id,
                job_id=doc.job_id,
                name=doc.name,
                type=doc.type,
                status=doc.status,
                output_format=doc.output_format,
                submitted_at=doc.submitted_at.isoformat() if doc.submitted_at else None,
                completed_at=doc.completed_at.isoformat() if doc.completed_at else None,
                error=doc.error,
                metadata=doc.metadata
            ))
        return documents
    
    def list_jobs(self, limit: int = 100, offset: int = 0,
                 status: Optional[str] = None) -> List[JobState]:
        """List jobs with pagination and optional filtering using ORM."""
        query = self.session.query(Job)
        
        if status:
            query = query.filter(Job.status == status)
        
        query = query.order_by(Job.submitted_at.desc()).limit(limit).offset(offset)
        
        jobs = []
        for job in query.all():
            # Get documents for each job
            documents = self.session.query(Document).filter(Document.job_id == job.job_id).all()
            doc_summaries = [
                JobDocumentSummary(id=doc.doc_id, name=doc.name, status=doc.status)
                for doc in documents
            ]
            
            jobs.append(JobState(
                job_id=job.job_id,
                job_name=job.job_name,
                operation=job.operation,
                status=job.status,
                submitted_at=job.submitted_at.isoformat() if job.submitted_at else None,
                completed_at=job.completed_at.isoformat() if job.completed_at else None,
                error=job.error,
                documents=doc_summaries,
                stats=job.stats
            ))
        return jobs
```

**Benefits of SQLAlchemy Implementation:**
- ✅ **Type Safety**: ORM models provide compile-time type checking
- ✅ **Cleaner Code**: No raw SQL strings, Pythonic query building
- ✅ **Automatic Connection Management**: Session handles connections automatically
- ✅ **Built-in Connection Pooling**: SQLAlchemy manages pool internally
- ✅ **Database Agnostic**: Easy to switch databases if needed
- ✅ **Relationship Handling**: Automatic JOIN operations via relationships
- ✅ **Transaction Management**: Automatic commit/rollback on errors

### Dependencies

**File: `spyre-rag/requirements.txt`** (add these lines)
```
sqlalchemy>=2.0.0
psycopg2-binary>=2.9.0  # PostgreSQL driver
alembic>=1.12.0  # For database migrations (optional but recommended)
            self._put_conn(conn)
```

#### 2.2 Update StatusManager

**File: `spyre-rag/src/digitize/status.py`** (modifications)
```python
from digitize.db_store import MetadataStore

class StatusManager:
    """Thread-safe handler for updating Job and Document status using PostgreSQL"""
    def __init__(self, job_id: str):
        self.job_id = job_id
        self.db_store = MetadataStore()
        self._job_lock = threading.Lock()
        self._doc_locks: dict[str, threading.Lock] = {}
        self._doc_locks_lock = threading.Lock()
    
    def update_doc_metadata(self, doc_id: str, details: Mapping[str, Any], error: str = "") -> None:
        """Updates document metadata in PostgreSQL."""
        doc_lock = self._get_doc_lock(doc_id)
        with doc_lock:
            try:
                updates = {}
                
                # Categorize fields
                metadata_fields, top_level_fields = self._categorize_fields(details)
                
                # Add error if provided
                if error:
                    top_level_fields["error"] = str(error)
                    if "status" not in top_level_fields:
                        top_level_fields["status"] = DocStatus.FAILED
                
                # Merge metadata updates
                if metadata_fields:
                    updates['metadata'] = metadata_fields
                
                # Add top-level updates
                updates.update(top_level_fields)
                
                # Update in database
                self.db_store.update_document_metadata(doc_id, updates)
                logger.debug(f"✅ Successfully updated metadata for {doc_id}")
            except Exception as e:
                logger.error(f"❌ Failed to update metadata for {doc_id}: {str(e)}", exc_info=True)
    
    def update_job_progress(self, doc_id: str, doc_status: DocStatus, job_status: JobStatus, error: str = ""):
        """Updates job progress in PostgreSQL."""
        with self._job_lock:
            try:
                # Get current job state
                job = self.db_store.get_job(self.job_id)
                if not job:
                    logger.error(f"Job {self.job_id} not found")
                    return
                
                # Update document status if doc_id provided
                if doc_id:
                    for doc in job.documents:
                        if doc.id == doc_id:
                            doc.status = doc_status.value
                            break
                
                # Recalculate stats
                status_counts = {}
                for doc in job.documents:
                    status_counts[doc.status] = status_counts.get(doc.status, 0) + 1
                
                job.stats.completed = status_counts.get(DocStatus.COMPLETED.value, 0)
                job.stats.failed = status_counts.get(DocStatus.FAILED.value, 0)
                job.stats.in_progress = (
                    status_counts.get(DocStatus.IN_PROGRESS.value, 0) +
                    status_counts.get(DocStatus.DIGITIZED.value, 0) +
                    status_counts.get(DocStatus.PROCESSED.value, 0) +
                    status_counts.get(DocStatus.CHUNKED.value, 0)
                )
                
                # Update job-level fields
                job.status = job_status
                if job_status in [JobStatus.COMPLETED, JobStatus.FAILED]:
                    total_docs = job.stats.total_documents
                    completed_docs = job.stats.completed
                    failed_docs = job.stats.failed
                    if total_docs > 0 and (completed_docs + failed_docs) == total_docs:
                        job.completed_at = get_utc_timestamp()
                
                if error and job_status == JobStatus.FAILED:
                    job.error = str(error)
                
                # Save to database
                self.db_store.save_job(job)
            except Exception as e:
                logger.error(f"Failed to update job progress: {e}", exc_info=True)
```

### Phase 3: Testing & Validation (Week 3)

#### 3.1 Unit Tests with SQLAlchemy

**File: `tests/test_db_store.py`**
```python
import pytest
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from digitize.db_store import MetadataStore, Base, Job, Document
from digitize.document import DocumentMetadata
from digitize.job import JobState, JobStats, JobDocumentSummary
from digitize.types import DocStatus, JobStatus, OutputFormat

@pytest.fixture
def test_engine():
    """Create in-memory SQLite database for testing."""
    engine = create_engine('sqlite:///:memory:', echo=True)
    Base.metadata.create_all(engine)
    return engine

@pytest.fixture
def db_store(test_engine):
    """Create MetadataStore with test database."""
    Session = sessionmaker(bind=test_engine)
    store = MetadataStore()
    store.session = Session()
    yield store
    store.session.close()

def test_save_and_get_document(db_store):
    """Test document creation and retrieval using ORM."""
    doc = DocumentMetadata(
        id="test_doc_1",
        name="test.pdf",
        type="ingestion",
        status=DocStatus.ACCEPTED,
        output_format=OutputFormat.JSON,
        submitted_at="2024-01-01T00:00:00Z",
        job_id="test_job_1",
        metadata={"pages": 10}
    )
    
    db_store.save_document(doc)
    retrieved = db_store.get_document("test_doc_1")
    
    assert retrieved is not None
    assert retrieved.id == doc.id
    assert retrieved.name == doc.name
    assert retrieved.metadata["pages"] == 10

def test_update_document_metadata(db_store):
    """Test document metadata updates using ORM."""
    doc = DocumentMetadata(
        id="test_doc_2",
        name="test2.pdf",
        type="ingestion",
        status=DocStatus.IN_PROGRESS,
        output_format=OutputFormat.JSON,
        submitted_at="2024-01-01T00:00:00Z",
        job_id="test_job_1",
        metadata={}
    )
    
    db_store.save_document(doc)
    db_store.update_document_metadata("test_doc_2", {
        "status": DocStatus.COMPLETED.value,
        "metadata": {"pages": 20, "tables": 5}
    })
    
    retrieved = db_store.get_document("test_doc_2")
    assert retrieved.status == DocStatus.COMPLETED.value
    assert retrieved.metadata["pages"] == 20
    assert retrieved.metadata["tables"] == 5

def test_job_with_documents(db_store):
    """Test job creation with related documents using ORM relationships."""
    # Create job
    job = JobState(
        job_id="test_job_1",
        job_name="Test Job",
        operation="ingestion",
        status=JobStatus.IN_PROGRESS,
        submitted_at="2024-01-01T00:00:00Z",
        documents=[],
        stats=JobStats(total_documents=2, completed=0, failed=0, in_progress=2)
    )
    db_store.save_job(job)
    
    # Create documents for this job
    doc1 = DocumentMetadata(
        id="doc_1",
        name="file1.pdf",
        type="ingestion",
        status=DocStatus.IN_PROGRESS,
        output_format=OutputFormat.JSON,
        submitted_at="2024-01-01T00:00:00Z",
        job_id="test_job_1",
        metadata={}
    )
    doc2 = DocumentMetadata(
        id="doc_2",
        name="file2.pdf",
        type="ingestion",
        status=DocStatus.COMPLETED,
        output_format=OutputFormat.JSON,
        submitted_at="2024-01-01T00:00:00Z",
        job_id="test_job_1",
        metadata={}
    )
    
    db_store.save_document(doc1)
    db_store.save_document(doc2)
    
    # Retrieve job and verify documents are loaded via relationship
    retrieved_job = db_store.get_job("test_job_1")
    assert retrieved_job is not None
    assert len(retrieved_job.documents) == 2
    assert any(d.id == "doc_1" for d in retrieved_job.documents)
    assert any(d.id == "doc_2" for d in retrieved_job.documents)

def test_list_documents_with_filtering(db_store):
    """Test document listing with status filtering using ORM queries."""
    # Create test documents
    for i in range(5):
        status = DocStatus.COMPLETED if i < 3 else DocStatus.FAILED
        doc = DocumentMetadata(
            id=f"doc_{i}",
            name=f"file{i}.pdf",
            type="ingestion",
            status=status,
            output_format=OutputFormat.JSON,
            submitted_at="2024-01-01T00:00:00Z",
            job_id="test_job",
            metadata={}
        )
        db_store.save_document(doc)
    
    # Test filtering by status
    completed_docs = db_store.list_documents(status=DocStatus.COMPLETED.value)
    assert len(completed_docs) == 3
    
    failed_docs = db_store.list_documents(status=DocStatus.FAILED.value)
    assert len(failed_docs) == 2

def test_pagination(db_store):
    """Test pagination using ORM limit/offset."""
    # Create 10 test documents
    for i in range(10):
        doc = DocumentMetadata(
            id=f"doc_{i}",
            name=f"file{i}.pdf",
            type="ingestion",
            status=DocStatus.COMPLETED,
            output_format=OutputFormat.JSON,
            submitted_at=f"2024-01-01T00:00:{i:02d}Z",
            job_id="test_job",
            metadata={}
        )
        db_store.save_document(doc)
    
    # Test pagination
    page1 = db_store.list_documents(limit=5, offset=0)
    page2 = db_store.list_documents(limit=5, offset=5)
    
    assert len(page1) == 5
    assert len(page2) == 5
    assert page1[0].id != page2[0].id
```

#### 3.2 Integration Tests

**File: `tests/integration/test_status_manager.py`**
```python
import pytest
import concurrent.futures
from digitize.status import StatusManager
from digitize.types import DocStatus, JobStatus

def test_concurrent_document_updates(db_store):
    """Test concurrent document updates using StatusManager with ORM."""
    job_id = "concurrent_test_job"
    status_mgr = StatusManager(job_id)
    
    # Create test documents
    doc_ids = [f"doc_{i}" for i in range(10)]
    for doc_id in doc_ids:
        doc = DocumentMetadata(
            id=doc_id,
            name=f"{doc_id}.pdf",
            type="ingestion",
            status=DocStatus.IN_PROGRESS,
            output_format=OutputFormat.JSON,
            submitted_at="2024-01-01T00:00:00Z",
            job_id=job_id,
            metadata={}
        )
        db_store.save_document(doc)
    
    # Update documents concurrently
    def update_doc(doc_id):
        status_mgr.update_doc_metadata(doc_id, {"pages": 100})
        status_mgr.update_job_progress(doc_id, DocStatus.COMPLETED, JobStatus.IN_PROGRESS)
    
    with concurrent.futures.ThreadPoolExecutor(max_workers=10) as executor:
        futures = [executor.submit(update_doc, doc_id) for doc_id in doc_ids]
        concurrent.futures.wait(futures)
    
    # Verify all documents were updated
    for doc_id in doc_ids:
        doc = db_store.get_document(doc_id)
        assert doc.status == DocStatus.COMPLETED.value
        assert doc.metadata.get("pages") == 100
```

#### 3.3 Performance Testing

**Load Test Script: `tests/load_test_db.py`**
```python
import time
import concurrent.futures
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
from digitize.db_store import MetadataStore, Base
from digitize.document import DocumentMetadata
from digitize.types import DocStatus, OutputFormat

def setup_test_db():
    """Setup test database with proper connection pooling."""
    engine = create_engine(
        'postgresql://user:pass@localhost/testdb',
        pool_size=10,
        max_overflow=20,
        pool_pre_ping=True
    )
    Base.metadata.create_all(engine)
    return engine

def create_test_document(doc_id: str, engine):
    """Create document using ORM session."""
    Session = sessionmaker(bind=engine)
    session = Session()
    try:
        store = MetadataStore()
        store.session = session
        
        doc = DocumentMetadata(
            id=doc_id,
            name=f"test_{doc_id}.pdf",
            type="ingestion",
            status=DocStatus.ACCEPTED,
            output_format=OutputFormat.JSON,
            submitted_at="2024-01-01T00:00:00Z",
            job_id="load_test_job",
            metadata={"pages": 100}
        )
        store.save_document(doc)
        return doc_id
    finally:
        session.close()

def test_concurrent_writes(num_documents=1000, num_workers=32):
    """Test concurrent write performance with SQLAlchemy ORM."""
    engine = setup_test_db()
    start_time = time.time()
    
    with concurrent.futures.ThreadPoolExecutor(max_workers=num_workers) as executor:
        futures = [executor.submit(create_test_document, f"doc_{i}", engine)
                  for i in range(num_documents)]
        results = [f.result() for f in concurrent.futures.as_completed(futures)]
    
    elapsed = time.time() - start_time
    print(f"✅ Created {len(results)} documents in {elapsed:.2f}s")
    print(f"📊 Throughput: {len(results)/elapsed:.2f} docs/sec")
    print(f"⚡ Avg latency: {elapsed/len(results)*1000:.2f}ms per document")

def test_query_performance(engine):
    """Test query performance with ORM."""
    Session = sessionmaker(bind=engine)
    session = Session()
    store = MetadataStore()
    store.session = session
    
    # Test pagination
    start = time.time()
    docs = store.list_documents(limit=100, offset=0)
    print(f"📄 Paginated query (100 docs): {(time.time()-start)*1000:.2f}ms")
    
    # Test filtered query
    start = time.time()
    docs = store.list_documents(status=DocStatus.COMPLETED.value, limit=100)
    print(f"🔍 Filtered query: {(time.time()-start)*1000:.2f}ms")
    
    session.close()

if __name__ == "__main__":
    print("🚀 Starting load tests with SQLAlchemy ORM...")
    test_concurrent_writes()
    print("\n📊 Running query performance tests...")
    test_query_performance(setup_test_db())
```

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
- [ ] Rollback plan documented

#### 4.2 Rollback Plan

If issues arise:
1. Switch back to JSON file storage (keep old code path)
2. Export data from PostgreSQL back to JSON files
3. Investigate and fix issues
4. Re-attempt migration

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
