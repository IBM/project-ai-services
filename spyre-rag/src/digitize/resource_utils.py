"""
Resource management utilities for handling disk space, cleanup, and resource monitoring.
"""
import os
import shutil
import logging
from pathlib import Path
from typing import Optional, Tuple
from datetime import datetime, timedelta, timezone

from common.misc_utils import get_logger

logger = get_logger("resource_utils")

# Constants
MIN_FREE_SPACE_GB = 5  # Minimum free space required in GB
STAGING_CLEANUP_AGE_HOURS = 24  # Clean up staging dirs older than this
FAILED_JOB_RETENTION_DAYS = 7  # Keep failed job data for this many days


class DiskSpaceError(Exception):
    """Raised when insufficient disk space is available."""
    pass


def get_disk_usage(path: str) -> Tuple[int, int, int]:
    """
    Get disk usage statistics for a given path.
    
    Args:
        path: Directory path to check
        
    Returns:
        Tuple of (total, used, free) in bytes
    """
    try:
        stat = shutil.disk_usage(path)
        return stat.total, stat.used, stat.free
    except Exception as e:
        logger.error(f"Failed to get disk usage for {path}: {e}")
        raise


def check_disk_space(path: str, required_gb: float = MIN_FREE_SPACE_GB) -> bool:
    """
    Check if sufficient disk space is available.
    
    Args:
        path: Directory path to check
        required_gb: Minimum required free space in GB
        
    Returns:
        True if sufficient space is available
        
    Raises:
        DiskSpaceError: If insufficient space is available
    """
    try:
        total, used, free = get_disk_usage(path)
        free_gb = free / (1024 ** 3)
        
        logger.debug(f"Disk space check for {path}: {free_gb:.2f} GB free")
        
        if free_gb < required_gb:
            raise DiskSpaceError(
                f"Insufficient disk space. Required: {required_gb} GB, "
                f"Available: {free_gb:.2f} GB"
            )
        
        return True
    except DiskSpaceError:
        raise
    except Exception as e:
        logger.error(f"Error checking disk space: {e}")
        raise


def estimate_required_space(file_sizes: list, multiplier: float = 3.0) -> float:
    """
    Estimate required disk space for processing files.
    
    Args:
        file_sizes: List of file sizes in bytes
        multiplier: Space multiplier (default 3x for intermediate files)
        
    Returns:
        Estimated required space in GB
    """
    total_size = sum(file_sizes)
    estimated_gb = (total_size * multiplier) / (1024 ** 3)
    return estimated_gb


def cleanup_staging_directory(staging_dir: str, job_id: Optional[str] = None, 
                              force: bool = False) -> bool:
    """
    Clean up staging directory for a specific job or all old jobs.
    
    Args:
        staging_dir: Base staging directory path
        job_id: Specific job ID to clean up (None for all old jobs)
        force: Force cleanup regardless of age
        
    Returns:
        True if cleanup was successful
    """
    try:
        staging_path = Path(staging_dir)
        
        if not staging_path.exists():
            logger.debug(f"Staging directory does not exist: {staging_dir}")
            return True
        
        if job_id:
            # Clean up specific job
            job_path = staging_path / job_id
            if job_path.exists():
                shutil.rmtree(job_path)
                logger.info(f"Cleaned up staging directory for job: {job_id}")
            return True
        
        # Clean up old staging directories
        cutoff_time = datetime.now(timezone.utc) - timedelta(hours=STAGING_CLEANUP_AGE_HOURS)
        cleaned_count = 0
        
        for item in staging_path.iterdir():
            if not item.is_dir():
                continue
            
            try:
                # Check directory age
                mtime = datetime.fromtimestamp(item.stat().st_mtime, tz=timezone.utc)
                
                if force or mtime < cutoff_time:
                    shutil.rmtree(item)
                    cleaned_count += 1
                    logger.debug(f"Cleaned up old staging directory: {item.name}")
            except Exception as e:
                logger.warning(f"Failed to clean up {item.name}: {e}")
        
        if cleaned_count > 0:
            logger.info(f"Cleaned up {cleaned_count} old staging directories")
        
        return True
    except Exception as e:
        logger.error(f"Error during staging cleanup: {e}")
        return False


def cleanup_failed_job(job_id: str, cache_dir: str = "/var/cache") -> bool:
    """
    Clean up resources for a failed job.
    
    Args:
        job_id: Job ID to clean up
        cache_dir: Base cache directory
        
    Returns:
        True if cleanup was successful
    """
    try:
        staging_dir = f"{cache_dir}/staging"
        docs_dir = f"{cache_dir}/docs"
        jobs_dir = f"{cache_dir}/jobs"
        
        # Clean up staging directory
        cleanup_staging_directory(staging_dir, job_id, force=True)
        
        # Clean up partial document files
        docs_path = Path(docs_dir)
        if docs_path.exists():
            # Read job status to get document IDs
            job_status_file = Path(jobs_dir) / f"{job_id}_status.json"
            if job_status_file.exists():
                import json
                with open(job_status_file, 'r') as f:
                    job_data = json.load(f)
                
                # Clean up document metadata and intermediate files
                for doc in job_data.get('documents', []):
                    doc_id = doc.get('id')
                    if doc_id:
                        # Remove all files related to this document
                        for pattern in [
                            f"{doc_id}*.json",
                            f"{doc_id}*.checksum"
                        ]:
                            for file_path in docs_path.glob(pattern):
                                try:
                                    file_path.unlink()
                                    logger.debug(f"Removed file: {file_path.name}")
                                except Exception as e:
                                    logger.warning(f"Failed to remove {file_path.name}: {e}")
        
        logger.info(f"Cleaned up resources for failed job: {job_id}")
        return True
    except Exception as e:
        logger.error(f"Error cleaning up failed job {job_id}: {e}")
        return False


def cleanup_old_failed_jobs(cache_dir: str = "/var/cache", 
                            retention_days: int = FAILED_JOB_RETENTION_DAYS) -> int:
    """
    Clean up old failed jobs based on retention policy.
    
    Args:
        cache_dir: Base cache directory
        retention_days: Number of days to retain failed job data
        
    Returns:
        Number of jobs cleaned up
    """
    try:
        import json
        from digitize.types import JobStatus
        
        jobs_dir = Path(cache_dir) / "jobs"
        if not jobs_dir.exists():
            return 0
        
        cutoff_time = datetime.now(timezone.utc) - timedelta(days=retention_days)
        cleaned_count = 0
        
        for status_file in jobs_dir.glob("*_status.json"):
            try:
                with open(status_file, 'r') as f:
                    job_data = json.load(f)
                
                # Check if job is failed and old enough
                if job_data.get('status') == JobStatus.FAILED.value:
                    submitted_at = datetime.fromisoformat(
                        job_data.get('submitted_at', '').replace('Z', '+00:00')
                    )
                    
                    if submitted_at < cutoff_time:
                        job_id = job_data.get('job_id')
                        if cleanup_failed_job(job_id, cache_dir):
                            # Remove job status file
                            status_file.unlink()
                            cleaned_count += 1
            except Exception as e:
                logger.warning(f"Failed to process {status_file.name}: {e}")
        
        if cleaned_count > 0:
            logger.info(f"Cleaned up {cleaned_count} old failed jobs")
        
        return cleaned_count
    except Exception as e:
        logger.error(f"Error cleaning up old failed jobs: {e}")
        return 0


def safe_cleanup(path: str, max_retries: int = 3) -> bool:
    """
    Safely clean up a path with retries.
    
    Args:
        path: Path to clean up
        max_retries: Maximum number of retry attempts
        
    Returns:
        True if cleanup was successful
    """
    import time
    
    for attempt in range(max_retries):
        try:
            path_obj = Path(path)
            if path_obj.exists():
                if path_obj.is_dir():
                    shutil.rmtree(path_obj)
                else:
                    path_obj.unlink()
                logger.debug(f"Successfully cleaned up: {path}")
            return True
        except Exception as e:
            if attempt < max_retries - 1:
                logger.warning(f"Cleanup attempt {attempt + 1} failed for {path}: {e}")
                time.sleep(1)  # Wait before retry
            else:
                logger.error(f"Failed to clean up {path} after {max_retries} attempts: {e}")
                return False
    
    return False


def get_directory_size(path: str) -> int:
    """
    Calculate total size of a directory.
    
    Args:
        path: Directory path
        
    Returns:
        Total size in bytes
    """
    total_size = 0
    try:
        for dirpath, dirnames, filenames in os.walk(path):
            for filename in filenames:
                filepath = os.path.join(dirpath, filename)
                try:
                    total_size += os.path.getsize(filepath)
                except (OSError, FileNotFoundError):
                    pass
    except Exception as e:
        logger.error(f"Error calculating directory size for {path}: {e}")
    
    return total_size


def log_resource_usage(cache_dir: str = "/var/cache") -> None:
    """
    Log current resource usage statistics.
    
    Args:
        cache_dir: Base cache directory
    """
    try:
        total, used, free = get_disk_usage(cache_dir)
        free_gb = free / (1024 ** 3)
        used_gb = used / (1024 ** 3)
        total_gb = total / (1024 ** 3)
        
        logger.info(
            f"Disk usage: {used_gb:.2f}/{total_gb:.2f} GB used, "
            f"{free_gb:.2f} GB free ({(free/total)*100:.1f}% free)"
        )
        
        # Log cache directory sizes
        for subdir in ['staging', 'docs', 'jobs']:
            subdir_path = Path(cache_dir) / subdir
            if subdir_path.exists():
                size = get_directory_size(str(subdir_path))
                size_mb = size / (1024 ** 2)
                logger.info(f"{subdir} directory size: {size_mb:.2f} MB")
    except Exception as e:
        logger.error(f"Error logging resource usage: {e}")
