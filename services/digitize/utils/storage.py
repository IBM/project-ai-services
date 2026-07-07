"""
File storage utilities for document caching.

Provides a ``StorageManager`` class that consolidates all file-system
operations (staging, content read/write, cleanup) that were previously
scattered across ``digitize_utils.py``.
"""

import asyncio
import json
import shutil
from functools import partial
from pathlib import Path

from common.misc_utils import get_logger, cleanup_staging_directory
from digitize.models import DocumentContentResponse, OutputFormat
from digitize.settings import settings

logger = get_logger("storage")


class StorageManager:
    """
    Manages file storage operations for the digitize service.

    All path construction is centralised here so that the rest of the
    codebase only calls high-level methods and never builds paths itself.

    Directories used:
    - ``staging_dir``: temporary upload area, one sub-directory per job
    - ``digitized_docs_dir``: permanent content files, one file per document
    """

    # ------------------------------------------------------------------ #
    # Staging                                                              #
    # ------------------------------------------------------------------ #

    async def stage_upload_files(
        self,
        job_id: str,
        filenames: list[str],
        file_contents: list[bytes],
    ) -> None:
        """
        Write uploaded file bytes to the per-job staging directory.

        Args:
            job_id: Unique job identifier (used to name the staging sub-dir).
            filenames: Original filenames — one entry per file.
            file_contents: Raw bytes for each file.
        """
        staging_path = Path(settings.digitize.staging_dir) / job_id
        staging_path.mkdir(parents=True, exist_ok=True)

        loop = asyncio.get_running_loop()

        for filename, content in zip(filenames, file_contents):
            target = staging_path / filename
            try:
                await loop.run_in_executor(
                    None,
                    partial(self._write_bytes, target, content),
                )
                logger.debug(f"Staged file: {filename} for job {job_id}")
            except PermissionError as exc:
                logger.error(
                    f"Permission denied staging {filename} (job {job_id}): {exc}"
                )
                raise
            except Exception as exc:
                logger.error(
                    f"Unexpected error staging {filename} (job {job_id}): {exc}"
                )
                raise

    @staticmethod
    def _write_bytes(path: Path, content: bytes) -> None:
        with open(path, "wb") as fh:
            fh.write(content)

    def cleanup_staging(self, job_id: str) -> None:
        """
        Remove the staging directory for *job_id*.

        Args:
            job_id: Unique job identifier.
        """
        cleanup_staging_directory(job_id, settings.digitize.staging_dir)

    # ------------------------------------------------------------------ #
    # Content read / write                                                 #
    # ------------------------------------------------------------------ #

    def get_content_path(self, doc_id: str, output_format: str) -> Path:
        """
        Return the path to the digitized content file for *doc_id*.

        Args:
            doc_id: Unique document identifier.
            output_format: File extension (``json``, ``md``, or ``txt``).

        Returns:
            :class:`pathlib.Path` to the content file.
        """
        return settings.digitize.digitized_docs_dir / f"{doc_id}.{output_format}"

    def read_document_content(self, doc_id: str, output_format: str) -> DocumentContentResponse:
        """
        Read the digitized content of *doc_id* from the cache.

        Args:
            doc_id: Unique document identifier.
            output_format: Expected format (``json``, ``md``, or ``txt``).

        Returns:
            :class:`~digitize.models.DocumentContentResponse`

        Raises:
            FileNotFoundError: Content file does not exist.
            json.JSONDecodeError: Content file is malformed JSON.
        """
        content_file = self.get_content_path(doc_id, output_format)

        if not content_file.exists():
            logger.error(f"Content file not found: {content_file}")
            raise FileNotFoundError(
                f"Content file for document '{doc_id}' not found"
            )

        try:
            with open(content_file, "r", encoding="utf-8") as fh:
                if output_format == OutputFormat.JSON.value:
                    content_data = json.load(fh)
                else:
                    content_data = fh.read()
        except json.JSONDecodeError:
            logger.error(
                f"Failed to parse JSON content file for document {doc_id}"
            )
            raise
        except Exception as exc:
            logger.error(f"Failed to read content file for document {doc_id}: {exc}")
            raise

        logger.debug(
            f"Retrieved content for document {doc_id} in {output_format} format"
        )
        return DocumentContentResponse(result=content_data, output_format=output_format)

    # ------------------------------------------------------------------ #
    # Deletion                                                             #
    # ------------------------------------------------------------------ #

    def delete_document_content(self, doc_id: str, output_format: str) -> None:
        """
        Delete the digitized content file for *doc_id*.

        Args:
            doc_id: Unique document identifier.
            output_format: File extension (``json``, ``md``, or ``txt``).

        Raises:
            ValueError: *output_format* is not a valid :class:`~digitize.models.OutputFormat`.
            Exception: Deletion failed for an unexpected reason.
        """
        valid_formats = {fmt.value for fmt in OutputFormat}
        if output_format not in valid_formats:
            raise ValueError(
                f"Invalid output_format: '{output_format}'. "
                f"Must be one of: {', '.join(sorted(valid_formats))}"
            )

        content_file = self.get_content_path(doc_id, output_format)
        if content_file.exists():
            try:
                content_file.unlink()
                logger.info(f"Deleted content file for document {doc_id}")
            except Exception as exc:
                raise Exception(
                    f"Failed to delete content file {content_file}: {exc}"
                ) from exc
        else:
            logger.warning(
                f"Content file not found (may have been deleted already): {content_file}"
            )

    def delete_all_contents(self) -> dict:
        """
        Delete all digitized content files from the cache directory.

        Returns:
            Dictionary with ``content_files_deleted`` (int) and ``errors`` (list[str]).
        """
        stats: dict = {"content_files_deleted": 0, "errors": []}
        digitized_dir = settings.digitize.digitized_docs_dir

        if not digitized_dir.exists():
            logger.info(f"Digitized directory {digitized_dir} does not exist — nothing to clean")
            return stats

        try:
            file_count = sum(1 for p in digitized_dir.iterdir() if p.is_file())
            shutil.rmtree(digitized_dir)
            digitized_dir.mkdir(parents=True, exist_ok=True)
            stats["content_files_deleted"] = file_count
            logger.info(f"✅ Deleted {file_count} content file(s) from {digitized_dir}")
        except Exception as exc:
            msg = f"Failed to clean digitized directory: {exc}"
            logger.error(msg)
            stats["errors"].append(msg)

        return stats


# Module-level singleton.
storage_manager = StorageManager()
