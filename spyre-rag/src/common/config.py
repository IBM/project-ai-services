"""
Configuration settings for RAG system.
These values can be overridden via environment variables.
"""
import os
from pathlib import Path

# Prompts - Query streaming (English)
QUERY_VLLM_STREAM_PROMPT = os.getenv(
    "QUERY_VLLM_STREAM_PROMPT",
    "You are given:\n1. **A short context text** containing factual information.\n2. **A user's question** seeking clarification or advice.\n3. **Return a concise, to-the-point answer grounded strictly in the provided context.**\n\nThe answer should be accurate, easy to follow, based on the context(s), and include clear reasoning or justification.\nIf the context does not provide enough information, answer using your general knowledge.\n\nContext:\n{context}\n\nQuestion:\n{question}\n\nAnswer:"
)

# Prompts - Query streaming (German)
QUERY_VLLM_STREAM_DE_PROMPT = os.getenv(
    "QUERY_VLLM_STREAM_DE_PROMPT",
    "Sie erhalten: 1. **Einen kurzen Kontexttext** mit sachlichen Informationen.\n2. **Die Frage eines Nutzers**, der um Klärung oder Rat bittet.\n3. **Geben Sie eine prägnante und aussagekräftige Antwort, die sich strikt auf den gegebenen Kontext stützt.**\n\nDie Antwort sollte korrekt, leicht verständlich und kontextbezogen sein sowie eine klare Begründung enthalten.\nWenn der Kontext nicht genügend Informationen liefert, antworten Sie mit Ihrem Allgemeinwissen.\n\nKontext:{context}\n\nFrage:{question}\n\nAntwort:"
)

# Prompts - LLM classification
LLM_CLASSIFY_PROMPT = os.getenv(
    "LLM_CLASSIFY_PROMPT",
    "You are an intelligent assistant helping to curate a knowledge base for a Retrieval-Augmented Generation (RAG) system.\nYour task is to decide whether the following text should be included in the knowledge corpus. Respond only with \"yes\" or \"no\".\n\nRespond \"yes\" if the text contains factual, instructional, or explanatory information that could help answer general user questions on any topic.\nRespond \"no\" if the text contains personal, administrative, or irrelevant content, such as names, acknowledgements, contact info, disclaimers, legal notices, or unrelated commentary.\n\nText: {text}\n\nAnswer:"
)

# Prompts - Table summary
TABLE_SUMMARY_PROMPT = os.getenv(
    "TABLE_SUMMARY_PROMPT",
    "You are an intelligent assistant analyzing set of documents.\nYou are given a table extracted from a document. Your task is to summarize the key points and insights from the table. Avoid repeating the entire content; focus on what is meaningful or important.\n\nTable:\n{content}\n\nSummary:"
)

# Prompts - Summarization system prompt
SUMMARIZE_SYSTEM_PROMPT = os.getenv(
    "SUMMARIZE_SYSTEM_PROMPT",
    "You are a summarization assistant. Output ONLY the summary. \n\nDo not add questions, explanations, headings, code, or any other text."
)

# Prompts - Summarization user prompt with length
SUMMARIZE_USER_PROMPT_WITH_LENGTH = os.getenv(
    "SUMMARIZE_USER_PROMPT_WITH_LENGTH",
    "Summarize the following text in {target_words} words, with an allowed variance of ±50 words.Avoid being overly concise.\nExpand explanations where necessary to meet the word requirement.\n\nYou must strictly meet this word-range requirement. Do not exceed or fall short of the range.\n\n\nText:\n{text}\n\nSummary:"
)

# Prompts - Summarization user prompt without length
SUMMARIZE_USER_PROMPT_WITHOUT_LENGTH = os.getenv(
    "SUMMARIZE_USER_PROMPT_WITHOUT_LENGTH",
    "Summarize the following text.\n\nText:\n{text}\n\nSummary:"
)

# Context lengths
GRANITE_3_3_8B_INSTRUCT_CONTEXT_LENGTH = int(os.getenv("GRANITE_3_3_8B_INSTRUCT_CONTEXT_LENGTH", "32768"))

# Token to word ratios
TOKEN_TO_WORD_RATIO_EN = float(os.getenv("TOKEN_TO_WORD_RATIO_EN", "0.75"))

# RAG parameters
SCORE_THRESHOLD = float(os.getenv("SCORE_THRESHOLD", "0.5"))
MAX_CONCURRENT_REQUESTS = int(os.getenv("MAX_CONCURRENT_REQUESTS", "32"))
NUM_CHUNKS_POST_SEARCH = int(os.getenv("NUM_CHUNKS_POST_SEARCH", "10"))
NUM_CHUNKS_POST_RERANKER = int(os.getenv("NUM_CHUNKS_POST_RERANKER", "3"))

# LLM settings
LLM_MAX_TOKENS = int(os.getenv("LLM_MAX_TOKENS", "512"))
LLM_MAX_TOKENS_DE = int(os.getenv("LLM_MAX_TOKENS_DE", "700"))
TEMPERATURE = float(os.getenv("TEMPERATURE", "0.0"))
MAX_INPUT_LENGTH = int(os.getenv("MAX_INPUT_LENGTH", "6000"))
PROMPT_TEMPLATE_TOKEN_COUNT = int(os.getenv("PROMPT_TEMPLATE_TOKEN_COUNT", "250"))
MAX_QUERY_TOKEN_LENGTH = int(os.getenv("MAX_QUERY_TOKEN_LENGTH", "512"))

# Summarization settings
SUMMARIZATION_COEFFICIENT = float(os.getenv("SUMMARIZATION_COEFFICIENT", "0.2"))
SUMMARIZATION_PROMPT_TOKEN_COUNT = int(os.getenv("SUMMARIZATION_PROMPT_TOKEN_COUNT", "100"))
SUMMARIZATION_TEMPERATURE = float(os.getenv("SUMMARIZATION_TEMPERATURE", "0.2"))
SUMMARIZATION_STOP_WORDS = os.getenv("SUMMARIZATION_STOP_WORDS", "Keywords, Note, ***")

# Language detection
LANGUAGE_DETECTION_MIN_CONFIDENCE = float(os.getenv("LANGUAGE_DETECTION_MIN_CONFIDENCE", "0.5"))

# ============================================================================
# Digitize Service Configuration
# ============================================================================

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

# API concurrency limits
DIGITIZATION_CONCURRENCY_LIMIT = int(os.getenv("DIGITIZATION_CONCURRENCY_LIMIT", "2"))
INGESTION_CONCURRENCY_LIMIT = int(os.getenv("INGESTION_CONCURRENCY_LIMIT", "1"))

# LLM connection pool size
LLM_POOL_SIZE = int(os.getenv("LLM_POOL_SIZE", "32"))

# Chunking parameters
DEFAULT_MAX_TOKENS = int(os.getenv("CHUNK_MAX_TOKENS", "512"))
DEFAULT_OVERLAP_TOKENS = int(os.getenv("CHUNK_OVERLAP_TOKENS", "50"))

# Document conversion parameters
PDF_CHUNK_SIZE = int(os.getenv("PDF_CHUNK_SIZE", "100"))  # Pages per chunk for large PDF processing

# Batch processing
OPENSEARCH_BATCH_SIZE = int(os.getenv("OPENSEARCH_BATCH_SIZE", "10"))

# Retry configuration
RETRY_MAX_ATTEMPTS = int(os.getenv("RETRY_MAX_ATTEMPTS", "3"))
RETRY_INITIAL_DELAY = float(os.getenv("RETRY_INITIAL_DELAY", "0.5"))
RETRY_BACKOFF_MULTIPLIER = float(os.getenv("RETRY_BACKOFF_MULTIPLIER", "2.0"))

# Chunk ID generation
CHUNK_ID_CONTENT_SAMPLE_SIZE = int(os.getenv("CHUNK_ID_CONTENT_SAMPLE_SIZE", "500"))

# Made with Bob