"""
Utility functions for async summarization job management.

Includes file validation, staging, directory initialization, and job operations.
"""

import json
import os
import shutil

from datetime import datetime, timezone
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




def recover_zombie_jobs() -> int:
    """
    Scan for zombie jobs (accepted or in_progress) on startup and mark them as failed.
    
    This function is called during FastAPI startup to handle jobs that were interrupted
    by a system restart or crash. It:
    1. Queries the database for jobs with status 'accepted' or 'in_progress'
    2. Marks them as 'failed' with an appropriate error message
    3. Sets their completed_at timestamp
    4. Deletes their staging directories
    
    Returns:
        Number of zombie jobs recovered
        
    Note:
        This operation is atomic at the database level due to PostgreSQL transactions,
        preventing race conditions with newly submitted jobs.
    """
    from summarize.db.manager import db_repo
    from summarize.models import JobStatus
    
    logger.info("Starting zombie job recovery scan...")
    
    try:
        # Get all jobs with accepted or in_progress status
        zombie_jobs = db_repo.get_active_jobs()
        
        if not zombie_jobs:
            logger.info("No zombie jobs found")
            return 0
        
        recovered_count = 0
        error_message = "System restarted during processing"
        
        for job in zombie_jobs:
            job_id = job.job_id
            logger.warning(f"Found zombie job: {job_id} (status: {job.status})")
            
            try:
                # Mark job as failed with error message and completion timestamp
                success = db_repo.update_job(
                    job_id=job_id,
                    status=JobStatus.FAILED,
                    error=error_message,
                    completed_at=datetime.now(timezone.utc)
                )
                
                if success:
                    logger.info(f"Marked zombie job {job_id} as failed")
                    
                    # Delete staging directory for this job
                    staging_path = settings.summarize.staging_dir / job_id
                    if staging_path.exists():
                        try:
                            shutil.rmtree(staging_path, ignore_errors=True)
                            logger.info(f"Deleted staging directory for zombie job {job_id}")
                        except Exception as cleanup_error:
                            logger.error(f"Failed to delete staging directory for job {job_id}: {cleanup_error}")
                    
                    recovered_count += 1
                else:
                    logger.error(f"Failed to update zombie job {job_id} in database")
                    
            except Exception as job_error:
                logger.error(f"Error recovering zombie job {job_id}: {job_error}", exc_info=True)
        
        logger.info(f"Zombie job recovery complete: {recovered_count} jobs recovered")
        return recovered_count
        
    except Exception as e:
        logger.error(f"Error during zombie job recovery scan: {e}", exc_info=True)
        return 0
