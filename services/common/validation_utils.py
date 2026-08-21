import unicodedata
from pathlib import Path

from fastapi import UploadFile

from common.llm_utils import tokenize_with_llm
from common.misc_utils import get_logger

logger = get_logger("validation")

_ALLOWED_EXTENSIONS = frozenset({"txt", "md"})

# Probe only the first 8 KB — large enough to catch null bytes, invalid UTF-8,
# or control-character runs in virtually any misnamed binary file, while keeping
# the check cheap.
_MAX_PROBE_BYTES = 8192


def validate_file_extension(filename: str) -> str:
    """Validate that *filename* has an allowed extension (.txt or .md).

    Returns the bare extension string (e.g. ``"txt"`` or ``"md"``) on success.

    Raises:
        ValueError: The extension is missing or not in the allowed set.
    """
    suffix = Path(filename).suffix.lstrip(".").lower()
    if suffix not in _ALLOWED_EXTENSIONS:
        raise ValueError(
            f"File type '.{suffix}' is not supported. "
            f"Accepted types: {', '.join(sorted(_ALLOWED_EXTENSIONS))}."
        )
    return suffix


async def validate_file_content(file: UploadFile) -> None:
    """Probe the first 8 KB of *file* to confirm it is a genuine UTF-8 text file.

    Resets the file pointer to 0 after the check so the caller can still read
    the full content.

    Raises:
        ValueError: on an empty file, invalid UTF-8, null bytes, excessive
                    control characters, or PDF magic bytes.
    """
    probe = await file.read(_MAX_PROBE_BYTES)
    await file.seek(0)

    if not probe:
        raise ValueError("File is empty.")

    try:
        decoded = probe.decode("utf-8")
    except UnicodeDecodeError:
        raise ValueError("File content is not valid UTF-8 text.")

    if b"\x00" in probe:
        raise ValueError("File contains null bytes and appears to be binary.")

    control_count = sum(
        1 for ch in decoded
        if unicodedata.category(ch).startswith("Cc")
        and ch not in ("\n", "\r", "\t", "\f")
    )
    if len(decoded) > 0 and (control_count / len(decoded)) > 0.05:
        raise ValueError(
            "File contains excessive control characters and appears to be binary."
        )

    if probe[:4] == b"%PDF":
        ext = Path(file.filename or "").suffix.lower()
        raise ValueError(
            f"File has '{ext or 'unknown'}' extension but contains PDF content."
        )


def validate_query_length(query: str, emb_endpoint: str, max_token_length: int) -> tuple[bool, str | None]:
    """
    Validate that query length does not exceed maximum allowed tokens.

    Args:
        query: The query string to validate
        emb_endpoint: Endpoint used for tokenization
        max_token_length: Maximum allowed token count (service-specific)

    Returns:
        (is_valid, error_message) tuple
    """
    try:
        tokens = tokenize_with_llm(query, emb_endpoint)
        token_count = len(tokens)

        if token_count > max_token_length:
            error_msg = f"Query length ({token_count} tokens) exceeds maximum allowed length of {max_token_length} tokens"
            logger.warning(error_msg)
            return False, error_msg

        return True, None
    except Exception as e:
        logger.error(f"Error validating query length: {e}")
        # If tokenization fails, allow the request to proceed
        # to avoid blocking legitimate requests due to tokenization issues
        return True, None
