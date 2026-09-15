"""
Request/response models and inference logic for the embedding service.

The service calls a vLLM-compatible POST /v1/embeddings endpoint and
returns float32 vectors in the OpenAI embeddings response format.
"""
import logging
from typing import Union

import requests
from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Request / response models
# ---------------------------------------------------------------------------

class EmbeddingInput(BaseModel):
    """Request body for POST /v1/embeddings."""

    model: str = Field(
        ...,
        description=(
            "Model name to use for embedding generation. "
            "Must match a DMG-approved model loaded by the service "
            "(e.g. openai/clip-vit-base-patch32)."
        ),
    )
    input: Union[str, list[str]] = Field(
        ...,
        description=(
            "Text string or list of text strings to embed. "
            "For image inputs use a URL string prefixed with 'http'."
        ),
    )


class EmbeddingObject(BaseModel):
    """A single embedding result, matching the OpenAI embeddings object."""

    object: str = Field(default="embedding")
    embedding: list[float] = Field(
        ..., description="Float32 vector produced by the model."
    )
    index: int = Field(..., description="Position of this item in the input list.")


class EmbeddingResponse(BaseModel):
    """Response from POST /v1/embeddings, compatible with the OpenAI format."""

    object: str = Field(default="list")
    data: list[EmbeddingObject] = Field(
        ..., description="One embedding object per input item."
    )
    model: str = Field(..., description="Model used to produce the embeddings.")

    model_config = {
        "json_schema_extra": {
            "example": {
                "object": "list",
                "model": "openai/clip-vit-base-patch32",
                "data": [
                    {
                        "object": "embedding",
                        "index": 0,
                        "embedding": [0.021, -0.134, 0.087],
                    }
                ],
            }
        }
    }


# ---------------------------------------------------------------------------
# Inference helper
# ---------------------------------------------------------------------------

def generate_embeddings(
    inputs: list[str],
    model: str,
    endpoint: str,
) -> list[list[float]]:
    """Call the vLLM /v1/embeddings endpoint and return float32 vectors.

    Args:
        inputs:   List of text (or image URL) strings to embed.
        model:    Model name to pass in the request payload.
        endpoint: Base URL of the vLLM server
                  (e.g. http://localhost:40321).

    Returns:
        List of float32 vectors, one per input item, preserving order.
    """
    payload = {"model": model, "input": inputs}
    response = requests.post(
        f"{endpoint}/v1/embeddings",
        json=payload,
        timeout=120,
    )
    response.raise_for_status()
    data = response.json()["data"]
    # vLLM returns items in the same order as inputs
    return [item["embedding"] for item in data]
