"""
Utility models and functions for the retrieve API.
Contains all Pydantic models for request/response validation and Swagger documentation.
"""

from pydantic import BaseModel, Field
from typing import Optional


class ReferenceRequest(BaseModel):
    """Request model for document retrieval"""
    prompt: str = Field(..., description="Search query to retrieve relevant documents")


class Document(BaseModel):
    """Document chunk with metadata"""
    page_content: str = Field(..., description="The text content of the document chunk")
    filename: str = Field(default="", description="Source filename")
    type: str = Field(default="", description="Document type (text, image, table)")
    source: str = Field(default="", description="Source path or HTML content")
    chunk_id: int = Field(default=0, description="Unique chunk identifier")


class ReferenceResponse(BaseModel):
    """Response containing retrieved documents and performance metrics"""
    documents: list[Document] = Field(..., description="List of retrieved document chunks")
    perf_metrics: dict = Field(..., description="Performance metrics for the retrieval operation")

    model_config = {
        "json_schema_extra": {
            "example": {
                "documents": [
                    {
                        "page_content": "Artificial intelligence is transforming industries...",
                        "filename": "ai_report.pdf",
                        "type": "text",
                        "source": "/path/to/ai_report.pdf",
                        "chunk_id": "12345"
                    }
                ],
                "perf_metrics": {
                    "retrieve_time": 0.15,
                    "rerank_time": 0.12
                }
            }
        }
    }


class Message(BaseModel):
    """Chat message"""
    content: str = Field(..., description="The content of the message")


class ChatCompletionRequest(BaseModel):
    """Request model for chat completion"""
    messages: list[Message] = Field(..., description="List of messages in the conversation")
    max_tokens: int = Field(default=512, description="Maximum number of tokens to generate")
    temperature: float = Field(default=0.1, description="Sampling temperature (0.0 to 2.0)")
    stop: Optional[list[str]] = Field(default=None, description="Stop sequences for generation")
    stream: bool = Field(default=False, description="Whether to stream the response")


class ChatMessage(BaseModel):
    """Chat message in response"""
    content: str = Field(..., description="The generated message content")


class ChatChoice(BaseModel):
    """Chat completion choice"""
    message: ChatMessage = Field(..., description="The generated message")


class ChatCompletionResponse(BaseModel):
    """Non-streaming chat completion response"""
    choices: list[ChatChoice] = Field(..., description="List of completion choices")

    model_config = {
        "json_schema_extra": {
            "example": {
                "choices": [
                    {
                        "message": {
                            "content": "Based on the retrieved documents, artificial intelligence..."
                        }
                    }
                ]
            }
        }
    }


class ModelInfo(BaseModel):
    """Model information"""
    id: str = Field(..., description="Model identifier")
    object: str = Field(default="model", description="Object type")
    created: Optional[int] = Field(default=None, description="Creation timestamp")
    owned_by: Optional[str] = Field(default=None, description="Model owner")


class ModelsResponse(BaseModel):
    """List of available models"""
    object: str = Field(default="list", description="Object type")
    data: list[ModelInfo] = Field(..., description="List of available models")

    model_config = {
        "json_schema_extra": {
            "example": {
                "object": "list",
                "data": [
                    {
                        "id": "ibm-granite/granite-3.3-8b-instruct",
                        "object": "model",
                        "created": 1234567890,
                        "owned_by": "ibm"
                    }
                ]
            }
        }
    }


class DBStatusResponse(BaseModel):
    """Database status response"""
    ready: bool = Field(..., description="Whether the vector database is ready")
    message: Optional[str] = Field(default=None, description="Additional status message")

    model_config = {
        "json_schema_extra": {
            "examples": [
                {
                    "ready": True
                },
                {
                    "ready": False,
                    "message": "No data ingested"
                }
            ]
        }
    }


class HealthResponse(BaseModel):
    """Health check response"""
    status: str = Field(..., description="Service health status")

    model_config = {
        "json_schema_extra": {
            "example": {
                "status": "ok"
            }
        }
    }
