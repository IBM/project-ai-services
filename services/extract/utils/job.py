"""
Utility functions for async extract-job management.

Includes directory setup, file staging, result file I/O, and the
zombie-job recovery scan run at service startup.
"""

import json
import shutil
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, Optional

from fastapi import UploadFile

from common.misc_utils import get_logger
from extract.settings import settings

logger = get_logger("job_utils")

ALLOWED_EXTENSIONS = {".txt", ".pdf"}


# ---------------------------------------------------------------------------
# Directory management
# ---------------------------------------------------------------------------

def ensure_directories() -> None:
    """Create staging and results directories if they do not exist."""
    for d in [settings.extract.staging_dir, settings.extract.results_dir]:
        d.mkdir(parents=True, exist_ok=True)


# ---------------------------------------------------------------------------
# File helpers
# ---------------------------------------------------------------------------

def validate_file_extension(filename: str) -> tuple[bool, Optional[str]]:
    """Return (is_valid, extension_or_None)."""
    if not filename:
        return False, None
    import os
    ext = os.path.splitext(filename)[1].lower()
    return (ext in ALLOWED_EXTENSIONS), (ext if ext in ALLOWED_EXTENSIONS else None)


def stage_uploaded_file(job_id: str, file: UploadFile) -> Path:
    """
    Write the uploaded file to the staging directory for *job_id*.

    Returns the path to the staged file.
    Raises IOError on failure (caller should clean up and return 500).
    """
    job_dir = settings.extract.staging_dir / job_id
    job_dir.mkdir(parents=True, exist_ok=True)

    filename = file.filename or "uploaded_file"
    staged_path = job_dir / filename

    try:
        with open(staged_path, "wb") as fh:
            shutil.copyfileobj(file.file, fh)
        logger.info(f"Staged file for job {job_id}: {staged_path}")
        return staged_path
    except Exception as exc:
        shutil.rmtree(job_dir, ignore_errors=True)
        raise IOError(f"Failed to stage file for job {job_id}: {exc}") from exc


def read_result_file(job_id: str) -> Optional[Dict[str, Any]]:
    """Read and parse the result JSON file for *job_id*.  Returns None if absent."""
    path = settings.extract.results_dir / f"{job_id}_result.json"
    if not path.exists():
        return None
    try:
        with open(path, "r", encoding="utf-8") as fh:
            return json.load(fh)
    except Exception as exc:
        logger.error(f"Failed to read result file for job {job_id}: {exc}")
        return None


def delete_job_files(job_id: str) -> None:
    """Delete the result file and staging directory for *job_id*."""
    result_path = settings.extract.results_dir / f"{job_id}_result.json"
    if result_path.exists():
        try:
            result_path.unlink()
        except Exception as exc:
            logger.error(f"Failed to delete result file for job {job_id}: {exc}")

    staging_path = settings.extract.staging_dir / job_id
    if staging_path.exists():
        try:
            shutil.rmtree(staging_path, ignore_errors=True)
        except Exception as exc:
            logger.error(f"Failed to delete staging dir for job {job_id}: {exc}")


def delete_all_job_files() -> None:
    """Delete all result files and all staging directories."""
    results_dir = settings.extract.results_dir
    if results_dir.exists():
        for f in results_dir.glob("*_result.json"):
            try:
                f.unlink()
            except Exception as exc:
                logger.error(f"Failed to delete result file {f.name}: {exc}")

    staging_dir = settings.extract.staging_dir
    if staging_dir.exists():
        for d in staging_dir.iterdir():
            if d.is_dir():
                try:
                    shutil.rmtree(d, ignore_errors=True)
                except Exception as exc:
                    logger.error(f"Failed to delete staging dir {d.name}: {exc}")


# ---------------------------------------------------------------------------
# Zombie-job recovery
# ---------------------------------------------------------------------------

def recover_zombie_jobs() -> int:
    """
    Mark any ``accepted`` or ``in_progress`` job as ``failed`` after a restart.

    Called once during FastAPI startup.  Returns the number of recovered jobs.
    """
    from extract.db.manager import db_repo

    logger.info("Starting zombie job recovery scan...")
    try:
        zombies = db_repo.get_active_jobs()
        if not zombies:
            logger.info("No zombie jobs found")
            return 0

        count = 0
        for job in zombies:
            job_id = job.job_id
            logger.warning(f"Zombie job found: {job_id} (status={job.status})")
            db_repo.update_job(
                job_id=job_id,
                status="failed",
                error="System restarted during processing",
                completed_at=datetime.now(timezone.utc),
            )
            staging_path = settings.extract.staging_dir / job_id
            if staging_path.exists():
                shutil.rmtree(staging_path, ignore_errors=True)
            count += 1

        logger.info(f"Zombie recovery complete: {count} job(s) recovered")
        return count
    except Exception as exc:
        logger.error(f"Error during zombie recovery scan: {exc}", exc_info=True)
        return 0

# Made with Bob
