import logging
import os
import uuid
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI, HTTPException, Request
from fastapi.openapi.docs import get_swagger_ui_html

from common.misc_utils import set_log_level

# Set log level before importing application modules so their loggers
# inherit the level.
log_level = logging.INFO
level = os.getenv("LOG_LEVEL", "").removeprefix("--").lower()
if level != "":
    if "debug" in level:
        log_level = logging.DEBUG
    elif "info" not in level:
        logging.warning(
            f"Unknown LOG_LEVEL passed: '{level}', using default INFO level"
        )
set_log_level(log_level)

from common.error_utils import (
    APIError,
    ErrorCode,
    http_error_responses,
    http_exception_handler,
)
from common.misc_utils import set_request_id
from embedding.embedding_utils import (
    EmbeddingInput,
    EmbeddingObject,
    EmbeddingResponse,
    generate_embeddings,
)
from embedding.settings import settings

# Resolved at startup from the EMBEDDING_MODEL_NAME env var.
_model_name: str = ""
# Resolved at startup from the EMB_ENDPOINT env var (common settings).
_endpoint: str = ""


@asynccontextmanager
async def lifespan(app: FastAPI):
    """Resolve model name and endpoint at startup."""
    global _model_name, _endpoint
    _model_name = settings.embedding_service.model_name
    _endpoint = settings.common.embedding.endpoint
    logging.info(
        "Embedding service started — model=%s endpoint=%s",
        _model_name,
        _endpoint,
    )
    yield


tags_metadata = [
    {
        "name": "embeddings",
        "description": "Vector embedding generation operations",
    },
    {
        "name": "monitoring",
        "description": "Health checks and service status",
    },
]

app = FastAPI(
    lifespan=lifespan,
    title="AI-Services Embedding API",
    description=(
        "Generates float32 vectors from text or image inputs using a "
        "vLLM-served embedding model. Compatible with the OpenAI "
        "embeddings API format."
    ),
    version="1.0.0",
    openapi_tags=tags_metadata,
)

app.add_exception_handler(HTTPException, http_exception_handler)


@app.middleware("http")
async def add_request_id(request: Request, call_next):
    """Attach a unique request ID to every request for tracing."""
    request_id = request.headers.get("X-Request-ID", str(uuid.uuid4()))
    set_request_id(request_id)
    response = await call_next(request)
    response.headers["X-Request-ID"] = request_id
    return response


@app.get("/", include_in_schema=False)
def swagger_root():
    """Expose Swagger UI at the root path."""
    return get_swagger_ui_html(
        openapi_url="/openapi.json",
        title="AI-Services Embedding API - Swagger UI",
    )


@app.post(
    "/v1/embeddings",
    response_model=EmbeddingResponse,
    responses={
        400: http_error_responses[400],
        500: http_error_responses[500],
    },
    tags=["embeddings"],
    summary="Generate embeddings",
    description=(
        "Converts text or image inputs into float32 vectors using the "
        "configured vLLM embedding model.\n\n"
        "Accepts a single string or a list of strings in the `input` "
        "field. Returns one embedding object per input item, compatible "
        "with the OpenAI embeddings response format.\n\n"
        "The `model` field must match the model name loaded by the "
        "service (`EMBEDDING_MODEL_NAME` env var)."
    ),
    response_description=(
        "List of embedding objects, one per input item, in input order."
    ),
)
async def create_embeddings(req: EmbeddingInput) -> EmbeddingResponse:
    """Generate embeddings for the provided input(s).

    Validates the request, delegates to the vLLM endpoint, and returns
    vectors in OpenAI-compatible format.
    """
    inputs = [req.input] if isinstance(req.input, str) else req.input
    if not inputs or not any(s.strip() for s in inputs):
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "input is required")

    if req.model != _model_name:
        APIError.raise_error(
            ErrorCode.INVALID_PARAMETER,
            f"model '{req.model}' is not loaded; "
            f"loaded model is '{_model_name}'",
        )

    try:
        vectors = generate_embeddings(inputs, req.model, _endpoint)
    except Exception as e:
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, repr(e))

    return EmbeddingResponse(
        model=req.model,
        data=[
            EmbeddingObject(index=i, embedding=vec)
            for i, vec in enumerate(vectors)
        ],
    )


@app.get(
    "/health",
    tags=["monitoring"],
    summary="Health check",
    description="Returns 200 when the service is running.",
)
async def health():
    """Basic liveness check."""
    return {"status": "ok"}


if __name__ == "__main__":
    port = int(os.getenv("PORT", "7000"))
    uvicorn.run(app, host="0.0.0.0", port=port)
