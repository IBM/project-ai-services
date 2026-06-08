import asyncio
import json
import uuid
from typing import List, Optional
from contextlib import asynccontextmanager
import uvicorn

from fastapi import FastAPI, UploadFile, File, HTTPException, BackgroundTasks, Query, status, Request
from fastapi.openapi.docs import get_swagger_ui_html
from lingua import Language

from common.diagnostic_logger import setup_comprehensive_crash_handler
from common.misc_utils import set_log_level, get_logger
from common.lang_utils import setup_language_detector
from digitize.settings import settings

set_log_level(settings.common.app.log_level)

from common.misc_utils import validate_document_file, set_request_id, configure_uvicorn_logging, cleanup_staging_directory
from common.error_utils import APIError, ErrorCode, http_error_responses, http_exception_handler
import digitize.digitize_utils as dg_util
import digitize.models as models
from digitize.digitize_core import digitize
from digitize.cleanup import reset_db
from digitize.ingest import ingest
from digitize.db_operations import get_status_manager
from digitize.db.connection import check_db_connection, close_db_connections
import digitize.db_operations as db_ops

# Semaphores for concurrency limiting
digitization_semaphore = asyncio.BoundedSemaphore(settings.digitize.digitization_concurrency_limit)
ingestion_semaphore = asyncio.BoundedSemaphore(settings.digitize.ingestion_concurrency_limit)

logger = get_logger("digitize_server")

diagnostic_logger, stderr_monitor, signal_handler = setup_comprehensive_crash_handler(logger)

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifespan events (startup and shutdown)."""
    # Startup
    filtered_paths = ['/health', '/v1/jobs']
    configure_uvicorn_logging(settings.common.app.log_level, filtered_paths)
    logger.info("Application starting up...")

    # Initialize language detector for document processing
    try:
        setup_language_detector([Language.ENGLISH, Language.GERMAN, Language.ITALIAN, Language.FRENCH])
        logger.info("Language detector initialized for EN, DE, IT, FR")
    except Exception as e:
        logger.error(f"Error initializing language detector: {e}", exc_info=True)

    # Check database connection (required for ingestion/digitize operation)
    try:
        if check_db_connection():
            logger.info("✅ Database connection established")
            
            # Initialize database schema (create tables if they don't exist)
            try:
                from digitize.db.models import Base
                from digitize.db.connection import engine
                if engine is None:
                    raise RuntimeError("Database engine is not initialized")
                Base.metadata.create_all(bind=engine)
                logger.info("✅ Database schema initialized")
            except Exception as schema_error:
                logger.error(f"❌ Failed to initialize database schema: {schema_error}")
                raise RuntimeError(f"Database schema initialization failed: {schema_error}")
        else:
            logger.error("❌ Database connection failed - service requires database to operate")
            raise RuntimeError("Database connection required but not available. Please check database configuration.")
    except RuntimeError:
        raise
    except Exception as e:
        logger.error(f"❌ Database check failed: {e}")
        raise RuntimeError(f"Database connection required but failed: {e}")

    # Scan for orphan jobs and mark them as failed
    try:
        orphan_count = dg_util.scan_and_recover_orphan_jobs()
        if orphan_count > 0:
            logger.info(f"Found {orphan_count} orphan job(s) from previous app server run")
    except Exception as e:
        logger.error(f"Error during orphan job recovery: {e}", exc_info=True)

    yield

    # Shutdown
    logger.info("Application shutting down...")
    
    # Close database connections
    try:
        close_db_connections()
        logger.info("Database connections closed")
    except Exception as e:
        logger.error(f"Error closing database connections: {e}", exc_info=True)
    
    stderr_monitor.stop()


# OpenAPI tags metadata for endpoint organization
tags_metadata = [
    {
        "name": "health",
        "description": "Health check and service status endpoints"
    },
    {
        "name": "jobs",
        "description": "Job tracking and management for document processing(Ingestion | Digitization) operations"
    },
    {
        "name": "documents",
        "description": "Document management operations including retrieval and deletion"
    }
]

app = FastAPI(
    title="Digitize Documents Service",
    description="Document digitization and ingestion API for processing PDF and DOCX files into searchable content. "
                "Supports both digitization (converting documents to text/markdown/JSON) and ingestion "
                "(processing and indexing documents into a vector database for semantic search).",
    version="1.0.0",
    lifespan=lifespan,
    openapi_tags=tags_metadata
)

# Use the shared exception handler from common.error_utils
@app.exception_handler(HTTPException)
async def custom_http_exception_handler(request: Request, exc: HTTPException):
    """Use shared exception handler from common.error_utils"""
    return await http_exception_handler(request, exc)

@app.middleware("http")
async def add_request_id(request: Request, call_next):
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response

@app.get("/", include_in_schema=False)
def swagger_root():
    """Expose Swagger UI at the root path (/)"""
    return get_swagger_ui_html(
        openapi_url="/openapi.json",
        title="Digitize Documents Service - Swagger UI",
    )

@app.get(
    "/health",
    status_code=status.HTTP_200_OK,
    tags=["health"],
    summary="Health check",
    description="Check if the service is running and healthy. Used for liveness probes.",
    response_description="Service health status"
)
async def health_check():
    """
    Health check endpoint for liveness probe.

    Returns:
    - 200 OK if the service is healthy
    """
    return {"status": "ok"}

async def digitize_documents(job_id: str, doc_id_dict: dict, output_format: models.OutputFormat):
    status_mgr = get_status_manager(job_id)
    job_staging_path = settings.digitize.staging_dir / f"{job_id}"

    try:
        logger.info(f"🚀 Digitization started for job: {job_id}")
        # to_thread prevents the heavy 'digitize' process from blocking the main FastAPI event loop and returns the response to request asynchronously.
        await asyncio.to_thread(digitize, job_staging_path, job_id, doc_id_dict, output_format)
        logger.info(f"Digitization for job {job_id} completed successfully")
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
        status_mgr.update_job_progress("", models.DocStatus.FAILED, models.JobStatus.FAILED, error=f"Error occurred while processing digitization pipeline: {str(e)}")
    finally:
        # Always clean up staging directory, even on crashes
        cleanup_staging_directory(job_id, settings.digitize.staging_dir)

        # Crucial: Always release the semaphore slot back to the API
        digitization_semaphore.release()
        logger.debug(f"Semaphore slot released from digitization job {job_id}")

async def ingest_documents(job_id: str, filenames: List[str], doc_id_dict: dict):
    status_mgr = get_status_manager(job_id)
    job_staging_path = settings.digitize.staging_dir / f"{job_id}"

    try:
        logger.info(f"🚀 Ingestion started for job: {job_id}")
        # to_thread prevents the heavy 'ingest' process from blocking the main FastAPI event loop and returns the response to request asynchronously.
        await asyncio.to_thread(ingest, job_staging_path, job_id, doc_id_dict)
        logger.info(f"Ingestion for {job_id} completed successfully")
    except Exception as e:
        logger.error(f"Error in job {job_id}: {e}")
        status_mgr.update_job_progress("", models.DocStatus.FAILED, models.JobStatus.FAILED, error=f"Error occurred while processing ingestion pipeline: {str(e)}")
    finally:
        # Always clean up staging directory, even on crashes
        cleanup_staging_directory(job_id, settings.digitize.staging_dir)

        # Mandatory Semaphore Release
        ingestion_semaphore.release()
        logger.debug(f"✅ Job {job_id} done. Semaphore released.")

async def validate_pdf_files(
    files: List[UploadFile],
    file_contents_raw: List[bytes | BaseException]
) -> tuple[List[str], List[bytes]]:
    """
    Validate uploaded document files (PDF or DOCX) using shared validation logic.

    Raises APIError on any validation failure.

    Returns:
        Tuple of (filenames, file_contents)
    """

    filenames: List[str] = []
    file_contents: List[bytes] = []

    for idx, file in enumerate(files):
        filename = file.filename or ""
        content = file_contents_raw[idx]

        # Use shared validation function
        try:
            await asyncio.to_thread(validate_document_file, filename, content)
        except ValueError as e:
            APIError.raise_error(ErrorCode.UNSUPPORTED_MEDIA_TYPE, str(e))

        assert isinstance(content, bytes)  # Type narrowing after validation
        filenames.append(filename)
        file_contents.append(content)

    return filenames, file_contents

@app.post(
    "/v1/jobs",
    status_code=status.HTTP_202_ACCEPTED,
    response_model=models.JobCreatedResponse,
    responses=http_error_responses,
    tags=["jobs"],
    summary="Create async jobs to upload and process documents",
    description=(
        "Upload documents (PDF or DOCX) for processing. Supports two operation types:\n\n"
        "- **ingestion**: Process and index documents into vector database for semantic search\n"
        "- **digitization**: Convert document to text/markdown/JSON format (single file only)\n\n"
        "The operation runs asynchronously in the background. Use the returned `job_id` to track progress."
    ),
    response_description="Job ID for tracking the processing status"
)
async def digitize_document(
    background_tasks: BackgroundTasks,
    files: List[UploadFile] = File(..., description="Document files (PDF or DOCX) to process (multiple for ingestion, single for digitization)"),
    operation: models.OperationType = Query(
        models.OperationType.INGESTION,
        description="Operation type: 'ingestion' (index into vector DB) or 'digitization' (convert to text/md/json)"
    ),
    output_format: models.OutputFormat = Query(
        models.OutputFormat.JSON,
        description="Output format for digitization: 'json', 'md', or 'txt' (only applies to digitization operation)"
    ),
    job_name: Optional[str] = Query(None, description="Optional human-readable name for the job")
):
    try:
        # 1. Check if import/export is in progress
        if await db_ops.is_import_export_in_progress():
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "Cannot create new jobs while import/export operation is in progress"
            )

        # 2. Early exit if no files submitted
        if not files or len(files) == 0:
            APIError.raise_error(ErrorCode.INVALID_REQUEST, "No files provided. Please submit at least one file.")

        if operation == models.OperationType.DIGITIZATION and len(files) > 1:
            APIError.raise_error(ErrorCode.INVALID_REQUEST, "Only 1 file allowed for digitization.")

        # 3. Check for active ingestion jobs BEFORE semaphore check (cross-process coordination)
        if operation == models.OperationType.INGESTION:
            has_active, active_job_ids = dg_util.has_active_jobs(operation=operation.value)
            if has_active:
                error_msg = "An ingestion job is already running"
                if active_job_ids:
                    error_msg += f" (job_id: {active_job_ids[0]})"
                logger.error(f"Rejected ingestion request: {error_msg}")
                APIError.raise_error(ErrorCode.RATE_LIMIT_EXCEEDED, error_msg)

        # 3. Check semaphore availability (for digitization or as backup for ingestion)
        sem = ingestion_semaphore if operation == models.OperationType.INGESTION else digitization_semaphore
        if sem.locked():
            APIError.raise_error(ErrorCode.RATE_LIMIT_EXCEEDED, f"Too many concurrent {operation} requests.")

        # 3. Generate job ID and validate PDF files
        job_id = dg_util.generate_uuid()
        file_contents_raw = await asyncio.gather(*[f.read() for f in files], return_exceptions=True)
        filenames, file_contents = await validate_pdf_files(files, file_contents_raw)

        # 4. Acquire the semaphore
        await sem.acquire()

        # 5. Schedule the background pipeline
        try:
            # Upload the file byte stream to files in staging directory
            # files are written to disk here before creating background task to avoid OOM crashes in the thread. Useful for retrying the ingestion if background task crashes
            await dg_util.stage_upload_files(job_id, filenames, str(settings.digitize.staging_dir / job_id), file_contents)
            doc_id_dict = dg_util.initialize_job_state(job_id, operation, output_format, filenames, job_name)
            if operation == models.OperationType.INGESTION:
                background_tasks.add_task(ingest_documents, job_id, filenames, doc_id_dict)
            else:
                background_tasks.add_task(digitize_documents, job_id, doc_id_dict, output_format)
        except Exception as e:
            sem.release()
            logger.error(f"Failed to schedule background task for job {job_id}, semaphore released: {e}")
            APIError.raise_error("INTERNAL_SERVER_ERROR", str(e))

        return {"job_id": job_id}
    except HTTPException:
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in digitize_document: {e}")
        APIError.raise_error("INTERNAL_SERVER_ERROR", str(e))

# ============================================================================
# Import/Export Configuration
# ============================================================================

# Import/Export configuration
MAX_IMPORT_RECORDS = -1  # -1 means no limit, set to positive integer to enforce limit


# ============================================================================
# Import/Export API Endpoints
# ============================================================================

@app.post(
    "/v1/import",
    response_model=models.ImportResponse,
    responses={400: http_error_responses[400], 409: http_error_responses[409], 413: http_error_responses[413], 500: http_error_responses[500]},
    tags=["jobs"],
    summary="Import metadata into PostgreSQL",
    description="Import job and document metadata into PostgreSQL using the export-compatible JSON payload.",
    response_description="Import summary with imported, skipped, failed records and warnings"
)
async def import_metadata(payload: models.ImportRequest):
    """Import job and document metadata into PostgreSQL."""

    try:
        # Try to acquire distributed lock (works across all worker processes)
        if not await db_ops.acquire_import_export_lock():
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "Another import/export operation is already in progress. Please wait for it to complete.",
            )

        try:
            total_records = len(payload.data.jobs) + len(payload.data.documents)
            # MAX_IMPORT_RECORDS = -1 means no limit (allow importing all records)
            if MAX_IMPORT_RECORDS != -1 and total_records > MAX_IMPORT_RECORDS:
                APIError.raise_error(
                    ErrorCode.CONTEXT_LIMIT_EXCEEDED,
                    f"Request contains {total_records} records, maximum allowed is {MAX_IMPORT_RECORDS}",
                )

            has_active, active_job_ids = dg_util.has_active_jobs()
            if has_active:
                APIError.raise_error(
                    ErrorCode.RESOURCE_LOCKED,
                    f"Cannot import while jobs are active. Active jobs: {', '.join(active_job_ids)}",
                )

            return db_ops.import_metadata(payload)
        finally:
            # Release distributed lock
            await db_ops.release_import_export_lock()
    except HTTPException:
        raise
    except ValueError as exc:
        logger.error(f"Invalid import request: {exc}")
        APIError.raise_error(ErrorCode.INVALID_REQUEST, str(exc))
    except Exception as exc:
        logger.error(f"Failed to import metadata: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Database connection failed during import")


@app.get(
    "/v1/export",
    response_model=models.ExportResponse,
    responses={400: http_error_responses[400], 413: http_error_responses[413], 500: http_error_responses[500]},
    tags=["jobs"],
    summary="Export metadata from PostgreSQL",
    description="Export job and document metadata from PostgreSQL as JSON for backup and restore workflows.",
    response_description="Exported metadata with summary and pagination details"
)
async def export_metadata(
    limit: int = Query(db_ops.IMPORT_EXPORT_DEFAULT_LIMIT, description="Maximum combined records to export. Use -1 to export all records in one response."),
    offset: int = Query(0, ge=0, description="Number of combined records to skip for pagination"),
):
    """Export job and document metadata from PostgreSQL."""
    import os
    pid = os.getpid()
    logger.warning(f"[PID {pid}] 🔵 Export request received (limit={limit}, offset={offset})")

    try:
        # Try to acquire distributed lock (works across all worker processes)
        logger.info(f"[PID {pid}] Trying to acquire lock for export...")
        if not await db_ops.acquire_import_export_lock():
            logger.warning(f"[PID {pid}] ❌ Export REJECTED - lock held by another process")
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                "Another import/export operation is already in progress. Please wait for it to complete.",
            )

        logger.warning(f"[PID {pid}] ✅ Export proceeding with lock acquired")
        try:
            if limit < -1 or limit == 0:
                APIError.raise_error(ErrorCode.INVALID_REQUEST, "limit must be -1 or a positive integer")

            # Check for active jobs before allowing export
            has_active, active_job_ids = dg_util.has_active_jobs()
            if has_active:
                APIError.raise_error(
                    ErrorCode.RESOURCE_LOCKED,
                    f"Cannot export while jobs are active. Active jobs: {', '.join(active_job_ids)}",
                )

            logger.info(f"[PID {pid}] Starting export operation...")
            result = db_ops.export_metadata(limit=limit, offset=offset)
            logger.warning(f"[PID {pid}] ✅ Export completed successfully")
            return result
        finally:
            # Release distributed lock
            await db_ops.release_import_export_lock()
    except HTTPException:
        raise
    except ValueError as exc:
        logger.error(f"Invalid export request: {exc}")
        APIError.raise_error(ErrorCode.INVALID_REQUEST, str(exc))
    except Exception as exc:
        logger.error(f"Failed to export metadata: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Database query failed during export")


@app.get(
    "/v1/jobs",
    response_model=models.JobsListResponse,
    responses={500: http_error_responses[500]},
    tags=["jobs"],
    summary="List all jobs",
    description="Retrieve information about all submitted jobs with pagination and filtering options.",
    response_description="Paginated list of jobs with their current status"
)
async def get_all_jobs(
    latest: bool = Query(False, description="Return only the latest job"),
    limit: int = Query(20, ge=1, le=100, description="Number of records per page"),
    offset: int = Query(0, ge=0, description="Number of records to skip"),
    status: Optional[models.JobStatus] = Query(None, description="Filter by job status"),
    operation: Optional[models.OperationType] = Query(None, description="Filter by operation type")
):
    """Retrieve information about all submitted jobs with pagination and filtering."""
    try:
        # Use database function
        from digitize.db_operations import get_all_jobs

        # Get jobs from database
        jobs_data, total = get_all_jobs(
            status=status,
            operation=operation.value if operation else None,
            limit=limit if not latest else 1,
            offset=offset if not latest else 0
        )

        # Handle latest flag (already handled in query if latest=True)
        if latest and jobs_data:
            jobs_data = [jobs_data[0]]
            total = 1

        return models.JobsListResponse(
            pagination=models.PaginationInfo(total=total, limit=limit, offset=offset),
            data=jobs_data
        )
    except HTTPException as e:
        logger.error(f"Server error in get_all_jobs: {e.status_code} - {e.detail}")
        raise
    except Exception as e:
        logger.error(f"Failed to retrieve jobs: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Failed to retrieve jobs")


@app.get(
    "/v1/jobs/{job_id}",
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    tags=["jobs"],
    summary="Get job by ID",
    description="Retrieve detailed status and progress information for a specific job.",
    response_description="Detailed job information including document statuses and statistics"
)
async def get_job_by_id(job_id: str):
    """Retrieve detailed status of a specific job by its ID."""
    try:
        # Use database function
        from digitize.db_operations import get_job

        job_data = get_job(job_id)
        
        if job_data is None:
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"No job found with id '{job_id}'")
            return  # This line should never be reached, but helps type checker

        return job_data
    except HTTPException as e:
        logger.error(f"HTTP error retrieving job {job_id}: "
        f"status={e.status_code}, detail={e.detail}")
        raise
    except Exception as e:
        logger.error(f"Failed to retrieve job {job_id}: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Failed to retrieve job information for '{job_id}'")

@app.delete(
    "/v1/jobs/{job_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={404: http_error_responses[404], 409: http_error_responses[409], 500: http_error_responses[500]},
    tags=["jobs"],
    summary="Delete job",
    description="Delete a job status record. Only completed or failed jobs can be deleted. "
                "Active jobs (accepted or in_progress) cannot be deleted. "
                "Note: This only deletes the job record, not the associated document data.",
    response_description="No content on successful deletion"
)
async def delete_job(job_id: str):
    """Deletes a job record from database. Does not touch associated document metadata."""
    try:
        # Use database function to get job
        from digitize.db_operations import get_job
        from digitize.db.manager import db_manager

        job_data = get_job(job_id)
        
        if job_data is None:
            APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, f"No job found with id '{job_id}'")

        # Reject deletion if the job is still active
        job_status = job_data.get("status", "")
        if job_status in (models.JobStatus.ACCEPTED, models.JobStatus.IN_PROGRESS):
            APIError.raise_error(ErrorCode.RESOURCE_LOCKED, f"Job '{job_id}' is still active and cannot be deleted")

        # Delete the job from database (CASCADE will delete associated documents)
        db_manager.delete_job(job_id)
        logger.info(f"Deleted job '{job_id}' from database")
        
        return
    except HTTPException as e:
        logger.error(f"HTTP error deleting job {job_id}: "
                     f"status={e.status_code}, detail={e.detail}")
        raise
    except Exception as e:
        logger.error(f"Failed to delete job {job_id}: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Failed to delete job '{job_id}'")



@app.get(
    "/v1/documents",
    response_model=models.DocumentsListResponse,
    responses={400: http_error_responses[400], 500: http_error_responses[500]},
    tags=["documents"],
    summary="List all documents",
    description="Get high-level information of all documents with pagination and filtering. "
                "Documents are sorted by submission time (newest first).",
    response_description="Paginated list of documents with basic metadata"
)
async def list_documents(
    limit: int = Query(20, ge=1, le=100, description="Number of records to return per page"),
    offset: int = Query(0, ge=0, description="Number of records to skip"),
    status: Optional[str] = Query(None, description="Filter by status: accepted/in_progress/completed/failed"),
    name: Optional[str] = Query(None, description="Filter by document name (partial match, case-insensitive)")
):
    """
    Get high-level information of all documents sorted by submitted_time.

    Query Parameters:
    - limit: Number of records to return per page (default: 20, max: 100)
    - offset: Number of records to skip (default: 0)
    - status: Filter by status (accepted/in_progress/completed/failed)
    - name: Filter by document name (partial match, case-insensitive)

    Returns:
    - pagination: Object with total, limit, and offset
    - data: List of document metadata objects
    """
    try:
        logger.debug(f"Fetching documents with filters: limit={limit}, offset={offset}, status={status}, name={name}")
        # Validate status if provided
        valid_statuses = {s.value for s in models.DocStatus}
        if status and status.lower() not in valid_statuses:
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Invalid status '{status}'. Must be one of: {', '.join(sorted(valid_statuses))}"
            )

        # Use database function
        from digitize.db_operations import get_all_documents_paginated

        documents_data, total = get_all_documents_paginated(
            status=status,
            name=name,
            limit=limit,
            offset=offset
        )

        logger.debug(f"Returning {len(documents_data)} documents out of {total} total (offset={offset}, limit={limit})")

        # Convert to DocumentListItem if needed
        from digitize.models import DocumentListItem
        doc_items = [DocumentListItem(**doc) for doc in documents_data]

        # Return properly typed response
        return models.DocumentsListResponse(
            pagination=models.PaginationInfo(total=total, limit=limit, offset=offset),
            data=doc_items
        )

    except HTTPException as e:
        logger.error(f"Failed to list documents, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in list_documents: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))

@app.get(
    "/v1/documents/{doc_id}",
    response_model=models.DocumentDetailResponse,
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    tags=["documents"],
    summary="Get document metadata",
    description="Retrieve detailed metadata for a specific document by its ID. "
                "Optionally include processing details like page count, table count, and timing information.",
    response_description="Document metadata with optional detailed processing information"
)
async def get_document_metadata(doc_id: str, details: bool = Query(False, description="Include detailed metadata (pages, tables, timing)")):
    """
    Get details of a specific document by ID.

    Path Parameters:
    - doc_id: Unique identifier of the document

    Query Parameters:
    - details: If true, includes detailed metadata (pages, tables, timing information)

    Returns:
    - Document metadata with optional detailed information
    """
    try:
        # Use database function - now returns DocumentDetailResponse directly
        from digitize.db_operations import get_document

        response = get_document(doc_id, include_details=details)
        return response
    except FileNotFoundError as e:
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, str(e))
    except HTTPException as e:
        logger.error(f"Failed to get document by id {doc_id}, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in get_document_metadata: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))

@app.get(
    "/v1/documents/{doc_id}/content",
    response_model=models.DocumentContentResponse,
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    tags=["documents"],
    summary="Get document content",
    description="Retrieve the digitized/processed content of a document. "
                "For digitization operations, returns content in the requested format (text/markdown/JSON). "
                "For ingestion operations, returns the extracted JSON representation.",
    response_description="Document content in the specified output format"
)
async def get_document_content(doc_id: str):
    """
    Get the digitized content of a specific document.

    Returns the digitized content stored in /var/cache/digitized/<doc_id>.json
    - For documents submitted via digitization: returns the output_format requested during POST (md/text/json)
    - For documents submitted via ingestion: returns the extracted json representation

    Path Parameters:
    - doc_id: Unique identifier of the document

    Returns:
    - result: Content based on output_format (str for md/text, dict for json)
    - output_format: The format of the returned content (md/text/json)
    """
    try:
        response = dg_util.get_document_content(doc_id)
        return response
    except FileNotFoundError as e:
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, str(e))
    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse content file for document {doc_id}: {e}")
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Failed to read document content")
    except HTTPException as e:
        logger.error(f"Failed to get document content for id {doc_id}, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error in get_document_content: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))

@app.delete(
    "/v1/documents/{doc_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={404: http_error_responses[404], 409: http_error_responses[409], 500: http_error_responses[500]},
    tags=["documents"],
    summary="Delete document",
    description="Delete a single document by ID. Removes the document from the vector database (if ingested), "
                "deletes all associated files, and removes metadata. "
                "Documents that are part of active jobs cannot be deleted.",
    response_description="No content on successful deletion"
)
async def delete_document(doc_id: str):
    """
    Delete a single document by ID.

    This endpoint implements a robust deletion strategy:
    1. Checks if the document exists
    2. Verifies the document is not part of any active job (in_progress)
    3. Removes the document from the vector database (if ingested) - FIRST
    4. Deletes all associated files from cache - LAST

    Deletes document with an 'Always-Clean-VDB' retry strategy.
    Order: 1. VDB (Search) -> 2. Files (Storage) -> 3. Metadata (Record)

    Path Parameters:
    - doc_id: Unique identifier of the document to delete

    Returns:
    - 204 No Content on successful deletion (HTTP 204)
    - 404 Not Found if document doesn't exist
    - 409 Conflict if document is part of an active job
    - 500 Internal Server Error on unexpected errors
    """

    try:
        # 1. Fetch Metadata (if it exists)
        doc_metadata = None
        try:
            doc_metadata = dg_util.get_document(doc_id, include_details=False)
        except FileNotFoundError:
            logger.error(f"Metadata for {doc_id} not found. Proceeding with vectorstore cleanup.")

        # 2. Lock Check: Only if metadata exists and indicates an active job
        if doc_metadata:
            is_active = dg_util.is_document_in_active_job(doc_id, job_id=doc_metadata.job_id)
            if is_active:
                APIError.raise_error(
                    ErrorCode.RESOURCE_LOCKED,
                    f"Document part of active job '{doc_metadata.job_id}' and cannot be deleted"
                )

        # 3. Step A: Vector Database Cleanup (High Priority)
        # We attempt this regardless of whether metadata exists to fix partial failures.
        try:
            import common.db_utils as db
            vector_store = db.get_vector_store()
            # This method should use the 'refresh=True' param we discussed earlier
            deleted_chunks = vector_store.delete_document_by_id(doc_id)
            logger.info(f"VDB cleanup for {doc_id}: {deleted_chunks} chunks removed.")
        except Exception as e:
            logger.error(f"VDB cleanup failed for {doc_id}: {e}")
            # If metadata is already deleted and VDB fails, we MUST raise to let the user retry
            APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Document metadata deleted but VDB cleanup failed: {e}")

        # 4. Step B: File & Metadata Cleanup
        # If metadata is already deleted, we assume files were either deleted or are being handled
        if doc_metadata:
            try:
                # Delete digitized files AND the metadata file last
                # Pass output_format to delete only the specific format file instead of looping through all
                dg_util.delete_document_files(doc_id, output_format=doc_metadata.output_format)
                logger.info(f"Files and metadata for {doc_id} deleted successfully.")
            except Exception as e:
                # If VDB succeeded but files failed, we report a 500 so the user retries
                # even though the document is now 'hidden' from search.
                logger.error(f"VDB cleaned but file deletion failed for {doc_id}")
                APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Search data removed but files remain: {e}")

        # 5. Step C: Database Cleanup
        # Delete the document record from the database
        try:
            from digitize.db.manager import db_manager
            success = db_manager.delete_document(doc_id)
            if success:
                logger.info(f"Database record for {doc_id} deleted successfully.")
            else:
                logger.warning(f"Database record for {doc_id} not found (may have been deleted already).")
        except Exception as e:
            logger.error(f"Failed to delete database record for {doc_id}: {e}")
            # If VDB and files are cleaned but DB fails, report error so user can retry
            APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, f"Search data and files removed but database cleanup failed: {e}")

        # 6. Idempotent Success
        # If we reach here, either everything is deleted, or metadata was already deleted and VDB is now clean.
        return None

    except HTTPException as e:
        logger.error(f"Failed to delete document {doc_id}, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error deleting document {doc_id}: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))


@app.delete(
    "/v1/documents",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={400: http_error_responses[400], 409: http_error_responses[409], 500: http_error_responses[500]},
    tags=["documents"],
    summary="Bulk delete all documents",
    description="⚠️ **DANGER**: Delete ALL documents from the system. "
                "This performs a complete cleanup including vector database reset and file deletion. "
                "Requires explicit confirmation and will fail if any jobs are active.",
    response_description="No content on successful deletion"
)
async def bulk_delete_documents(confirm: bool = Query(..., description="Must be true to proceed with bulk deletion")):
    """
    Bulk delete all documents from the system.

    This endpoint performs a complete system cleanup:
    1. Checks for active jobs and rejects if any exist
    2. Resets the vector database index (removes all indexed chunks)
    3. Deletes all digitized content files from /var/cache/digitized
    4. Deletes all document metadata files from /var/cache/docs

    Query Parameters:
    - confirm: Must be true to proceed with deletion (required)

    Returns:
    - 204 No Content on successful deletion
    - 400 Bad Request if confirm is not true
    - 409 Conflict if there are active jobs
    - 500 Internal Server Error on unexpected errors
    """
    try:
        # 1. Validate confirmation parameter
        if not confirm:
            logger.error("Bulk delete rejected: confirm parameter is false")
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                "Bulk deletion requires explicit confirmation. Set 'confirm=true' to proceed."
            )

        # 2. Check for active jobs
        has_active, active_job_ids = dg_util.has_active_jobs()
        if has_active:
            logger.error(f"Bulk delete rejected: {len(active_job_ids)} active job(s) found")
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                f"Cannot perform bulk deletion while jobs are active. Active jobs: {', '.join(active_job_ids)}"
            )

        logger.info("No active jobs found, proceeding with bulk deletion")

        # 3. Reset vector database and delete all files
        # Uses reset_db() which handles VDB reset and file deletion with proper error handling
        reset_db()
        logger.info("✅ Bulk deletion completed successfully")
        return None

    except HTTPException as e:
        logger.error(f"Failed to bulk delete documents, HTTP error: {e}")
        # Re-raise HTTPException as-is
        raise
    except Exception as e:
        logger.error(f"Unexpected error during bulk deletion: {e}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(e))

if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=4000)
