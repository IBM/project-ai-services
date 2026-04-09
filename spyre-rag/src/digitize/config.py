"""
Configuration settings for Digitize service.
These values can be overridden via environment variables.
"""
from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class DigitizeConfig(BaseSettings):
    """Digitize service configuration."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=True,
        extra="ignore",
    )

    # Directory paths
    cache_dir: Path = Field(
        alias="CACHE_DIR",
        default=Path("/var/cache"),
        description="Base cache directory for all operations",
    )

    # Worker pool sizes
    doc_worker_size: int = Field(
        default=4,
        ge=1,
        alias="DOC_WORKER_SIZE",
        description="Number of workers for document processing",
    )

    heavy_pdf_convert_worker_size: int = Field(
        alias="HEAVY_PDF_CONVERT_WORKER_SIZE",
        default=2,
        ge=1,
        description="Number of workers for heavy PDF conversion",
    )

    heavy_pdf_page_threshold: int = Field(
        alias="HEAVY_PDF_PAGE_THRESHOLD",
        default=500,
        ge=1,
        description="Page count threshold for heavy PDF classification",
    )

    # API concurrency limits
    digitization_concurrency_limit: int = Field(
        alias="DIGITIZATION_CONCURRENCY_LIMIT",
        default=2,
        ge=1,
        description="Concurrency limit for digitization API",
    )

    ingestion_concurrency_limit: int = Field(
        alias="INGESTION_CONCURRENCY_LIMIT",
        default=1,
        ge=1,
        description="Concurrency limit for ingestion API",
    )

    # Chunking parameters
    chunk_max_tokens: int = Field(
        alias="CHUNK_MAX_TOKENS",
        default=512,
        ge=1,
        description="Maximum tokens per chunk",
    )

    chunk_overlap_tokens: int = Field(
        alias="CHUNK_OVERLAP_TOKENS",
        default=50,
        ge=0,
        description="Overlap tokens between chunks",
    )

    # Document conversion parameters
    pdf_chunk_size: int = Field(
        alias="PDF_CHUNK_SIZE",
        default=100,
        ge=1,
        description="Pages per chunk for large PDF processing",
    )

    # Batch processing
    opensearch_batch_size: int = Field(
        alias="OPENSEARCH_BATCH_SIZE",
        default=10,
        ge=1,
        description="Batch size for OpenSearch operations",
    )

    # Retry configuration
    retry_max_attempts: int = Field(
        alias="RETRY_MAX_ATTEMPTS",
        default=3,
        ge=1,
        description="Maximum retry attempts for failed operations",
    )

    retry_initial_delay: float = Field(
        alias="RETRY_INITIAL_DELAY",
        default=0.5,
        gt=0.0,
        description="Initial delay in seconds for retry backoff",
    )

    retry_backoff_multiplier: float = Field(
        alias="RETRY_BACKOFF_MULTIPLIER",
        default=2.0,
        gt=1.0,
        description="Multiplier for exponential backoff",
    )

    # Chunk ID generation
    chunk_id_content_sample_size: int = Field(
        alias="CHUNK_ID_CONTENT_SAMPLE_SIZE",
        default=500,
        ge=1,
        description="Content sample size for chunk ID generation",
    )

    # LLM classification prompt
    llm_classify_prompt: str = Field(
        alias="LLM_CLASSIFY_PROMPT",
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
        alias="TABLE_SUMMARY_PROMPT",
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


# Global settings instance
settings = DigitizeConfig()

# Backward compatibility: expose settings as module-level constants
CACHE_DIR = settings.cache_dir
DOCS_DIR = settings.docs_dir
JOBS_DIR = settings.jobs_dir
STAGING_DIR = settings.staging_dir
DIGITIZED_DOCS_DIR = settings.digitized_docs_dir
WORKER_SIZE = settings.doc_worker_size
HEAVY_PDF_CONVERT_WORKER_SIZE = settings.heavy_pdf_convert_worker_size
HEAVY_PDF_PAGE_THRESHOLD = settings.heavy_pdf_page_threshold
DIGITIZATION_CONCURRENCY_LIMIT = settings.digitization_concurrency_limit
INGESTION_CONCURRENCY_LIMIT = settings.ingestion_concurrency_limit
DEFAULT_MAX_TOKENS = settings.chunk_max_tokens
DEFAULT_OVERLAP_TOKENS = settings.chunk_overlap_tokens
PDF_CHUNK_SIZE = settings.pdf_chunk_size
OPENSEARCH_BATCH_SIZE = settings.opensearch_batch_size
RETRY_MAX_ATTEMPTS = settings.retry_max_attempts
RETRY_INITIAL_DELAY = settings.retry_initial_delay
RETRY_BACKOFF_MULTIPLIER = settings.retry_backoff_multiplier
CHUNK_ID_CONTENT_SAMPLE_SIZE = settings.chunk_id_content_sample_size
LLM_CLASSIFY_PROMPT = settings.llm_classify_prompt
TABLE_SUMMARY_PROMPT = settings.table_summary_prompt

# Made with Bob
