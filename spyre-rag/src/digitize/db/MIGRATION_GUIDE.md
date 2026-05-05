# PostgreSQL Migration Guide

This guide explains how to migrate existing JSON-based metadata to PostgreSQL database.

## Overview

The migration utility (`migrate_json_to_postgres.py`) transfers job and document metadata from JSON files to PostgreSQL database using SQLAlchemy ORM's `session.merge()` for upsert operations.

## Prerequisites

### 1. Database Components

Before running migration, ensure these components are in place:

- **Database Models** (`spyre-rag/src/digitize/db/models.py`) - SQLAlchemy ORM models
- **Database Configuration** (`spyre-rag/src/digitize/db/database.py`) - Connection and session management
- **Database Schema** - Tables created via init container or manual execution

### 2. Dependencies

Required Python packages (should be in `requirements.txt`):
```
sqlalchemy>=2.0.0
psycopg2-binary>=2.9.0
```

### 3. PostgreSQL Instance

- PostgreSQL 13+ running and accessible
- Database created (e.g., `digitize_metadata`)
- User with appropriate permissions

### 4. Environment Variables

Set these before running migration:

```bash
export POSTGRES_HOST="localhost"           # or pod name for Podman
export POSTGRES_PORT="5432"
export POSTGRES_DB="digitize_metadata"
export POSTGRES_USER="digitize_user"
export POSTGRES_PASSWORD="your-password"
```

## Migration Process

### Step 1: Dry Run (Recommended)

Always perform a dry run first to verify what will be migrated:

```bash
cd spyre-rag/src/digitize
python -m db.migrate_json_to_postgres --dry-run
```

**Output Example:**
```
======================================================================
PostgreSQL Migration Utility
======================================================================
🔍 DRY RUN MODE - No changes will be made
Jobs directory: /var/cache/jobs
Docs directory: /var/cache/docs

======================================================================
Migrating Jobs
======================================================================
Found 5 job status files to migrate
[DRY-RUN] Would migrate job: job-123 (My Job)
[DRY-RUN] Would migrate job: job-456 (Another Job)
...
[DRY-RUN] Would migrate 5 jobs, 0 would fail

======================================================================
Migrating Documents
======================================================================
Found 150 document metadata files to migrate
[DRY-RUN] Would migrate document: doc-001 (document1.pdf)
[DRY-RUN] Would migrate document: doc-002 (document2.pdf)
...
[DRY-RUN] Would migrate 150 documents, 0 would fail

======================================================================
Migration Summary
======================================================================
Jobs:      5 migrated, 0 failed
Documents: 150 migrated, 0 failed

💡 JSON files retained. Use --cleanup flag to remove them after migration.
```

### Step 2: Actual Migration

Once dry run looks good, perform the actual migration:

```bash
python -m db.migrate_json_to_postgres
```

**Features:**
- ✅ **Idempotent** - Safe to run multiple times (uses upsert)
- ✅ **Transactional** - All-or-nothing per entity type
- ✅ **Progress Reporting** - Shows progress every 50 jobs / 100 documents
- ✅ **Error Handling** - Continues on individual file errors, reports at end

### Step 3: Verify Migration

After migration, verify the data:

```bash
# Connect to PostgreSQL
podman exec -it <app-name>--postgres-postgres psql -U digitize_user -d digitize_metadata

# Check counts
SELECT COUNT(*) FROM jobs;
SELECT COUNT(*) FROM documents;

# Sample data
SELECT job_id, job_name, status, submitted_at FROM jobs LIMIT 5;
SELECT doc_id, name, status, submitted_at FROM documents LIMIT 5;

# Exit
\q
```

### Step 4: Cleanup (Optional)

After verifying migration success, optionally remove JSON files:

```bash
python -m db.migrate_json_to_postgres --cleanup
```

**⚠️ Warning:** This permanently deletes JSON files. Only do this after:
1. Verifying all data migrated successfully
2. Testing application with PostgreSQL backend
3. Having a backup if needed

## Migration Scenarios

### Scenario 1: Fresh Migration

No existing data in PostgreSQL:

```bash
# 1. Dry run
python -m db.migrate_json_to_postgres --dry-run

# 2. Migrate
python -m db.migrate_json_to_postgres

# 3. Verify
# ... check database ...

# 4. Cleanup
python -m db.migrate_json_to_postgres --cleanup
```

### Scenario 2: Re-migration (Update Existing Data)

Data already exists in PostgreSQL, need to update:

```bash
# Migration will upsert (update existing, insert new)
python -m db.migrate_json_to_postgres

# JSON files remain for safety
```

### Scenario 3: Partial Migration Recovery

Some files failed during previous migration:

```bash
# Fix the problematic JSON files
# Then re-run migration (will skip already migrated files via upsert)
python -m db.migrate_json_to_postgres
```

## Data Mapping

### Job Metadata

**JSON File:** `/var/cache/jobs/<job_id>_status.json`

```json
{
  "job_id": "job-123",
  "job_name": "My Processing Job",
  "operation": "digitization",
  "status": "completed",
  "submitted_at": "2024-01-01T12:00:00Z",
  "completed_at": "2024-01-01T12:30:00Z",
  "error": null,
  "stats": {
    "total_documents": 10,
    "completed": 10,
    "failed": 0,
    "in_progress": 0
  }
}
```

**PostgreSQL Table:** `jobs`

| Column | Type | Mapped From |
|--------|------|-------------|
| job_id | VARCHAR(255) | job_id |
| job_name | VARCHAR(500) | job_name |
| operation | VARCHAR(50) | operation |
| status | VARCHAR(50) | status |
| submitted_at | TIMESTAMP | submitted_at (parsed) |
| completed_at | TIMESTAMP | completed_at (parsed) |
| error | TEXT | error |
| stats | JSONB | stats |
| updated_at | TIMESTAMP | AUTO (trigger) |

### Document Metadata

**JSON File:** `/var/cache/docs/<doc_id>_metadata.json`

```json
{
  "id": "doc-001",
  "name": "document.pdf",
  "type": "digitization",
  "status": "completed",
  "output_format": "json",
  "submitted_at": "2024-01-01T12:00:00Z",
  "completed_at": "2024-01-01T12:05:00Z",
  "error": null,
  "job_id": "job-123",
  "metadata": {
    "pages": 10,
    "tables": 3,
    "timing_in_secs": {
      "digitizing": 5.2,
      "processing": 3.1,
      "chunking": 2.5,
      "indexing": 1.8
    }
  }
}
```

**PostgreSQL Table:** `documents`

| Column | Type | Mapped From |
|--------|------|-------------|
| doc_id | VARCHAR(255) | id |
| job_id | VARCHAR(255) | job_id (FK to jobs) |
| name | VARCHAR(500) | name |
| type | VARCHAR(50) | type |
| status | VARCHAR(50) | status |
| output_format | VARCHAR(10) | output_format |
| submitted_at | TIMESTAMP | submitted_at (parsed) |
| completed_at | TIMESTAMP | completed_at (parsed) |
| error | TEXT | error |
| metadata | JSONB | metadata |
| updated_at | TIMESTAMP | AUTO (trigger) |

## Troubleshooting

### Issue: Import Errors

**Error:**
```
ImportError: cannot import name 'SessionLocal' from 'digitize.db.database'
```

**Solution:**
Ensure database components are implemented:
- `spyre-rag/src/digitize/db/models.py`
- `spyre-rag/src/digitize/db/database.py`

### Issue: Connection Refused

**Error:**
```
psycopg2.OperationalError: could not connect to server: Connection refused
```

**Solution:**
1. Verify PostgreSQL is running: `podman ps | grep postgres`
2. Check environment variables are set correctly
3. Verify network connectivity to PostgreSQL host

### Issue: Permission Denied

**Error:**
```
psycopg2.errors.InsufficientPrivilege: permission denied for table jobs
```

**Solution:**
Grant permissions to user:
```sql
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO digitize_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO digitize_user;
```

### Issue: Foreign Key Violation

**Error:**
```
psycopg2.errors.ForeignKeyViolation: insert or update on table "documents" violates foreign key constraint
```

**Solution:**
This happens if a document references a non-existent job. The migration script handles this by:
1. Migrating jobs first
2. Continuing on individual document errors
3. Reporting failed files at the end

Check the failed files list and verify job_id references.

### Issue: Timestamp Parsing Error

**Error:**
```
ValueError: Invalid isoformat string
```

**Solution:**
Check JSON file for malformed timestamps. Expected format: `2024-01-01T12:00:00Z`

## Performance Considerations

### Large Datasets

For large datasets (>10,000 documents):

1. **Monitor Progress:**
   ```bash
   python -m db.migrate_json_to_postgres 2>&1 | tee migration.log
   ```

2. **Batch Commits:**
   The script commits after each entity type (jobs, then documents)

3. **Database Tuning:**
   Temporarily adjust PostgreSQL settings for bulk loading:
   ```sql
   -- Increase work memory
   SET work_mem = '256MB';
   
   -- Disable synchronous commit (careful!)
   SET synchronous_commit = OFF;
   ```

### Estimated Times

Based on typical hardware:

| Dataset Size | Estimated Time |
|--------------|----------------|
| 100 documents | < 1 second |
| 1,000 documents | ~5 seconds |
| 10,000 documents | ~30 seconds |
| 100,000 documents | ~5 minutes |

## Post-Migration Steps

### 1. Update Application Configuration

Enable database backend in application:

```bash
export USE_DATABASE_BACKEND="true"
```

### 2. Restart Services

Restart digitize service to use PostgreSQL:

```bash
podman pod restart <app-name>--digitize
```

### 3. Monitor Application

Watch logs for any database-related issues:

```bash
podman logs -f <app-name>--digitize-backend-server
```

### 4. Backup Strategy

Implement regular PostgreSQL backups:

```bash
# Example backup script
pg_dump -h localhost -U digitize_user -d digitize_metadata > backup_$(date +%Y%m%d).sql
```

## Rollback Plan

If issues arise after migration:

### Option 1: Keep JSON Files (Recommended)

Don't use `--cleanup` flag initially. This allows:
- Reverting to file-based storage if needed
- Re-running migration after fixes
- Comparing data between systems

### Option 2: Restore from Backup

If JSON files were deleted:
1. Restore PostgreSQL from backup
2. Or restore JSON files from system backup
3. Re-run migration

## Integration with Deployment

### As Init Container

The migration can run automatically as an init container:

```yaml
initContainers:
  - name: migrate-metadata
    image: <digitize-image>
    command: ["python", "-m", "digitize.db.migrate_json_to_postgres"]
    env:
      - name: POSTGRES_HOST
        value: "postgresql-service"
      - name: POSTGRES_DB
        value: "digitize_metadata"
      # ... other env vars ...
    volumeMounts:
      - name: digitize-data
        mountPath: /var/cache
```

### Manual Execution

For controlled migration:

```bash
# Enter the container
podman exec -it <app-name>--digitize-backend-server bash

# Run migration
cd /app
python -m digitize.db.migrate_json_to_postgres --dry-run
python -m digitize.db.migrate_json_to_postgres
```

## References

- Design Document: [`docs/proposals/digitize_metadata_db_migration.md`](../../../docs/proposals/digitize_metadata_db_migration.md)
- Implementation Status: [`docs/proposals/postgresql_implementation_status.md`](../../../docs/proposals/postgresql_implementation_status.md)
- Database Schema: [`spyre-rag/src/digitize/db/scripts/init_schema.sql`](scripts/init_schema.sql)