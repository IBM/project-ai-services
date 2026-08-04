"""Schema-related API endpoints.

Handles schema creation, listing, retrieval, and deletion.

Exposes one router:
- ``router`` → mounted at ``/v1/schema``
"""

import asyncio
import uuid
from typing import  Optional

from fastapi import APIRouter, Query
from fastapi.responses import  Response
from sqlalchemy.exc import IntegrityError
from common.misc_utils import get_logger, get_llm_endpoint
from common.error_utils import  http_error_responses

from extract.settings import settings

from extract.db.manager import db_repo
from extract.models import (
    PaginationInfo,
    SchemaCreatedResponse,
    SchemaDetailResponse,
    SchemaListItem,
    SchemaListResponse,
    SchemaRegisterRequest,
)
from extract.utils.schema import (
    SchemaValidationError,
    check_schema_share_in_context,
    compute_token_counts,
    infer_schema_from_examples,
    normalize_schema,
    validate_examples,
    validate_json_schema_structure,
    fmt_dt,
)


router = APIRouter(redirect_slashes=False)
logger = get_logger("schema_router")

# ---------------------------------------------------------------------------
# POST /v1/schemas — Register a new immutable extraction schema
# ---------------------------------------------------------------------------

@router.post(
    "",
    status_code=201,
    response_model=SchemaCreatedResponse,
    responses={
        400: http_error_responses[400],
        409: {"description": "Schema name already exists"},
        500: http_error_responses[500],
    },
    summary="Register extraction schema",
    description=(
        "Register a new immutable extraction schema.\n\n"
        "**Validation performed:**\n"
        "1. `json_schema` is valid draft 2020-12 with root `type: object`\n"
        "2. Per-property `\"required\": true` tags are normalized into a standard "
        "`required` array (nested sub-schemas are handled recursively)\n"
        "3. Every `examples[i].output` validates against the normalized schema\n"
        "4. Token-count budget check: fixed overhead ≤ CONTEXT_SCHEMA_SHARE × MAX_MODEL_LEN\n\n"
        "The stored schema is always the **normalized** form."
    ),
    tags=["schemas"],
)
async def register_schema(body: SchemaRegisterRequest) -> SchemaCreatedResponse:
    # --- Conflict check (name uniqueness) ---
    if db_repo.schema_name_exists(body.name):
        raise SchemaValidationError(
            "CONFLICT",
            f"A schema with name {body.name!r} already exists.",
            status=409,
        )

    examples_raw = [ex.model_dump() for ex in body.examples] if body.examples else None

    if body.json_schema is None and not examples_raw:
        raise SchemaValidationError(
            "MISSING_SCHEMA",
            "Either json_schema or at least one example must be provided.",
            status=400,
        )

    if body.json_schema is None:
        # --- Infer schema from examples when no explicit schema is provided ---
        normalized = infer_schema_from_examples(examples_raw or [])
        is_inferred = True
    else:
        # --- Normalize per-property "required": true convention FIRST ---
        normalized = normalize_schema(body.json_schema)
        is_inferred = False

    # --- JSON Schema structural validation (against the normalized form) ---
    validate_json_schema_structure(normalized)

    # --- Validate example outputs against normalized schema ---
    validate_examples(examples_raw, normalized)


    # --- Token-count caching ---
    llm_model_dict = get_llm_endpoint()
    llm_endpoint = llm_model_dict.get("llm_endpoint", "")
    try:
        schema_tokens, examples_tokens, custom_prompt_tokens = await asyncio.to_thread(
            compute_token_counts,
            normalized,
            examples_raw,
            body.custom_prompt,
            llm_endpoint,
        )
    except Exception as exc:
        logger.error(f"Token counting failed: {exc}", exc_info=True)
        raise SchemaValidationError(
            "TOKENIZATION_ERROR",
            "Failed to compute token counts for the schema. "
            "Ensure the LLM tokenize endpoint is reachable.",
            status=500,
        )

    # --- Registration budget check ---
    max_model_len = settings.common.llm.max_model_len
    check_schema_share_in_context(schema_tokens, examples_tokens, custom_prompt_tokens, max_model_len)

    # ---  Persist ---
    schema_id = str(uuid.uuid4())
    row = db_repo.create_schema(
        schema_id=schema_id,
        name=body.name,
        json_schema=normalized,
        schema_tokens=schema_tokens,
        examples_tokens=examples_tokens,
        custom_prompt_tokens=custom_prompt_tokens,
        description=body.description,
        examples=examples_raw,
        custom_prompt=body.custom_prompt,
        is_schema_inferred=is_inferred,
    )
    if row is None:
        raise SchemaValidationError(
            "DATABASE_ERROR",
            "Failed to persist the schema. Please try again.",
            status=500,
        )

    logger.info(f"Registered schema {schema_id!r} ({body.name!r})")
    return SchemaCreatedResponse(
        schema_id=row.schema_id,
        name=row.name,
        description=row.description,
        created_at=fmt_dt(row.created_at),
    )


# ---------------------------------------------------------------------------
# GET /v1/schemas — List schemas (paginated, name filter, metadata only)
# ---------------------------------------------------------------------------

@router.get(
    "",
    response_model=SchemaListResponse,
    responses={
        400: http_error_responses[400],
        500: http_error_responses[500],
    },
    summary="List extraction schemas",
    description=(
        "Return a paginated list of registered schemas.  "
        "Schema bodies are **excluded** from this endpoint; use "
        "`GET /v1/schemas/{schema_id}` to retrieve the full definition."
    ),
    tags=["schemas"],
)
async def list_schemas(
    limit: int = Query(default=20, ge=1, le=100, description="Records per page"),
    offset: int = Query(default=0, ge=0, description="Records to skip"),
    name: Optional[str] = Query(default=None, description="Case-insensitive name substring filter"),
) -> SchemaListResponse:
    rows, total = db_repo.list_schemas(name_filter=name, limit=limit, offset=offset)
    data = [
        SchemaListItem(
            schema_id=row.schema_id,
            name=row.name,
            description=row.description,
            example_count=len(row.examples) if row.examples else 0,
            schema_tokens=row.schema_tokens,
            examples_tokens=row.examples_tokens,
            custom_prompt_tokens=row.custom_prompt_tokens,
            created_at=fmt_dt(row.created_at),
        )
        for row in rows
    ]
    return SchemaListResponse(
        pagination=PaginationInfo(total=total, limit=limit, offset=offset),
        data=data,
    )


# ---------------------------------------------------------------------------
# GET /v1/schemas/{schema_id} — Retrieve full schema definition
# ---------------------------------------------------------------------------

@router.get(
    "/{schema_id}",
    response_model=SchemaDetailResponse,
    responses={
        404: http_error_responses[404],
        500: http_error_responses[500],
    },
    summary="Get schema by ID",
    description=(
        "Retrieve the full schema record, including the **normalized** "
        "`json_schema`, `examples`, and `custom_prompt`."
    ),
    tags=["schemas"],
)
async def get_schema(schema_id: str) -> SchemaDetailResponse:
    row = db_repo.get_schema_by_id(schema_id)
    if row is None:
        raise SchemaValidationError(
            "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.",
            status=404,
        )
    return SchemaDetailResponse(
        schema_id=row.schema_id,
        name=row.name,
        description=row.description,
        json_schema=row.json_schema,
        examples=row.examples,
        custom_prompt=row.custom_prompt,
        schema_tokens=row.schema_tokens,
        examples_tokens=row.examples_tokens,
        custom_prompt_tokens=row.custom_prompt_tokens,
        created_at=fmt_dt(row.created_at),
    )


# ---------------------------------------------------------------------------
# DELETE /v1/schemas/{schema_id} — Delete a single schema (RESTRICT)
# ---------------------------------------------------------------------------

@router.delete(
    "/{schema_id}",
    status_code=204,
    responses={
        204: {"description": "Schema deleted"},
        404: http_error_responses[404],
        409: {"description": "Schema is referenced by one or more jobs"},
        500: http_error_responses[500],
    },
    summary="Delete schema",
    description=(
        "Delete a schema.  Rejected if **any** extract job (active or "
        "historical) references this schema.  Delete referencing jobs first."
    ),
    tags=["schemas"],
)
async def delete_schema(schema_id: str) -> Response:
    # Check existence first for a clear 404.
    row = db_repo.get_schema_by_id(schema_id)
    if row is None:
        raise SchemaValidationError(
            "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.",
            status=404,
        )

    # Check for referencing jobs before attempting delete (avoids ambiguous DB errors).
    referencing = db_repo.get_referencing_job_ids(schema_id, limit=10)
    if referencing:
        raise SchemaValidationError(
            "SCHEMA_IN_USE",
            f"Schema {schema_id!r} is referenced by {len(referencing)} job(s). "
            "Delete the referencing jobs first.",
            status=409,
            details={"referencing_job_ids": referencing},
        )

    try:
        deleted = db_repo.delete_schema(schema_id)
    except IntegrityError:
        # FK RESTRICT fired — another job was created concurrently.
        referencing = db_repo.get_referencing_job_ids(schema_id, limit=10)
        raise SchemaValidationError(
            "SCHEMA_IN_USE",
            f"Schema {schema_id!r} is referenced by job(s) and cannot be deleted.",
            status=409,
            details={"referencing_job_ids": referencing},
        )

    if not deleted:
        raise SchemaValidationError(
            "SCHEMA_NOT_FOUND",
            f"No schema with id {schema_id!r}.",
            status=404,
        )

    logger.info(f"Deleted schema {schema_id!r}")
    return Response(status_code=204)


# ---------------------------------------------------------------------------
# DELETE /v1/schemas — Bulk delete (confirm=true required)
# ---------------------------------------------------------------------------

@router.delete(
    "",
    status_code=204,
    responses={
        204: {"description": "All schemas deleted"},
        400: http_error_responses[400],
        409: {"description": "One or more schemas are referenced by jobs"},
        500: http_error_responses[500],
    },
    summary="Bulk delete all schemas",
    description=(
        "Delete **all** registered schemas.  Requires `?confirm=true`.\n\n"
        "Rejected (409) if any extract job exists, because jobs reference "
        "schemas via a FK.  Delete all jobs first."
    ),
    tags=["schemas"],
)
async def bulk_delete_schemas(
    confirm: Optional[str] = Query(
        default=None,
        description="Must be 'true' to confirm destructive bulk deletion",
    ),
) -> Response:
    if confirm != "true":
        raise SchemaValidationError(
            "CONFIRMATION_REQUIRED",
            "Bulk delete requires ?confirm=true.",
            status=400,
        )

    if db_repo.any_schema_has_jobs():
        raise SchemaValidationError(
            "SCHEMAS_IN_USE",
            "One or more schemas are referenced by extract jobs. "
            "Delete all jobs (DELETE /v1/extract/jobs?confirm=true) before bulk-deleting schemas.",
            status=409,
        )

    try:
        db_repo.delete_all_schemas()
    except IntegrityError:
        raise SchemaValidationError(
            "SCHEMAS_IN_USE",
            "One or more schemas are referenced by extract jobs.",
            status=409,
        )

    logger.info("Bulk deleted all schemas")
    return Response(status_code=204)
