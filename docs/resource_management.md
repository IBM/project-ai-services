# Resource Management Improvements

## Overview

This document describes the resource management improvements implemented to address issues with staging directory cleanup, failed job handling, and disk space management in the spyre-rag digitization service.

## Issues Addressed

### 1. Staging Directory Cleanup
**Problem**: Staging directories were only cleaned up in the `finally` block, which could lead to accumulation on crashes or unexpected terminations.

**Solution**: 
- Implemented `safe_cleanup()` function with retry logic for robust cleanup
- Added startup cleanup to remove orphaned staging directories from previous crashes
- Implemented periodic cleanup scheduler that runs every hour to clean up old staging directories (>24 hours old)
- Cleanup now happens in multiple places:
  - On successful job completion (finally block)
  - On job failure (exception handler)
  - On application startup (cleanup orphaned directories)
  - Periodically via background scheduler

### 2. Failed Job Cleanup
**Problem**: No cleanup strategy existed for failed jobs, leading to accumulation of partial data.

**Solution**:
- Created `cleanup_failed_job()` function that removes:
  - Staging directory for the job
  - Document metadata files
  - Intermediate processing files (converted JSON, text, tables, chunks)
  - Checksum files
- Implemented retention policy for failed jobs (7 days by default)
- Periodic cleanup runs daily to remove old failed jobs
- Failed job cleanup is triggered:
  - Immediately on job failure
  - Periodically for old failed jobs (>7 days)

### 3. Disk Space Checks
**Problem**: No disk space validation before processing, which could lead to failures mid-processing.

**Solution**:
- Implemented `check_disk_space()` function that validates available space before job acceptance
- Estimates required space based on input file sizes (3x multiplier for intermediate files)
- Returns HTTP 507 (Insufficient Storage) if space is inadequate
- Minimum free space requirement: 5 GB (configurable)
- Disk space is checked before:
  - Accepting new document processing requests
  - Starting file staging operations

## New Components

### 1. `resource_utils.py`
Core resource management utilities module providing:

#### Functions:
- `check_disk_space(path, required_gb)` - Validates available disk space
- `estimate_required_space(file_sizes, multiplier)` - Estimates space needed for processing
- `cleanup_staging_directory(staging_dir, job_id, force)` - Cleans staging directories
- `cleanup_failed_job(job_id, cache_dir)` - Removes all resources for a failed job
- `cleanup_old_failed_jobs(cache_dir, retention_days)` - Removes old failed job data
- `safe_cleanup(path, max_retries)` - Robust cleanup with retry logic
- `get_directory_size(path)` - Calculates total directory size
- `log_resource_usage(cache_dir)` - Logs disk usage statistics

#### Constants:
- `MIN_FREE_SPACE_GB = 5` - Minimum required free space
- `STAGING_CLEANUP_AGE_HOURS = 24` - Age threshold for staging cleanup
- `FAILED_JOB_RETENTION_DAYS = 7` - Retention period for failed jobs

### 2. `cleanup_scheduler.py`
Periodic cleanup scheduler for automated resource management:

#### CleanupScheduler Class:
- Manages three periodic background tasks:
  1. **Staging Cleanup** (every 1 hour) - Removes old staging directories
  2. **Failed Jobs Cleanup** (every 24 hours) - Removes old failed job data
  3. **Resource Logging** (every 30 minutes) - Logs disk usage statistics

#### Lifecycle:
- Started on application startup
- Stopped on application shutdown
- All tasks run asynchronously without blocking main application

## Integration Points

### app.py Changes

1. **Startup Event**:
```python
@app.on_event("startup")
async def startup_event():
    # Clean up orphaned staging directories
    cleanup_staging_directory(STAGING_DIR, force=False)
    # Clean up old failed jobs
    cleanup_old_failed_jobs(CACHE_DIR)
    # Log initial resource usage
    log_resource_usage(CACHE_DIR)
    # Start periodic cleanup scheduler
    await start_cleanup_scheduler(CACHE_DIR)
```

2. **Shutdown Event**:
```python
@app.on_event("shutdown")
async def shutdown_event():
    # Stop periodic cleanup scheduler
    await stop_cleanup_scheduler()
```

3. **Document Processing Endpoint**:
```python
# Check disk space before accepting job
check_disk_space(CACHE_DIR, required_gb=estimated_space)
```

4. **Ingestion Function**:
```python
try:
    # Process documents
    await asyncio.to_thread(ingest, job_staging_path, job_id, doc_id_dict)
except Exception as e:
    # Clean up failed job resources
    cleanup_failed_job(job_id, CACHE_DIR)
finally:
    # Always clean up staging directory
    safe_cleanup(str(job_staging_path))
```

### doc_utils.py Changes

1. **Import Resource Utilities**:
```python
from digitize.resource_utils import safe_cleanup, log_resource_usage
```

2. **Enhanced Error Handling**:
```python
except Exception as e:
    # Clean up intermediate files for failed documents
    for doc_id in failed_docs:
        for pattern in [f"{doc_id}.json", f"{doc_id}_text.json", ...]:
            safe_cleanup(str(file_path))
finally:
    # Log resource usage after processing
    log_resource_usage(out_path)
```

## Configuration

### Environment Variables (Optional)
You can customize resource management behavior through environment variables:

```bash
# Minimum free space required (GB)
export MIN_FREE_SPACE_GB=10

# Staging cleanup age threshold (hours)
export STAGING_CLEANUP_AGE_HOURS=12

# Failed job retention period (days)
export FAILED_JOB_RETENTION_DAYS=3
```

### Cleanup Intervals
Modify intervals in `cleanup_scheduler.py`:

```python
STAGING_CLEANUP_INTERVAL = 3600      # 1 hour
FAILED_JOBS_CLEANUP_INTERVAL = 86400 # 24 hours
RESOURCE_LOG_INTERVAL = 1800         # 30 minutes
```

## Monitoring

### Resource Usage Logs
The system now logs disk usage information:

```
INFO: Disk usage: 45.2/100.0 GB used, 54.8 GB free (54.8% free)
INFO: staging directory size: 1234.56 MB
INFO: docs directory size: 5678.90 MB
INFO: jobs directory size: 123.45 MB
```

### Cleanup Activity Logs
Cleanup operations are logged for monitoring:

```
INFO: Cleaned up 3 old staging directories
INFO: Cleaned up resources for failed job: abc-123-def
INFO: Cleaned up 2 old failed jobs
```

## Error Handling

### Disk Space Errors
When insufficient disk space is detected:
- HTTP 507 (Insufficient Storage) is returned
- Error message includes required vs. available space
- Job is not accepted or started

### Cleanup Failures
If cleanup operations fail:
- Errors are logged but don't block main operations
- Retry logic attempts cleanup up to 3 times
- Periodic cleanup will retry on next scheduled run

## Best Practices

1. **Monitor Disk Usage**: Regularly check logs for disk usage trends
2. **Adjust Retention Policies**: Modify retention periods based on your needs
3. **Review Failed Jobs**: Investigate patterns in failed jobs before cleanup
4. **Set Appropriate Limits**: Configure minimum free space based on typical job sizes
5. **Test Cleanup**: Verify cleanup operations work correctly in your environment

## Troubleshooting

### Staging Directories Not Cleaning Up
- Check if cleanup scheduler is running (look for startup logs)
- Verify file permissions on staging directory
- Check for processes holding file locks

### Disk Space Warnings Despite Cleanup
- Verify cleanup intervals are appropriate for your workload
- Check if other processes are consuming disk space
- Consider reducing retention periods

### Failed Job Data Accumulating
- Verify failed jobs cleanup is running (check logs every 24 hours)
- Check if job status files are being created correctly
- Ensure proper permissions on cache directories

## Future Enhancements

Potential improvements for consideration:

1. **Configurable Cleanup Policies**: Make all thresholds configurable via API
2. **Cleanup Metrics**: Export cleanup metrics to monitoring systems
3. **Selective Cleanup**: Allow manual cleanup of specific jobs via API
4. **Disk Quota Management**: Implement per-user or per-tenant quotas
5. **Compression**: Compress old job data before deletion
6. **Archive Strategy**: Archive important failed jobs before cleanup

## Testing

To test resource management improvements:

1. **Disk Space Check**:
```bash
# Fill disk to trigger space check
dd if=/dev/zero of=/var/cache/testfile bs=1G count=50
# Try to submit job - should fail with 507
curl -X POST http://localhost:4000/v1/documents -F "files=@test.pdf"
```

2. **Staging Cleanup**:
```bash
# Create old staging directory
mkdir -p /var/cache/staging/test-job-123
touch -t 202301010000 /var/cache/staging/test-job-123
# Wait for cleanup or restart service
# Verify directory is removed
```

3. **Failed Job Cleanup**:
```bash
# Simulate failed job
# Wait 7+ days or modify retention period
# Verify cleanup removes job data
```

## Summary

These resource management improvements provide:
- ✅ Robust cleanup that handles crashes and failures
- ✅ Proactive disk space validation
- ✅ Automated cleanup of old resources
- ✅ Better visibility into resource usage
- ✅ Configurable retention policies
- ✅ Retry logic for resilient operations

The system is now more resilient to resource exhaustion and better manages disk space over time.