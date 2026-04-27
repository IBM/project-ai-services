import hashlib
import logging
import os
import requests
from contextvars import ContextVar
from requests.adapters import HTTPAdapter

# ContextVar to store the request ID for each request
request_id_ctx = ContextVar("request_id", default="-")

# Global SESSION for all LLM and embedding API calls
SESSION = None


class DoclingConversionError(Exception):
    """Exception raised when Docling document conversion fails.

    This exception wraps any errors that occur during PDF conversion
    using the Docling library, making them identifiable for retry logic.
    """
    pass

class RequestIDFilter(logging.Filter):
    """Filter to inject request_id from ContextVar into log records."""
    def filter(self, record):
        record.request_id = request_id_ctx.get()
        return True


class RequestIDFormatter(logging.Formatter):
    """Custom formatter that conditionally includes request_id only when present."""
    def format(self, record):
        # Get the request_id from the record
        request_id = getattr(record, 'request_id', '-')

        # If request_id is the default "-", don't include it in the format
        if request_id == '-':
            # Format without request_id brackets
            self._style._fmt = '%(asctime)s - %(name)-18s - %(levelname)-8s - %(message)s'
        else:
            # Format with request_id
            self._style._fmt = '%(asctime)s - %(name)-18s - %(levelname)-8s - [%(request_id)s] - %(message)s'

        return super().format(record)


class EndpointFilter(logging.Filter):
    """
    Filter to exclude health check and polling endpoints from access logs.

    These endpoints are only logged when LOG_LEVEL is set to DEBUG.
    """
    def __init__(self, log_level, filtered_paths):
        super().__init__()
        self.log_level = log_level
        # Endpoints to filter out at INFO level
        self.filtered_paths = filtered_paths

    def filter(self, record):
        # If DEBUG level, allow all logs through
        if self.log_level == logging.DEBUG:
            return True

        # At INFO level or higher, filter out health checks and job polling
        message = record.getMessage()

        # Check if this is an access log for filtered endpoints
        for path in self.filtered_paths:
            if path in message and 'GET' in message:
                return False

        return True

def configure_uvicorn_logging(log_level, filtered_paths):
    """
    Configure uvicorn loggers with custom formatting and filtering.

    This function should be called after uvicorn sets up its logging (e.g., in lifespan).
    It applies consistent formatting to uvicorn's main and access loggers, and adds
    endpoint filtering to the access logger to reduce noise from health checks.

    Args:
        log_level: The logging level to apply (e.g., logging.INFO, logging.DEBUG)
        filtered_paths: List of endpoint paths to filter from access logs at INFO level
    """
    # Custom formatter matching application log format (without request_id for uvicorn logs)
    formatter = logging.Formatter(
        '%(asctime)s - %(name)-18s - %(levelname)-8s - %(message)s',
        datefmt='%Y-%m-%d %H:%M:%S'
    )

    # Configure uvicorn main logger (startup messages, etc.)
    uvicorn_logger = logging.getLogger("uvicorn")
    uvicorn_logger.setLevel(log_level)
    for handler in uvicorn_logger.handlers:
        handler.setFormatter(formatter)

    # Configure uvicorn.access logger (HTTP access logs)
    uvicorn_access_logger = logging.getLogger("uvicorn.access")
    uvicorn_access_logger.setLevel(log_level)
    for handler in uvicorn_access_logger.handlers:
        handler.setFormatter(formatter)

    # Apply endpoint filter to access logger only
    uvicorn_access_logger.addFilter(EndpointFilter(log_level, filtered_paths))


def set_request_id(request_id: str):
    #Set the request ID for the current context.
    request_id_ctx.set(request_id)

def get_request_id() -> str:
    # Get the request ID from the current context. Currently unused.
    return request_id_ctx.get()

LOG_LEVEL = logging.INFO

LOCAL_CACHE_DIR = os.getenv("LOCAL_CACHE_DIR", "/var/cache")
text_chunk_suffix = "_text_chunk.json"
text_suffix = "_text.json"
table_suffix = "_table.json"
table_chunk_suffix = "_table_chunk.json"

def set_log_level(level):
    global LOG_LEVEL
    LOG_LEVEL = level

def get_logger(name):
    logger = logging.getLogger(name)
    logger.setLevel(LOG_LEVEL)
    logger.propagate = False

    # Add the filter to inject request_id
    logger.addFilter(RequestIDFilter())

    console_handler = logging.StreamHandler()
    console_handler.setLevel(LOG_LEVEL)

    # Use custom formatter that conditionally includes request_id
    formatter = RequestIDFormatter(
        '%(asctime)s - %(name)-18s - %(levelname)-8s - [%(request_id)s] - %(message)s',
        datefmt='%Y-%m-%d %H:%M:%S')
    console_handler.setFormatter(formatter)

    logger.addHandler(console_handler)

    return logger


def create_llm_session(pool_maxsize, pool_connections: int = 3, pool_block: bool = True):
    global SESSION

    # SESSION object will be used by instruct and embedding endpoints. Hence keeping pool_connections = 2
    # Need to use SESSION object for following reasons:
    # - To limit the number of concurrent requests getting created to instruct vLLM's API to 32
    # - To fix the ephemeral port exhaustion issue during chunking, since numerous tokenize calls are made to embedding server
    if SESSION is None:
        adapter = HTTPAdapter(
            pool_connections=pool_connections,
            pool_maxsize=pool_maxsize,
            pool_block=pool_block
        )

        session = requests.Session()
        session.mount("http://", adapter)
        session.mount("https://", adapter)

        SESSION = session


def get_txt_tab_filenames(file_paths, out_path):
    original_filenames = [fp.split('/')[-1] for fp in file_paths]
    input_txt_files, input_tab_files = [], []
    for fn in original_filenames:
        f, _ = os.path.splitext(fn)
        input_txt_files.append(f'{out_path}/{f}{text_suffix}')
        input_tab_files.append(f'{out_path}/{f}{table_suffix}')
    return original_filenames, input_txt_files, input_tab_files


def get_model_endpoints():
    from common.settings import settings

    emb_model_dict = {
        'emb_endpoint': settings.model_endpoints.emb_endpoint,
        'emb_model':    settings.model_endpoints.emb_model,
        'max_tokens':   settings.model_endpoints.emb_max_tokens,
    }

    llm_model_dict = {
        'llm_endpoint': settings.model_endpoints.llm_endpoint,
        'llm_model':    settings.model_endpoints.llm_model,
    }

    reranker_model_dict = {
        'reranker_endpoint': settings.model_endpoints.reranker_endpoint,
        'reranker_model':    settings.model_endpoints.reranker_model,
    }

    return emb_model_dict, llm_model_dict, reranker_model_dict

def setup_digitized_doc_dir():
    from digitize.settings import settings

    os.makedirs(settings.digitize.digitized_docs_dir, exist_ok=True)
    return settings.digitize.digitized_docs_dir

def generate_file_checksum(file):
    sha256 = hashlib.sha256()
    with open(file, 'rb') as f:
        for chunk in iter(lambda: f.read(128 * sha256.block_size), b''):
            sha256.update(chunk)
    return sha256.hexdigest()

def verify_checksum(file, checksum_file):
    file_sha256 = generate_file_checksum(file)
    f = open(checksum_file, "r")
    data = f.read()
    csum = data.split(' ')[0]
    if csum == file_sha256:
        return True
    return False


def validate_pdf_file(filename: str, content) -> None:
    """
    Validate a PDF file with comprehensive checks.

    Performs validation checks:
    1. Filename exists
    2. Content was read successfully (not an Exception)
    3. Content is not empty
    4. File has .pdf extension
    5. File content is valid PDF (magic bytes check)

    Args:
        filename: Name of the file
        content: File content as bytes (at least first 4 bytes), or Exception if read failed

    Raises:
        ValueError: If validation fails
    """
    # Check filename exists
    if not filename:
        raise ValueError("File must have a filename.")

    # Validate .pdf extension
    if not filename.lower().endswith('.pdf'):
        raise ValueError(f"Only PDF files are allowed. Invalid file: {filename}")

    pdf_signature = b'%PDF'
    if not content.startswith(pdf_signature):
        raise ValueError(f"File has .pdf extension but unsupported format: {filename}")

    # Check content is bytes (not an exception from failed read)
    if isinstance(content, Exception):
        raise ValueError(f"Failed to read file: {filename}")

    if not isinstance(content, bytes):
        raise ValueError(f"Invalid file content for: {filename}")

    # Check content is not empty
    if len(content) == 0:
        raise ValueError(f"File is empty: {filename}")

def get_unprocessed_files(original_files, processed_pdfs):
    return set(original_files).difference(set(processed_pdfs))
