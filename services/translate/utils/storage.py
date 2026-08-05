"""
File storage utilities for the translate service.

Provides a ``StorageManager`` class that centralises all file-system
operations: staging uploaded files, writing result JSON files, reading
result files, and cleaning up directories.

Mirrors the pattern from ``digitize/utils/storage.py``.
"""

import asyncio
import json
import shutil
from functools import partial
from pathlib import Path
from typing import Any, Dict

from common.misc_utils import get_logger
from translate.settings import settings

logger = get_logger("storage")


class StorageManager:
    """
    Centralised file-system access for the translate service.

    Directories:
    - ``staging_dir``:  one sub-directory per job while it is being processed.
    - ``results_dir``:  one ``{job_id}_result.json`` per completed job.
    """

    # ------------------------------------------------------------------ #
    # Staging                                                              #
    # ------------------------------------------------------------------ #

    async def stage_upload_file(
        self,
        job_id: str,
        filename: str,
        content: bytes,
    ) -> Path:
        """
        Write an uploaded file to the per-job staging directory.

        Args:
            job_id: Unique job identifier.
            filename: Original filename of the upload.
            content: Raw file bytes.

        Returns:
            Path to the staged file.
        """
        staging_path = settings.translate.staging_dir / job_id
        staging_path.mkdir(parents=True, exist_ok=True)

        target = staging_path / filename
        loop = asyncio.get_running_loop()
        try:
            await loop.run_in_executor(
                None,
                partial(self._write_bytes, target, content),
            )
            logger.debug(f"Staged file '{filename}' for job {job_id}")
        except PermissionError as exc:
            logger.error(f"Permission denied staging '{filename}' (job {job_id}): {exc}")
            raise
        except Exception as exc:
            logger.error(f"Error staging '{filename}' (job {job_id}): {exc}")
            raise
        return target

    @staticmethod
    def _write_bytes(path: Path, content: bytes) -> None:
        with open(path, "wb") as fh:
            fh.write(content)

    def cleanup_staging(self, job_id: str) -> None:
        """Remove the staging directory for *job_id*."""
        staging_path = settings.translate.staging_dir / job_id
        if staging_path.exists():
            try:
                shutil.rmtree(staging_path)
                logger.debug(f"Removed staging dir for job {job_id}")
            except Exception as exc:
                logger.warning(f"Could not remove staging dir for {job_id}: {exc}")

    # ------------------------------------------------------------------ #
    # Results                                                              #
    # ------------------------------------------------------------------ #

    def result_path(self, job_id: str) -> Path:
        """Return the path of the result JSON file for *job_id*."""
        return settings.translate.results_dir / f"{job_id}_result.json"

    def write_result(self, job_id: str, payload: Dict[str, Any]) -> None:
        """
        Write the translation result to disk.

        Args:
            job_id: Unique job identifier.
            payload: Serialisable dict (data, meta, usage).
        """
        path = self.result_path(job_id)
        path.parent.mkdir(parents=True, exist_ok=True)
        with open(path, "w", encoding="utf-8") as fh:
            json.dump(payload, fh, ensure_ascii=False)
        logger.debug(f"Wrote result file for job {job_id}")

    def read_result(self, job_id: str) -> Dict[str, Any]:
        """
        Read the translation result from disk.

        Args:
            job_id: Unique job identifier.

        Returns:
            Deserialised result dict.

        Raises:
            FileNotFoundError: Result file does not exist.
        """
        path = self.result_path(job_id)
        if not path.exists():
            raise FileNotFoundError(f"Result file for job '{job_id}' not found")
        with open(path, "r", encoding="utf-8") as fh:
            return json.load(fh)

    def delete_result(self, job_id: str) -> None:
        """Delete the result file for *job_id* if it exists."""
        path = self.result_path(job_id)
        if path.exists():
            path.unlink()
            logger.debug(f"Deleted result file for job {job_id}")


# Module-level singleton
storage_manager = StorageManager()

# Made with Bob
