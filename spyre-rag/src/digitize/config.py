"""
Configuration settings for document processing.
These values can be overridden via environment variables.
"""
import os
from pathlib import Path

# Directory paths
CACHE_DIR = Path(os.getenv("CACHE_DIR", "/var/cache"))
DOCS_DIR = CACHE_DIR / "docs"
JOBS_DIR = CACHE_DIR / "jobs"
STAGING_DIR = CACHE_DIR / "staging"
DIGITIZED_DOCS_DIR = CACHE_DIR / "digitized"

# Worker pool sizes
WORKER_SIZE = int(os.getenv("DOC_WORKER_SIZE", "4"))
HEAVY_PDF_CONVERT_WORKER_SIZE = int(os.getenv("HEAVY_PDF_CONVERT_WORKER_SIZE", "2"))
HEAVY_PDF_PAGE_THRESHOLD = int(os.getenv("HEAVY_PDF_PAGE_THRESHOLD", "500"))

# LLM connection pool size
LLM_POOL_SIZE = int(os.getenv("LLM_POOL_SIZE", "32"))

# Chunking parameters
DEFAULT_MAX_TOKENS = int(os.getenv("CHUNK_MAX_TOKENS", "512"))
DEFAULT_OVERLAP_TOKENS = int(os.getenv("CHUNK_OVERLAP_TOKENS", "50"))

# Batch processing
OPENSEARCH_BATCH_SIZE = int(os.getenv("OPENSEARCH_BATCH_SIZE", "10"))

# Retry configuration
RETRY_MAX_ATTEMPTS = int(os.getenv("RETRY_MAX_ATTEMPTS", "3"))
RETRY_INITIAL_DELAY = float(os.getenv("RETRY_INITIAL_DELAY", "0.5"))
RETRY_BACKOFF_MULTIPLIER = float(os.getenv("RETRY_BACKOFF_MULTIPLIER", "2.0"))

# Chunk ID generation
CHUNK_ID_CONTENT_SAMPLE_SIZE = int(os.getenv("CHUNK_ID_CONTENT_SAMPLE_SIZE", "500"))

# Made with Bob
