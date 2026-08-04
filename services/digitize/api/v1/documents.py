"""
Document-related API endpoints.

Handles document listing, retrieval, content access, and deletion.
Extracted from the monolithic app.py following the digitize-api-sample pattern.
"""

import json
from typing import Optional

from fastapi import APIRouter, HTTPException, Query, status

from common.misc_utils import get_logger
from common.error_utils import APIError, ErrorCode, http_error_responses
import digitize.utils.jobs as dg_util
import digitize.models as models
from digitize.pipeline.cleanup import reset_db
from digitize.utils.storage import storage_manager

router = APIRouter()
logger = get_logger("documents_router")


@router.get(
    "",
    response_model=models.DocumentsListResponse,
    responses={400: http_error_responses[400], 500: http_error_responses[500]},
    summary="List all documents",
    description=(
        "Get high-level information of all documents with pagination and filtering. "
        "Documents are sorted by submission time (newest first)."
    ),
    response_description="Paginated list of documents with basic metadata",
)
async def list_documents(
    limit: int = Query(20, ge=1, le=100, description="Number of records to return per page"),
    offset: int = Query(0, ge=0, description="Number of records to skip"),
    status: Optional[str] = Query(
        None,
        description="Filter by status: accepted/in_progress/completed/failed",
    ),
    name: Optional[str] = Query(
        None,
        description="Filter by document name (partial match, case-insensitive)",
    ),
):
    try:
        logger.debug(
            f"Fetching documents with filters: limit={limit}, offset={offset}, "
            f"status={status}, name={name}"
        )

        valid_statuses = {s.value for s in models.DocStatus}
        if status and status.lower() not in valid_statuses:
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Invalid status '{status}'. "
                f"Must be one of: {', '.join(sorted(valid_statuses))}",
            )

        from digitize.utils.db import get_all_documents_paginated

        documents_data, total = get_all_documents_paginated(
            status=status,
            name=name,
            limit=limit,
            offset=offset,
        )

        logger.debug(
            f"Returning {len(documents_data)} documents out of {total} total "
            f"(offset={offset}, limit={limit})"
        )

        doc_items = [models.DocumentListItem(**doc) for doc in documents_data]

        return models.DocumentsListResponse(
            pagination=models.PaginationInfo(total=total, limit=limit, offset=offset),
            data=doc_items,
        )

    except HTTPException as exc:
        logger.error(f"Failed to list documents, HTTP error: {exc}")
        raise
    except Exception as exc:
        logger.error(f"Unexpected error in list_documents: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


@router.get(
    "/{doc_id}",
    response_model=models.DocumentDetailResponse,
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Get document metadata",
    description=(
        "Retrieve detailed metadata for a specific document by its ID. "
        "Optionally include processing details like page count, table count, and timing information."
    ),
    response_description="Document metadata with optional detailed processing information",
)
async def get_document_metadata(
    doc_id: str,
    details: bool = Query(False, description="Include detailed metadata (pages, tables, timing)"),
):
    try:
        from digitize.utils.db import get_document

        return get_document(doc_id, include_details=details)
    except FileNotFoundError as exc:
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, str(exc))
    except HTTPException as exc:
        logger.error(f"Failed to get document {doc_id}, HTTP error: {exc}")
        raise
    except Exception as exc:
        logger.error(f"Unexpected error in get_document_metadata: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


@router.get(
    "/{doc_id}/content",
    response_model=models.DocumentContentResponse,
    responses={404: http_error_responses[404], 500: http_error_responses[500]},
    summary="Get document content",
    description=(
        "Retrieve the digitized/processed content of a document. "
        "For digitization operations, returns content in the requested format (text/markdown/JSON). "
        "For ingestion operations, returns the extracted JSON representation."
    ),
    response_description="Document content in the specified output format",
)
async def get_document_content(doc_id: str):
    try:
        return dg_util.get_document_content(doc_id)
    except FileNotFoundError as exc:
        APIError.raise_error(ErrorCode.RESOURCE_NOT_FOUND, str(exc))
    except json.JSONDecodeError as exc:
        logger.error(f"Failed to parse content file for document {doc_id}: {exc}")
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Failed to read document content")
    except HTTPException as exc:
        logger.error(f"Failed to get document content for {doc_id}, HTTP error: {exc}")
        raise
    except Exception as exc:
        logger.error(f"Unexpected error in get_document_content: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


@router.delete(
    "/{doc_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        404: http_error_responses[404],
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Delete document",
    description=(
        "Delete a single document by ID. Removes the document from the vector database (if ingested), "
        "deletes all associated files, and removes metadata. "
        "Documents that are part of active jobs cannot be deleted."
    ),
    response_description="No content on successful deletion",
)
async def delete_document(doc_id: str):
    """
    Delete a single document — follows the 'Always-Clean-VDB' strategy:
    1. Fetch metadata
    2. Active-job guard
    3. VDB cleanup (high priority)
    4. File & metadata cleanup
    5. Database record removal
    """
    try:
        # 1. Fetch metadata (best-effort).
        doc_metadata = None
        try:
            doc_metadata = dg_util.get_document(doc_id, include_details=False)
        except FileNotFoundError:
            logger.error(f"Metadata for {doc_id} not found. Proceeding with VDB cleanup.")

        # 2. Active-job guard.
        if doc_metadata:
            if dg_util.is_document_in_active_job(doc_id, job_id=doc_metadata.job_id):
                APIError.raise_error(
                    ErrorCode.RESOURCE_LOCKED,
                    f"Document part of active job '{doc_metadata.job_id}' and cannot be deleted",
                )

        # 3. VDB cleanup.
        try:
            import common.db_utils as db

            vector_store = db.get_vector_store()
            deleted_chunks = vector_store.delete_document_by_id(doc_id)
            logger.info(f"VDB cleanup for {doc_id}: {deleted_chunks} chunks removed.")
        except Exception as exc:
            logger.error(f"VDB cleanup failed for {doc_id}: {exc}")
            APIError.raise_error(
                ErrorCode.INTERNAL_SERVER_ERROR,
                f"Document metadata deleted but VDB cleanup failed: {exc}",
            )

        # 4. File cleanup.
        if doc_metadata:
            try:
                storage_manager.delete_document_content(
                    doc_id, output_format=doc_metadata.output_format
                )
                logger.info(f"Files for {doc_id} deleted successfully.")
            except Exception as exc:
                logger.error(f"VDB cleaned but file deletion failed for {doc_id}")
                APIError.raise_error(
                    ErrorCode.INTERNAL_SERVER_ERROR,
                    f"Search data removed but files remain: {exc}",
                )

        # 5. Database record removal.
        try:
            from digitize.db.manager import db_manager

            success = db_manager.delete_document(doc_id)
            if success:
                logger.info(f"Database record for {doc_id} deleted successfully.")
            else:
                logger.warning(
                    f"Database record for {doc_id} not found (may have been deleted already)."
                )
        except Exception as exc:
            logger.error(f"Failed to delete database record for {doc_id}: {exc}")
            APIError.raise_error(
                ErrorCode.INTERNAL_SERVER_ERROR,
                f"Search data and files removed but database cleanup failed: {exc}",
            )

        return None

    except HTTPException as exc:
        logger.error(f"Failed to delete document {doc_id}, HTTP error: {exc}")
        raise
    except Exception as exc:
        logger.error(f"Unexpected error deleting document {doc_id}: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))


@router.delete(
    "",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: http_error_responses[400],
        409: http_error_responses[409],
        500: http_error_responses[500],
    },
    summary="Bulk delete all documents",
    description=(
        "⚠️ **DANGER**: Delete ALL documents from the system. "
        "This performs a complete cleanup including vector database reset and file deletion. "
        "Requires explicit confirmation and will fail if any jobs are active."
    ),
    response_description="No content on successful deletion",
)
async def bulk_delete_documents(
    confirm: bool = Query(..., description="Must be true to proceed with bulk deletion"),
):
    try:
        if not confirm:
            logger.error("Bulk delete rejected: confirm parameter is false")
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                "Bulk deletion requires explicit confirmation. Set 'confirm=true' to proceed.",
            )

        has_active, active_job_ids = dg_util.has_active_jobs()
        if has_active:
            logger.error(f"Bulk delete rejected: {len(active_job_ids)} active job(s) found")
            APIError.raise_error(
                ErrorCode.RESOURCE_LOCKED,
                f"Cannot perform bulk deletion while jobs are active. "
                f"Active jobs: {', '.join(active_job_ids)}",
            )

        logger.info("No active jobs found, proceeding with bulk deletion")
        reset_db()
        logger.info("✅ Bulk deletion completed successfully")
        return None

    except HTTPException as exc:
        logger.error(f"Failed to bulk delete documents, HTTP error: {exc}")
        raise
    except Exception as exc:
        logger.error(f"Unexpected error during bulk deletion: {exc}", exc_info=True)
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, str(exc))
