"""
Utility functions for async summarization job management.

Includes file validation, staging, directory initialization, and job operations.
"""

import os
import shutil
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional, Tuple

from fastapi import UploadFile

from common.misc_utils import get_logger
from summarize.settings import settings
from summarize.db.models import SummarizeJob
from summarize.db.database import get_db_session

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


def cleanup_staging_directory(job_id: str) -> None:
    """
    Clean up the staging directory for a job.
    
    Args:
        job_id: UUID of the job
    """
    job_staging_dir = settings.summarize.staging_dir / job_id
    
    if job_staging_dir.exists():
        try:
            shutil.rmtree(job_staging_dir)
            logger.debug(f"Cleaned up staging directory for job {job_id}")
        except Exception as e:
            logger.warning(f"Failed to clean up staging directory for job {job_id}: {e}")


def create_job_record(
    job_id: str,
    document_name: str,
    level: str = 'standard',
    job_name: Optional[str] = None
) -> SummarizeJob:
    """
    Create a new job record in the database.
    
    Args:
        job_id: UUID for the job
        document_name: Original filename
        level: Summarization level (brief, standard, detailed)
        job_name: Optional human-readable job name
        
    Returns:
        Created SummarizeJob instance
        
    Raises:
        Exception: If database insertion fails
    """
    job = SummarizeJob(
        job_id=job_id,
        job_name=job_name,
        status='accepted',
        submitted_at=datetime.now(timezone.utc),
        document_name=document_name,
        level=level,
        job_type='direct',  # Will be updated to 'chunked' if needed during processing
    )
    
    try:
        with get_db_session() as session:
            session.add(job)
            session.commit()
            session.refresh(job)
        
        logger.info(f"Created job record: {job_id}")
        return job
        
    except Exception as e:
        logger.error(f"Failed to create job record for {job_id}: {e}")
        raise


def get_job_by_id(job_id: str) -> Optional[SummarizeJob]:
    """
    Retrieve a job record by ID.
    
    Args:
        job_id: UUID of the job
        
    Returns:
        SummarizeJob instance or None if not found
    """
    try:
        with get_db_session() as session:
            job = session.query(SummarizeJob).filter(
                SummarizeJob.job_id == job_id
            ).first()
            return job
    except Exception as e:
        logger.error(f"Failed to retrieve job {job_id}: {e}")
        return None


def update_job_status(
    job_id: str,
    status: str,
    error: Optional[str] = None,
    completed_at: Optional[datetime] = None
) -> bool:
    """
    Update the status of a job.
    
    Args:
        job_id: UUID of the job
        status: New status (accepted, in_progress, completed, failed)
        error: Error message if status is 'failed'
        completed_at: Completion timestamp
        
    Returns:
        True if update successful, False otherwise
    """
    try:
        with get_db_session() as session:
            job = session.query(SummarizeJob).filter(
                SummarizeJob.job_id == job_id
            ).first()
            
            if not job:
                logger.warning(f"Job {job_id} not found for status update")
                return False
            
            job.status = status
            if error:
                job.error = error
            if completed_at:
                job.completed_at = completed_at
            
            session.commit()
            logger.info(f"Updated job {job_id} status to {status}")
            return True
            
    except Exception as e:
        logger.error(f"Failed to update job {job_id} status: {e}")
        return False
