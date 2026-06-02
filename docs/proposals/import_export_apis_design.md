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
POST /v1/import
```

#### Purpose
Import metadata from JSON data sent over the network into PostgreSQL database. Supports both initial migration and incremental imports. The request body structure matches the Export API response format for seamless backup/restore workflows.

#### Request Body
```json
{
  "data": {
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
    ]
  },
  "validate_only": false
}
```

#### Request Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `data.jobs` | array | Yes | [] | Array of job metadata objects to import |
| `data.documents` | array | Yes | [] | Array of document metadata objects to import |
| `validate_only` | boolean | No | false | Dry-run mode: validate without importing |

**Note**:
- The import uses "skip" mode by default - existing records with the same ID are skipped, only new records are inserted
- The `data` structure matches the Export API response format, allowing direct use of export responses for import
- For large datasets exported with pagination, import each batch separately by calling the Import API multiple times
- Internal batch processing (100 records per transaction) is handled automatically for memory efficiency

#### Response (Success - 200 OK)
```json
{
  "status": "completed",
  "summary": {
    "jobs": {
      "total_received": 150,
      "imported": 145,
      "skipped": 3,
      "failed": 2
    },
    "documents": {
      "total_received": 1500,
      "imported": 1480,
      "skipped": 15,
      "failed": 5
    }
  },
  "duration_seconds": 12.5,
  "errors": [
    {
      "record_type": "job",
      "record_id": "job-uuid-123",
      "type": "validation_error",
      "message": "Missing required field: job_id"
    }
  ],
  "warnings": [
    {
      "record_type": "document",
      "record_id": "doc-uuid-456",
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
    "message": "Invalid job data: missing required field 'job_id' in data.jobs[0]",
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
GET /v1/export?limit=10000&offset=0
```

#### Purpose
Export metadata from PostgreSQL database as JSON data in the response body for backup/restore purposes.

#### Query Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `limit` | integer | No | 10000 | Maximum number of records to export per request (jobs + documents combined) |
| `offset` | integer | No | 0 | Number of records to skip for pagination |

#### Example Requests
```
GET /v1/export
GET /v1/export?limit=5000
GET /v1/export?limit=10000&offset=10000
```

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
    "limit": 10000,
    "offset": 0,
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
    "message": "Invalid pagination parameters: offset cannot be negative",
    "status": 400,
    "details": {
      "parameter": "offset",
      "value": -10
    }
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
      "requested_limit": 50000,
      "suggestion": "Use pagination with limit <= 10000 and iterate using offset"
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
- Use "skip" mode: existing records are skipped, only new records are inserted
- Use database primary keys and unique constraints to detect duplicates
- Track and report skipped duplicates in response

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
- Return pagination information (limit, offset, has_more, total_records)
- Support offset-based pagination via query parameters
- Suggest multiple requests for very large exports

**Example Pagination Workflow**:
```bash
# First request
GET /v1/export?limit=10000&offset=0

# Second request (if has_more is true)
GET /v1/export?limit=10000&offset=10000

# Third request (if has_more is true)
GET /v1/export?limit=10000&offset=20000

# Continue until has_more is false
```

### 10. Backup/Restore Workflow

**Problem**: Ensuring reliable backup and restore process with network-based APIs when dealing with large datasets

**Solution**:

**Backup Process (Export with Pagination)**:
1. Backup script calls Export API with `limit` and `offset` query parameters
2. Script iterates through all pages using pagination (`has_more` flag)
3. Accumulates all jobs and documents from paginated responses
4. Saves complete dataset to local backup file

**Restore Process (Import from Backup)**:
1. Restore script loads backup file containing jobs and documents
2. Sends data to Import API using the `data` structure (same format as Export response)
3. Import API processes records and returns summary of imported/skipped/failed records
4. For very large backups, can split into multiple Import API calls with smaller chunks

**Key Points**:
- Export API returns paginated data with `limit` and `offset` query parameters
- Backup script iterates through all pages and accumulates records into a single backup file
- Import API accepts the same `data` structure that Export API returns
- The `data` structure is consistent between Export response and Import request, enabling seamless workflows
- For very large datasets, restore can be split into multiple Import API calls

## Conclusion

This design provides a robust, API-driven approach to metadata import/export that:
- ✅ Enables automated backup/restore workflows
- ✅ Provides comprehensive error handling
- ✅ Supports various use cases and edge cases
- ✅ Maintains data integrity and consistency
- ✅ Scales to large datasets
- ✅ Follows REST API best practices
