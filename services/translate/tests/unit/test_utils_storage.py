"""
Unit tests for translate/utils/storage.py — StorageManager.
"""

import json
import pytest
from pathlib import Path
from unittest.mock import MagicMock, patch, AsyncMock

from translate.utils.storage import StorageManager


@pytest.fixture
def manager(tmp_path):
    """Return a StorageManager with settings pointed at tmp_path."""
    mgr = StorageManager()
    with patch("translate.utils.storage.settings") as mock_settings:
        mock_settings.translate.staging_dir = tmp_path / "staging"
        mock_settings.translate.results_dir = tmp_path / "results"
        yield mgr, mock_settings, tmp_path


@pytest.mark.unit
class TestStorageManagerStaging:
    @pytest.mark.asyncio
    async def test_stage_upload_file_creates_file(self, tmp_path):
        mgr = StorageManager()
        staging_dir = tmp_path / "staging"
        results_dir = tmp_path / "results"

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.staging_dir = staging_dir
            mock_settings.translate.results_dir = results_dir

            job_id = "job-abc"
            content = b"Hello, world!"
            path = await mgr.stage_upload_file(job_id=job_id, filename="test.txt", content=content)

        assert path.exists()
        assert path.read_bytes() == content
        assert path.name == "test.txt"
        assert path.parent.name == job_id

    @pytest.mark.asyncio
    async def test_stage_upload_file_returns_path(self, tmp_path):
        mgr = StorageManager()
        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.staging_dir = tmp_path / "staging"
            job_id = "job-xyz"
            result = await mgr.stage_upload_file(job_id=job_id, filename="doc.md", content=b"# heading")

        assert isinstance(result, Path)
        assert result.name == "doc.md"

    def test_cleanup_staging_removes_directory(self, tmp_path):
        mgr = StorageManager()
        staging_dir = tmp_path / "staging"
        job_id = "job-cleanup"
        job_dir = staging_dir / job_id
        job_dir.mkdir(parents=True)
        (job_dir / "file.txt").write_text("content")

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.staging_dir = staging_dir
            mgr.cleanup_staging(job_id)

        assert not job_dir.exists()

    def test_cleanup_staging_nonexistent_directory_is_noop(self, tmp_path):
        mgr = StorageManager()
        staging_dir = tmp_path / "staging"

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.staging_dir = staging_dir
            # Should not raise
            mgr.cleanup_staging("nonexistent-job")


@pytest.mark.unit
class TestStorageManagerResults:
    def test_result_path_format(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "results"

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            path = mgr.result_path("job-123")

        assert path.name == "job-123_result.json"
        assert path.parent == results_dir

    def test_write_result_creates_json_file(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "results"
        payload = {"data": {"translation": "Hallo"}, "meta": {}, "usage": {}}

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            mgr.write_result("job-wr", payload)

        result_file = results_dir / "job-wr_result.json"
        assert result_file.exists()
        data = json.loads(result_file.read_text(encoding="utf-8"))
        assert data["data"]["translation"] == "Hallo"

    def test_read_result_returns_deserialized_dict(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "results"
        results_dir.mkdir()
        payload = {"data": {"translation": "Guten Tag"}, "meta": {"model": "m"}, "usage": {}}
        result_file = results_dir / "job-rr_result.json"
        result_file.write_text(json.dumps(payload), encoding="utf-8")

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            data = mgr.read_result("job-rr")

        assert data["data"]["translation"] == "Guten Tag"

    def test_read_result_raises_file_not_found(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "results"

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            with pytest.raises(FileNotFoundError):
                mgr.read_result("nonexistent-job")

    def test_delete_result_removes_file(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "results"
        results_dir.mkdir()
        result_file = results_dir / "job-del_result.json"
        result_file.write_text("{}", encoding="utf-8")

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            mgr.delete_result("job-del")

        assert not result_file.exists()

    def test_delete_result_nonexistent_is_noop(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "results"

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            # Should not raise
            mgr.delete_result("no-such-job")

    def test_write_result_creates_parent_dirs(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "deep" / "nested" / "results"

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            mgr.write_result("job-nested", {"data": {}, "meta": {}, "usage": {}})

        assert (results_dir / "job-nested_result.json").exists()

    def test_write_result_uses_utf8_encoding(self, tmp_path):
        mgr = StorageManager()
        results_dir = tmp_path / "results"
        payload = {"data": {"translation": "Ä Ö Ü ß é"}}

        with patch("translate.utils.storage.settings") as mock_settings:
            mock_settings.translate.results_dir = results_dir
            mgr.write_result("job-utf8", payload)

        result_file = results_dir / "job-utf8_result.json"
        raw = result_file.read_bytes()
        decoded = json.loads(raw.decode("utf-8"))
        assert decoded["data"]["translation"] == "Ä Ö Ü ß é"

# Made with Bob
