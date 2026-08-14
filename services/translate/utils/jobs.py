"""
Job lifecycle utilities.

Thin coordinator layer between the API routers and the DB / storage layers.
Provides:

- ``generate_uuid``          — generate a random UUID for a new job.
- ``validate_file_extension`` — check ``.txt`` / ``.md`` only.
- ``validate_file_content``  — probe first 8 KB for binary / encoding issues.
- ``stage_uploaded_file``    — persist uploaded bytes to the per-job staging dir.
- ``read_result_file``       — load the result JSON for a completed job.
- ``delete_job_files``       — clean up staging dir + result file after a job ends.
"""

import unicodedata
import uuid
from pathlib import Path

from fastapi import UploadFile

from common.misc_utils import get_logger
from translate.utils.errors import _raise_unsupported_file_type
from translate.utils.storage import storage_manager

logger = get_logger("jobs")

_ALLOWED_EXTENSIONS = frozenset({"txt", "md"})


def generate_uuid() -> str:
    """Generate a random UUID string suitable for use as a job ID."""
    job_id = str(uuid.uuid4())
    logger.debug(f"Generated job UUID: {job_id}")
    return job_id


def validate_file_extension(filename: str) -> str:
    suffix = Path(filename).suffix.lstrip(".").lower()
    if suffix not in _ALLOWED_EXTENSIONS:
        raise ValueError(
            f"File type '.{suffix}' is not supported. "
            f"Accepted types: {', '.join(sorted(_ALLOWED_EXTENSIONS))}."
        )
    return suffix


# Probe only the first 8 KB — large enough to catch null bytes, invalid UTF-8,
# or control-character runs in virtually any misnamed binary file, while keeping
# the check cheap. The full UTF-8 decode in the worker catches anything deeper.
_MAX_PROBE_BYTES = 8192


async def validate_file_content(file: UploadFile) -> None:
    """
    Probe the first 8 KB of *file* to confirm it is a genuine UTF-8 text file.
    Resets the file pointer to 0 after the check so the caller can still read
    the full content.

    Raises ``HTTPException(415)`` on:
    - empty file
    - invalid UTF-8 encoding
    - null bytes (binary file)
    - excessive control characters (> 5% of probe)
    - PDF magic bytes (``%PDF``) regardless of declared extension
    """
    probe = await file.read(_MAX_PROBE_BYTES)
    await file.seek(0)

    if not probe:
        _raise_unsupported_file_type("File is empty.")

    try:
        decoded = probe.decode("utf-8")
    except UnicodeDecodeError:
        _raise_unsupported_file_type("File content is not valid UTF-8 text.")

    if b"\x00" in probe:
        _raise_unsupported_file_type(
            "File contains null bytes and appears to be binary."
        )

    control_count = sum(
        1 for ch in decoded
        if unicodedata.category(ch).startswith("Cc")
        and ch not in ("\n", "\r", "\t", "\f")
    )
    if len(decoded) > 0 and (control_count / len(decoded)) > 0.05:
        _raise_unsupported_file_type(
            "File contains excessive control characters and appears to be binary."
        )

    if probe[:4] == b"%PDF":
        ext = Path(file.filename or "").suffix.lower()
        _raise_unsupported_file_type(
            f"File has '{ext or 'unknown'}' extension but contains PDF content."
        )


async def stage_uploaded_file(job_id: str, filename: str, content: bytes) -> Path:
    """Write uploaded bytes to the per-job staging directory. Returns the ``Path`` to the staged file."""
    return await storage_manager.stage_upload_file(
        job_id=job_id,
        filename=filename,
        content=content,
    )


def read_result_file(job_id: str) -> dict:
    """
    Load the result JSON for a completed job from disk.

    Args:
        job_id: Unique job identifier.

    Returns:
        Deserialised result dict (``data``, ``meta``, ``usage`` keys).

    Raises:
        FileNotFoundError: Result file does not exist (job may not have
                           completed or files were cleaned up).
    """
    return storage_manager.read_result(job_id)


def delete_job_files(job_id: str) -> None:
    """
    Remove the staging directory **and** the result file for *job_id*.

    Both operations are best-effort — errors are logged but not re-raised,
    so a partial clean-up never fails the caller.

    Args:
        job_id: Unique job identifier.
    """
    storage_manager.cleanup_staging(job_id)
    try:
        storage_manager.delete_result(job_id)
    except Exception as exc:
        logger.warning(f"Could not delete result file for job {job_id}: {exc}")

# Made with Bob
