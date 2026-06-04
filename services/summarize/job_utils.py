"""
Utility functions for async summarization job management.

Includes file validation, staging, directory initialization, and job operations.
"""

import os
import shutil

from pathlib import Path
from typing import Optional, Tuple

from fastapi import UploadFile

from common.misc_utils import get_logger
from summarize.settings import settings

logger = get_logger("job_utils")

# Allowed file extensions for summarization
ALLOWED_EXTENSIONS = {".txt", ".pdf"}


def ensure_directories() -> None:
    """
    Ensure that required cache directories exist.
    
    Creates:
    - /var/cache/summarize/staging/
    - /var/cache/summarize/results/
    """
    staging_dir = settings.summarize.staging_dir
    results_dir = settings.summarize.results_dir
    
    for directory in [staging_dir, results_dir]:
        directory.mkdir(parents=True, exist_ok=True)
        logger.debug(f"Ensured directory exists: {directory}")


def validate_file_extension(filename: str) -> Tuple[bool, Optional[str]]:
    """
    Validate that the file has an allowed extension.
    
    Args:
        filename: Name of the file to validate
        
    Returns:
        Tuple of (is_valid, extension)
        - is_valid: True if extension is allowed
        - extension: The file extension (e.g., '.pdf') or None
    """
    if not filename:
        return False, None
    
    ext = os.path.splitext(filename)[1].lower()
    is_valid = ext in ALLOWED_EXTENSIONS
    
    return is_valid, ext if is_valid else None


def stage_uploaded_file(job_id: str, file: UploadFile) -> Path:
    """
    Stage an uploaded file to the staging directory.
    
    Args:
        job_id: UUID of the job
        file: FastAPI UploadFile object
        
    Returns:
        Path to the staged file
        
    Raises:
        IOError: If file staging fails
    """
    # Create job-specific staging directory
    job_staging_dir = settings.summarize.staging_dir / job_id
    job_staging_dir.mkdir(parents=True, exist_ok=True)
    
    # Determine staged file path
    filename = file.filename or "uploaded_file"
    staged_file_path = job_staging_dir / filename
    
    try:
        # Write file to staging directory
        with open(staged_file_path, 'wb') as f:
            shutil.copyfileobj(file.file, f)
        
        logger.info(f"Staged file for job {job_id}: {staged_file_path}")
        return staged_file_path
        
    except Exception as e:
        logger.error(f"Failed to stage file for job {job_id}: {e}")
        # Clean up partial staging directory
        if job_staging_dir.exists():
            shutil.rmtree(job_staging_dir, ignore_errors=True)
        raise IOError(f"Failed to stage file: {e}")



