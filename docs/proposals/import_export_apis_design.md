# Import/Export API Design for Digitize Service

## Executive Summary

This document proposes a comprehensive design for Import and Export API endpoints in the digitize service to replace the current migration script approach. These endpoints will enable:
1. **Import API**: Accept JSON metadata over the network and create PostgreSQL database records
2. **Export API**: Return PostgreSQL database records as JSON in the response body for backup/restore

### Key Design Decision: Network-Based Approach

**Both Import and Export APIs use network-based data transfer**, accepting/returning JSON directly in request/response bodies rather than using file system paths. This design provides:

- **Better Security**: No file system access required, preventing path traversal attacks
- **Cloud-Native**: Works seamlessly in containerized/serverless environments
- **Flexibility**: Clients can send data from any source (files, databases, APIs)
- **Simplicity**: No need to mount volumes or manage file permissions
- **Testability**: Easy to test with mock data without file system setup

**Trade-offs**:
- Request size limits (50MB recommended)
- May require multiple API calls for very large datasets (>10,000 records)
- Network bandwidth considerations for large imports

**For large datasets**: Split into multiple requests with ~1,000 records each for optimal performance.

## Current State Analysis

### Current JSON Structure
**Job Status Files**: `{CACHE_DIR}/jobs/*_status.json`
```json
{
  "job_id": "uuid",
  "operation": "ingestion|digitization",
  "status": "accepted|in_progress|completed|failed",
  "job_name": "optional-name",
  "submitted_at": "ISO-8601-timestamp",
  "completed_at": "ISO-8601-timestamp",
  "stats": {
    "total_documents": 0,
    "completed": 0,
    "failed": 0,
    "in_progress": 0
  }
}
```

**Document Metadata Files**: `{CACHE_DIR}/docs/*_metadata.json`
```json
{
  "id": "uuid",
  "job_id": "uuid",
  "name": "filename.pdf",
  "type": "ingestion|digitization",
  "status": "accepted|in_progress|digitized|processed|chunked|completed|failed",
  "output_format": "txt|md|json",
  "submitted_at": "ISO-8601-timestamp",
  "completed_at": "ISO-8601-timestamp",
  "error": "error message if failed",
  "metadata": {
    "pages": 10,
    "tables": 5,
    "language": "en",
    "timing": {...}
  }
}
```

## Proposed API Design

### 1. Import API - JSON to Database

#### Endpoint
```
POST /v1/admin/import
```

#### Purpose
Import metadata from JSON data sent over the network into PostgreSQL database. Supports both initial migration and incremental imports.

#### Request Body
```json
{
  "jobs": [
    {
      "job_id": "job-uuid-123",
      "operation": "ingestion",
      "status": "completed",
      "job_name": "My Import Job",
      "submitted_at": "2024-01-15T10:00:00Z",
      "completed_at": "2024-01-15T10:30:00Z",
      "stats": {
        "total_documents": 10,
        "completed": 8,
        "failed": 2,
        "in_progress": 0
      }
    }
  ],
  "documents": [
    {
      "id": "doc-uuid-456",
      "job_id": "job-uuid-123",
      "name": "document.pdf",
      "type": "ingestion",
      "status": "completed",
      "output_format": "txt",
      "submitted_at": "2024-01-15T10:00:00Z",
      "completed_at": "2024-01-15T10:15:00Z",
      "error": null,
      "metadata": {
        "pages": 10,
        "tables": 5,
        "language": "en"
      }
    }
  ],
  "options": {
    "mode": "merge",
    "validate_only": false,
    "batch_size": 100
  }
}
```

#### Request Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `jobs` | array | Yes | [] | Array of job metadata objects to import |
| `documents` | array | Yes | [] | Array of document metadata objects to import |
| `options.mode` | enum | No | "merge" | Import strategy: "merge" (upsert), "replace" (delete+insert), "skip" (skip existing) |
| `options.validate_only` | boolean | No | false | Dry-run mode: validate without importing |
| `options.batch_size` | integer | No | 100 | Number of records to process per batch |

#### Response (Success - 200 OK)
```json
{
  "status": "completed",
  "summary": {
    "jobs": {
      "total_found": 150,
      "imported": 145,
      "skipped": 3,
      "failed": 2
    },
    "documents": {
      "total_found": 1500,
      "imported": 1480,
      "skipped": 15,
      "failed": 5
    }
  },
  "duration_seconds": 12.5,
  "errors": [
    {
      "file": "/path/to/job_123_status.json",
      "type": "validation_error",
      "message": "Missing required field: job_id"
    }
  ],
  "warnings": [
    {
      "file": "/path/to/doc_456_metadata.json",
      "type": "orphaned_document",
      "message": "Document references non-existent job_id: job-999"
    }
  ]
}
```

#### Error Responses

**400 Bad Request** - Invalid request parameters
```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid job data: missing required field 'job_id' in jobs[0]",
    "status": 400,
    "details": {
      "validation_errors": [
        {
          "index": 0,
          "type": "job",
          "field": "job_id",
          "message": "Required field missing"
        }
      ]
    }
  }
}
```

**413 Payload Too Large** - Request body exceeds size limit
```json
{
  "error": {
    "code": "PAYLOAD_TOO_LARGE",
    "message": "Request body exceeds maximum size of 50MB",
    "status": 413,
    "details": {
      "max_size_bytes": 52428800,
      "received_size_bytes": 60000000
    }
  }
}
```

**409 Conflict** - Active jobs prevent import
```json
{
  "error": {
    "code": "RESOURCE_LOCKED",
    "message": "Cannot import while jobs are active. Active jobs: job-1, job-2",
    "status": 409,
    "details": {
      "active_jobs": ["job-1", "job-2"]
    }
  }
}
```

**500 Internal Server Error** - Database or system error
```json
{
  "error": {
    "code": "INTERNAL_SERVER_ERROR",
    "message": "Database connection failed during import",
    "status": 500
  }
}
```

---

### 2. Export API - Database to JSON

#### Endpoint
```
POST /v1/admin/export
```

#### Purpose
Export metadata from PostgreSQL database as JSON data in the response body for backup/restore purposes.

#### Request Body
```json
{
  "filters": {
    "job_ids": [],
    "job_status": [],
    "date_range": {
      "start": "2024-01-01T00:00:00Z",
      "end": "2024-12-31T23:59:59Z"
    }
  },
  "options": {
    "include_completed": true,
    "include_failed": true,
    "include_active": false,
    "limit": 10000
  }
}
```

#### Request Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `filters.job_ids` | array | No | [] | Specific job IDs to export (empty = all) |
| `filters.job_status` | array | No | [] | Filter by job status (empty = all) |
| `filters.date_range` | object | No | null | Filter by submission date range |
| `options.include_completed` | boolean | No | true | Include completed jobs/documents |
| `options.include_failed` | boolean | No | true | Include failed jobs/documents |
| `options.include_active` | boolean | No | false | Include active jobs/documents (not recommended for backup) |
| `options.limit` | integer | No | 10000 | Maximum number of records to export (jobs + documents combined) |

#### Response (Success - 200 OK)
```json
{
  "status": "completed",
  "data": {
    "jobs": [
      {
        "job_id": "job-uuid-123",
        "operation": "ingestion",
        "status": "completed",
        "job_name": "My Job",
        "submitted_at": "2024-01-15T10:00:00Z",
        "completed_at": "2024-01-15T10:30:00Z",
        "stats": {
          "total_documents": 10,
          "completed": 8,
          "failed": 2,
          "in_progress": 0
        }
      }
    ],
    "documents": [
      {
        "id": "doc-uuid-456",
        "job_id": "job-uuid-123",
        "name": "document.pdf",
        "type": "ingestion",
        "status": "completed",
        "output_format": "txt",
        "submitted_at": "2024-01-15T10:00:00Z",
        "completed_at": "2024-01-15T10:15:00Z",
        "error": null,
        "metadata": {
          "pages": 10,
          "tables": 5,
          "language": "en"
        }
      }
    ]
  },
  "summary": {
    "jobs": {
      "total_exported": 145,
      "completed": 120,
      "failed": 25
    },
    "documents": {
      "total_exported": 1480,
      "completed": 1400,
      "failed": 80
    }
  },
  "export_timestamp": "2024-01-15T10:30:00Z",
  "duration_seconds": 2.5,
  "pagination": {
    "has_more": false,
    "total_records": 1625,
    "returned_records": 1625
  }
}
```

#### Error Responses

**400 Bad Request** - Invalid request parameters
```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid date range: start date must be before end date",
    "status": 400
  }
}
```

**413 Payload Too Large** - Too many records requested
```json
{
  "error": {
    "code": "PAYLOAD_TOO_LARGE",
    "message": "Export would exceed maximum response size. Requested: 50000 records, Maximum: 10000",
    "status": 413,
    "details": {
      "max_records": 10000,
      "requested_records": 50000,
      "suggestion": "Use filters or pagination to reduce result set"
    }
  }
}
```

**500 Internal Server Error** - Database or system error
```json
{
  "error": {
    "code": "INTERNAL_SERVER_ERROR",
    "message": "Database query failed during export",
    "status": 500
  }
}
```

## Corner Cases and Solutions

### 1. Concurrent Operations

**Problem**: Multiple import/export operations running simultaneously

**Solution**:
- Implement operation locking using database or file-based locks
- Return 409 Conflict if operation already in progress
- Track active operations in memory or database


### 2. Active Jobs During Import

**Problem**: Importing while jobs are running could cause data inconsistency

**Solution**:
- Check for active jobs before import
- Reject import if active jobs exist (unless force flag is set)
- Provide option to wait for active jobs to complete

### 3. Orphaned Documents

**Problem**: Documents referencing non-existent jobs

**Solution**:
- Validate job_id references before importing documents
- Skip orphaned documents with warning
- Provide option to import orphaned documents without job_id


### 4. Large Dataset Handling

**Problem**: Importing/exporting thousands of records could timeout or exhaust memory

**Solution**:
- Implement batch processing with configurable batch size
- Use streaming for large exports
- Provide progress tracking

```python
async def import_in_batches(
    jobs: List[dict],
    documents: List[dict],
    batch_size: int = 100
) -> ImportResult:
    """Import records in batches to avoid memory issues."""
    results = ImportResult()
    
    # Process jobs in batches
    total_jobs = len(jobs)
    for i in range(0, total_jobs, batch_size):
        batch = jobs[i:i + batch_size]
        batch_result = await process_job_batch(batch)
        results.merge_jobs(batch_result)
        
        # Commit after each batch
        db_manager.commit()
        
        # Log progress
        logger.info(f"Processed {min(i + batch_size, total_jobs)}/{total_jobs} jobs")
    
    # Process documents in batches
    total_docs = len(documents)
    for i in range(0, total_docs, batch_size):
        batch = documents[i:i + batch_size]
        batch_result = await process_document_batch(batch)
        results.merge_documents(batch_result)
        
        # Commit after each batch
        db_manager.commit()
        
        # Log progress
        logger.info(f"Processed {min(i + batch_size, total_docs)}/{total_docs} documents")
    
    return results
```

### 5. Request Size Limits

**Problem**: Large payloads could exhaust memory or cause timeouts

**Solution**:
- Implement request body size limits (e.g., 50MB)
- Return 413 Payload Too Large for oversized requests
- Suggest chunking for very large imports
- Document recommended batch sizes

```python
MAX_IMPORT_SIZE_BYTES = 50 * 1024 * 1024  # 50MB
MAX_RECORDS_PER_REQUEST = 10000

**Recommendation**: For imports with >10,000 records, split into multiple API calls with ~1,000 records each.

### 6. Duplicate Records

**Problem**: Importing same data multiple times

**Solution**:
- Support three modes: merge (upsert), replace (delete+insert), skip (ignore existing)
- Use database primary keys and unique constraints
- Track and report duplicates in response

### 7. Partial Failures

**Problem**: Some records fail while others succeed

**Solution**:
- Use database transactions per batch
- Collect all errors and warnings
- Return detailed error report
- Allow continuation after failures

### 9. Export Response Size Management

**Problem**: Large exports could exceed response size limits or cause timeouts

**Solution**:
- Implement record limits (default: 10,000 records)
- Return pagination information
- Support filtering to reduce result set
- Suggest multiple requests for very large exports

### 10. Backup/Restore Workflow

**Problem**: Ensuring reliable backup and restore process with network-based APIs

**Solution**:
- Client saves export response containing json metadata structures to local files
- Client validates data integrity
- Client sends data back via Import API for restore
- Support incremental backups via filters

## Conclusion

This design provides a robust, API-driven approach to metadata import/export that:
- ✅ Enables automated backup/restore workflows
- ✅ Provides comprehensive error handling
- ✅ Supports various use cases and edge cases
- ✅ Maintains data integrity and consistency
- ✅ Scales to large datasets
- ✅ Follows REST API best practices
