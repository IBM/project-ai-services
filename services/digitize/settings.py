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
    table_summary_and_classify: str = Field(
        default="""You are an intelligent assistant analyzing tables extracted from documents.

                Your tasks:

                1. Extract and document EVERY piece of information from the table in extensive detail:
                - List ALL sections, subsections, and their reference numbers if present
                - Include EVERY specification, feature, number, code, condition, and requirement
                - Mention ALL items even if they seem minor - nothing should be omitted
                - Use structured format with clear organization (numbered lists, bullet points, or detailed paragraphs)
                - Be extremely thorough and comprehensive - aim for maximum detail
                - If the table has multiple rows/columns, describe each one
                - Preserve all technical terms, version numbers, and specific details exactly as shown

                2. Decide if the table is relevant for a knowledge base:
                - Relevant: contains factual, instructional, or explanatory info useful for answering questions.
                - Irrelevant: personal info, disclaimers, administrative notes, or unrelated commentary.

                3. Output in the exact format below:

                Summary: <your extremely detailed summary here - be verbose and comprehensive>
                Decision: <yes or no>

                Do NOT output JSON, extra commentary, or any other text.

                Examples:

                Positive example (relevant):
                Table:
                | Processor | Cores | Memory |
                |-----------|-------|--------|
                | Power10   | 16    | 8 TB   |

                Output:
                Summary: The table presents technical specifications for the Power10 processor. The processor configuration includes 16 cores for parallel processing capabilities. The memory capacity supports up to 8 TB (terabytes) of RAM, providing substantial memory resources for enterprise workloads and data-intensive applications.
                Decision: yes

                Negative example (irrelevant):
                Table:
                | Prepared by: | John Smith |
                |--------------|------------|

                Output:
                Summary: Document metadata indicating it was prepared by John Smith.
                Decision: no

                Now analyze the table below:

                Table:
                {content}""",
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
