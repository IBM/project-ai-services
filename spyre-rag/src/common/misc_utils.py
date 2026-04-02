import hashlib
import logging
import os
from contextvars import ContextVar
from digitize.config import DIGITIZED_DOCS_DIR

# ContextVar to store the request ID for each request
request_id_ctx = ContextVar("request_id", default="-")


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
    def __init__(self, log_level):
        super().__init__()
        self.log_level = log_level
        # Endpoints to filter out at INFO level
        self.filtered_paths = ['/health', '/v1/jobs']
    
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

def set_request_id(request_id: str):
    #Set the request ID for the current context.
    request_id_ctx.set(request_id)

def get_request_id() -> str:
    # Get the request ID from the current context. Currently unused.
    return request_id_ctx.get()

LOG_LEVEL = logging.INFO

LOCAL_CACHE_DIR = os.getenv("LOCAL_CACHE_DIR", "/var/cache")
chunk_suffix = "_clean_chunk.json"
text_suffix = "_clean_text.json"
table_suffix = "_tables.json"

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


def get_txt_tab_filenames(file_paths, out_path):
    original_filenames = [fp.split('/')[-1] for fp in file_paths]
    input_txt_files, input_tab_files = [], []
    for fn in original_filenames:
        f, _ = os.path.splitext(fn)
        input_txt_files.append(f'{out_path}/{f}{text_suffix}')
        input_tab_files.append(f'{out_path}/{f}{table_suffix}')
    return original_filenames, input_txt_files, input_tab_files


def get_model_endpoints():
    emb_model_dict = {
        'emb_endpoint': os.getenv("EMB_ENDPOINT"),
        'emb_model':    os.getenv("EMB_MODEL"),
        'max_tokens':   int(os.getenv("EMB_MAX_TOKENS", "512")),
    }

    llm_model_dict = {
        'llm_endpoint': os.getenv("LLM_ENDPOINT", ""),
        'llm_model':    os.getenv("LLM_MODEL", ""),
    }

    reranker_model_dict = {
        'reranker_endpoint': os.getenv("RERANKER_ENDPOINT"),
        'reranker_model':    os.getenv("RERANKER_MODEL"),
    }

    return emb_model_dict, llm_model_dict, reranker_model_dict

def setup_digitized_doc_dir():
    os.makedirs(DIGITIZED_DOCS_DIR, exist_ok=True)
    return DIGITIZED_DOCS_DIR

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
