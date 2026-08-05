"""
Job lifecycle utilities.

Thin coordinator layer between the API routers and the DB / storage layers.

Higher-level helpers (validate_file_extension, read_result_file, etc.) are added in later PRs alongside chunk_utils and job_utils.
"""

import uuid

from common.misc_utils import get_logger

logger = get_logger("jobs")


def generate_uuid() -> str:
    """Generate a random UUID string suitable for use as a job ID."""
    job_id = str(uuid.uuid4())
    logger.debug(f"Generated job UUID: {job_id}")
    return job_id

# Made with Bob
