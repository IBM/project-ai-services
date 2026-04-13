"""
Configuration settings for Digitize service.
These values can be overridden via environment variables.
"""
from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict
from common.settings import Settings as CommonSettings

class DigitizeConfig(BaseSettings):
    """Digitize service configuration."""

    # Directory paths
    cache_dir: Path = Field(
        default=Path("/var/cache"),
        description="Base cache directory for all operations",
    )

    # Worker pool sizes
    doc_worker_size: int = Field(
        default=4,
        ge=1,
        description="Number of workers for document processing",
    )

    heavy_pdf_convert_worker_size: int = Field(
        default=2,
        ge=1,
        description="Number of workers for heavy PDF conversion",
    )

    heavy_pdf_page_threshold: int = Field(
        default=500,
        ge=1,
        description="Page count threshold for heavy PDF classification",
    )

    # API concurrency limits
    digitization_concurrency_limit: int = Field(
        default=2,
        ge=1,
        description="Concurrency limit for digitization API",
    )

    ingestion_concurrency_limit: int = Field(
        default=1,
        ge=1,
        description="Concurrency limit for ingestion API",
    )

    # Chunking parameters
    chunk_max_tokens: int = Field(
        default=512,
        ge=1,
        description="Maximum tokens per chunk",
    )

    chunk_overlap_tokens: int = Field(
        default=50,
        ge=0,
        description="Overlap tokens between chunks",
    )

    # Document conversion parameters
    pdf_chunk_size: int = Field(
        default=100,
        ge=1,
        description="Pages per chunk for large PDF processing",
    )

    # Batch processing
    opensearch_batch_size: int = Field(
        default=10,
        ge=1,
        description="Batch size for OpenSearch operations",
    )

    # Retry configuration
    retry_max_attempts: int = Field(
        default=3,
        ge=1,
        description="Maximum retry attempts for failed operations",
    )

    retry_initial_delay: float = Field(
        default=0.5,
        gt=0.0,
        description="Initial delay in seconds for retry backoff",
    )

    retry_backoff_multiplier: float = Field(
        default=2.0,
        gt=1.0,
        description="Multiplier for exponential backoff",
    )

    # Chunk ID generation
    chunk_id_content_sample_size: int = Field(
        default=500,
        ge=1,
        description="Content sample size for chunk ID generation",
    )

    # LLM classification prompt
    llm_classify_prompt: str = Field(
        default=(
            "You are an intelligent assistant helping to curate a knowledge base for a Retrieval-Augmented Generation (RAG) system.\n"
            "Your task is to decide whether the following text should be included in the knowledge corpus. Respond only with \"yes\" or \"no\".\n\n"
            "Respond \"yes\" if the text contains factual, instructional, or explanatory information that could help answer general user questions on any topic.\n"
            "Respond \"no\" if the text contains personal, administrative, or irrelevant content, such as names, acknowledgements, contact info, disclaimers, legal notices, or unrelated commentary.\n\n"
            "Text: {text}\n\nAnswer:"
        ),
        description="Prompt for LLM-based text classification",
    )

    # Table summary prompt
    table_summary_prompt: str = Field(
        default=(
            "You are an intelligent assistant analyzing set of documents.\n"
            "You are given a table extracted from a document. Your task is to summarize the key points and insights from the table. "
            "Avoid repeating the entire content; focus on what is meaningful or important.\n\n"
            "Table:\n{content}\n\nSummary:"
        ),
        description="Prompt for table summarization",
    )

    @property
    def docs_dir(self) -> Path:
        """Directory for document storage."""
        return self.cache_dir / "docs"

    @property
    def jobs_dir(self) -> Path:
        """Directory for job tracking."""
        return self.cache_dir / "jobs"

    @property
    def staging_dir(self) -> Path:
        """Directory for staging files."""
        return self.cache_dir / "staging"

    @property
    def digitized_docs_dir(self) -> Path:
        """Directory for digitized documents."""
        return self.cache_dir / "digitized"


class Settings(BaseSettings):
    common: CommonSettings = Field(default_factory=CommonSettings)
    digitize: DigitizeConfig = Field(default_factory=DigitizeConfig)

# Global settings instance
settings = Settings()

# Made with Bob
