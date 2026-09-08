"""
Unit tests for translate/utils/jobs.py — job lifecycle utilities.
"""

import pytest
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

from translate.utils.jobs import (
    delete_job_files,
    generate_uuid,
    read_result_file,
    stage_uploaded_file,
)


@pytest.mark.unit
class TestGenerateUuid:
    def test_returns_string(self):
        result = generate_uuid()
        assert isinstance(result, str)

    def test_uuid_format(self):
        import uuid
        result = generate_uuid()
        # Should be parseable as a UUID
        parsed = uuid.UUID(result)
        assert str(parsed) == result

    def test_each_call_is_unique(self):
        ids = {generate_uuid() for _ in range(10)}
        assert len(ids) == 10


@pytest.mark.unit
class TestStageUploadedFile:
    @pytest.mark.asyncio
    async def test_delegates_to_storage_manager(self):
        mock_path = Path("/tmp/staging/job-1/file.txt")
        with patch("translate.utils.jobs.storage_manager") as mock_storage:
            mock_storage.stage_upload_file = AsyncMock(return_value=mock_path)
            result = await stage_uploaded_file("job-1", "file.txt", b"content")

        mock_storage.stage_upload_file.assert_called_once_with(
            job_id="job-1",
            filename="file.txt",
            content=b"content",
        )
        assert result == mock_path

    @pytest.mark.asyncio
    async def test_returns_path_object(self):
        mock_path = Path("/tmp/staging/job-abc/doc.md")
        with patch("translate.utils.jobs.storage_manager") as mock_storage:
            mock_storage.stage_upload_file = AsyncMock(return_value=mock_path)
            result = await stage_uploaded_file("job-abc", "doc.md", b"# Title")

        assert isinstance(result, Path)


@pytest.mark.unit
class TestReadResultFile:
    def test_delegates_to_storage_manager(self):
        expected = {"data": {"translation": "Hallo"}, "meta": {}, "usage": {}}
        with patch("translate.utils.jobs.storage_manager") as mock_storage:
            mock_storage.read_result.return_value = expected
            result = read_result_file("job-123")

        mock_storage.read_result.assert_called_once_with("job-123")
        assert result == expected

    def test_file_not_found_propagates(self):
        with patch("translate.utils.jobs.storage_manager") as mock_storage:
            mock_storage.read_result.side_effect = FileNotFoundError("not found")
            with pytest.raises(FileNotFoundError):
                read_result_file("missing-job")


@pytest.mark.unit
class TestDeleteJobFiles:
    def test_calls_cleanup_staging_and_delete_result(self):
        with patch("translate.utils.jobs.storage_manager") as mock_storage:
            mock_storage.cleanup_staging = MagicMock()
            mock_storage.delete_result = MagicMock()
            delete_job_files("job-del")

        mock_storage.cleanup_staging.assert_called_once_with("job-del")
        mock_storage.delete_result.assert_called_once_with("job-del")

    def test_delete_result_error_is_logged_not_raised(self):
        with patch("translate.utils.jobs.storage_manager") as mock_storage:
            mock_storage.cleanup_staging = MagicMock()
            mock_storage.delete_result.side_effect = OSError("disk error")
            # Must not raise
            delete_job_files("job-err")

        mock_storage.cleanup_staging.assert_called_once()

    def test_cleanup_staging_error_propagates(self):
        """cleanup_staging errors are not swallowed — only delete_result errors are."""
        with patch("translate.utils.jobs.storage_manager") as mock_storage:
            mock_storage.cleanup_staging.side_effect = PermissionError("denied")
            mock_storage.delete_result = MagicMock()
            with pytest.raises(PermissionError):
                delete_job_files("job-perm")

# Made with Bob
