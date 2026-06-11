"""
Utility functions for async summarization job management.

Includes file validation, staging, directory initialization, and job operations.
"""

import json
import os
import shutil

from pathlib import Path
from typing import Optional, Tuple, Dict, Any

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


def read_result_file(job_id: str) -> Optional[Dict[str, Any]]:
    """
    Read and parse result JSON file for a job.
    
    Args:
        job_id: UUID of the job
        
    Returns:
        Dictionary with result data or None if file doesn't exist
    """
    result_path = settings.summarize.results_dir / f"{job_id}_result.json"
    if not result_path.exists():
        logger.debug(f"Result file not found for job {job_id}")
        return None
    
    try:
        with open(result_path, 'r', encoding='utf-8') as f:
            result_data = json.load(f)
        logger.debug(f"Read result file for job {job_id}")
        return result_data
    except json.JSONDecodeError as e:
        logger.error(f"Failed to parse result file for job {job_id}: {e}")
        return None
    except Exception as e:
        logger.error(f"Failed to read result file for job {job_id}: {e}")
        return None


def delete_job_files(job_id: str) -> None:
    """
    Delete result file and staging directory for a job.
    
    Args:
        job_id: UUID of the job
    """
    # Delete result file
    result_path = settings.summarize.results_dir / f"{job_id}_result.json"
    if result_path.exists():
        try:
            result_path.unlink()
            logger.debug(f"Deleted result file for job {job_id}")
        except Exception as e:
            logger.error(f"Failed to delete result file for job {job_id}: {e}")
    
    # Delete staging directory
    staging_path = settings.summarize.staging_dir / job_id
    if staging_path.exists():
        try:
            shutil.rmtree(staging_path, ignore_errors=True)
            logger.debug(f"Deleted staging directory for job {job_id}")
        except Exception as e:
            logger.error(f"Failed to delete staging directory for job {job_id}: {e}")


def delete_all_job_files() -> None:
    """
    Delete all result files and staging directories.
    Used for bulk cleanup operations.
    """
    # Delete all result files
    results_dir = settings.summarize.results_dir
    if results_dir.exists():
        for file in results_dir.glob("*_result.json"):
            try:
                file.unlink()
                logger.debug(f"Deleted result file: {file.name}")
            except Exception as e:
                logger.error(f"Failed to delete result file {file.name}: {e}")
    
    # Delete all staging directories
    staging_dir = settings.summarize.staging_dir
    if staging_dir.exists():
        for job_dir in staging_dir.iterdir():
            if job_dir.is_dir():
                try:
                    shutil.rmtree(job_dir, ignore_errors=True)
                    logger.debug(f"Deleted staging directory: {job_dir.name}")
                except Exception as e:
                    logger.error(f"Failed to delete staging directory {job_dir.name}: {e}")


