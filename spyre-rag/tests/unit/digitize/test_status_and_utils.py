import asyncio
import json
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

import digitize.digitize_utils as dg_util
import digitize.status as status_mod
from digitize.document import DocumentMetadata
from digitize.job import JobState
from digitize.types import DocStatus, JobStatus, OutputFormat


@pytest.fixture
def digitize_dirs(tmp_path, monkeypatch):
    docs_dir = tmp_path / "docs"
    jobs_dir = tmp_path / "jobs"
    digitized_dir = tmp_path / "digitized"
    staging_dir = tmp_path / "staging"

    for path in (docs_dir, jobs_dir, digitized_dir, staging_dir):
        path.mkdir(parents=True, exist_ok=True)

    fake_settings = SimpleNamespace(
        digitize=SimpleNamespace(
            docs_dir=docs_dir,
            jobs_dir=jobs_dir,
            digitized_docs_dir=digitized_dir,
            staging_dir=staging_dir,
            retry_max_attempts=3,
            retry_initial_delay=0.01,
            retry_backoff_multiplier=1,
        )
    )

    monkeypatch.setattr(status_mod, "settings", fake_settings)
    monkeypatch.setattr(dg_util, "settings", fake_settings)

    return {
        "docs_dir": docs_dir,
        "jobs_dir": jobs_dir,
        "digitized_dir": digitized_dir,
        "staging_dir": staging_dir,
    }


@pytest.mark.unit
class TestStatusHelpers:
    def test_get_utc_timestamp_format(self):
        value = status_mod.get_utc_timestamp()

        assert value.endswith("Z")
        assert "T" in value

    def test_create_initial_document_metadata_dict(self):
        value = status_mod.create_initial_document_metadata_dict()

        assert value == {
            "pages": 0,
            "tables": 0,
            "timing_in_secs": {
                "digitizing": None,
                "processing": None,
                "chunking": None,
                "indexing": None,
            },
        }

    def test_create_document_metadata_persists_file(self, digitize_dirs):
        doc = status_mod.create_document_metadata(
            doc_name="sample.pdf",
            doc_id="doc-1",
            job_id="job-1",
            output_format=OutputFormat.JSON,
            operation="digitization",
            submitted_at="2024-01-01T00:00:00Z",
            docs_dir=digitize_dirs["docs_dir"],
        )

        assert isinstance(doc, DocumentMetadata)
        saved = digitize_dirs["docs_dir"] / "doc-1_metadata.json"
        assert saved.exists()
        assert json.loads(saved.read_text())["job_id"] == "job-1"

    def test_create_job_state_persists_file(self, digitize_dirs):
        job = status_mod.create_job_state(
            job_id="job-1",
            operation="ingestion",
            submitted_at="2024-01-01T00:00:00Z",
            doc_id_dict={"a.pdf": "doc-1"},
            documents_info=["a.pdf"],
            jobs_dir=digitize_dirs["jobs_dir"],
            job_name="Batch One",
        )

        assert isinstance(job, JobState)
        saved = digitize_dirs["jobs_dir"] / "job-1_status.json"
        assert saved.exists()
        payload = json.loads(saved.read_text())
        assert payload["job_name"] == "Batch One"
        assert payload["stats"]["total_documents"] == 1

    def test_status_manager_update_doc_metadata(self, digitize_dirs):
        status_mod.create_document_metadata(
            "sample.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "digitization",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )

        manager = status_mod.StatusManager("job-1")
        manager.update_doc_metadata(
            "doc-1",
            {
                "status": DocStatus.COMPLETED,
                "pages": 3,
                "timing_in_secs": {"digitizing": 1.5},
                "completed_at": "2024-01-01T00:05:00Z",
            },
        )

        payload = json.loads((digitize_dirs["docs_dir"] / "doc-1_metadata.json").read_text())
        assert payload["status"] == "completed"
        assert payload["completed_at"] == "2024-01-01T00:05:00Z"
        assert payload["metadata"]["pages"] == 3
        assert payload["metadata"]["timing_in_secs"]["digitizing"] == 1.5

    def test_status_manager_update_job_progress(self, digitize_dirs):
        status_mod.create_job_state(
            "job-1",
            "digitization",
            "2024-01-01T00:00:00Z",
            {"sample.pdf": "doc-1"},
            ["sample.pdf"],
            digitize_dirs["jobs_dir"],
        )

        manager = status_mod.StatusManager("job-1")
        manager.update_job_progress("doc-1", DocStatus.COMPLETED, JobStatus.COMPLETED)

        payload = json.loads((digitize_dirs["jobs_dir"] / "job-1_status.json").read_text())
        assert payload["documents"][0]["status"] == "completed"
        assert payload["stats"]["completed"] == 1
        assert payload["status"] == "completed"
        assert payload["completed_at"]

    def test_status_manager_error_path_does_not_raise(self, digitize_dirs):
        manager = status_mod.StatusManager("missing-job")

        manager.update_job_progress("doc-1", DocStatus.FAILED, JobStatus.FAILED, error="boom")

        assert not (digitize_dirs["jobs_dir"] / "missing-job_status.json").exists()

    def test_atomic_write_json_writes_expected_payload(self, digitize_dirs):
        manager = status_mod.StatusManager("job-1")
        target = digitize_dirs["jobs_dir"] / "atomic.json"

        manager._atomic_write_json(target, {"ok": True})

        assert json.loads(target.read_text()) == {"ok": True}


@pytest.mark.unit
class TestDigitizeUtils:
    def test_generate_uuid_returns_string(self):
        generated = dg_util.generate_uuid()

        assert isinstance(generated, str)
        assert len(generated) == 36

    def test_get_all_document_ids_reads_directory(self, digitize_dirs):
        status_mod.create_document_metadata(
            "a.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "ingestion",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )
        status_mod.create_document_metadata(
            "b.pdf",
            "doc-2",
            "job-1",
            OutputFormat.JSON,
            "ingestion",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )

        result = dg_util.get_all_document_ids(digitize_dirs["docs_dir"])

        assert sorted(result) == ["doc-1", "doc-2"]

    def test_get_all_document_ids_handles_missing_directory(self, tmp_path):
        result = dg_util.get_all_document_ids(tmp_path / "missing")

        assert result == []

    def test_get_all_document_ids_skips_invalid_files(self, digitize_dirs):
        (digitize_dirs["docs_dir"] / "bad_metadata.json").write_text("{bad json")
        status_mod.create_document_metadata(
            "a.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "ingestion",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )

        result = dg_util.get_all_document_ids(digitize_dirs["docs_dir"])

        assert result == ["doc-1"]

    def test_initialize_job_state_creates_files(self, digitize_dirs, monkeypatch):
        values = iter(["doc-1", "doc-2"])
        monkeypatch.setattr(dg_util, "generate_uuid", lambda: next(values))

        result = dg_util.initialize_job_state(
            job_id="job-1",
            operation="ingestion",
            output_format=OutputFormat.JSON,
            documents_info=["a.pdf", "b.pdf"],
            job_name="Batch",
        )

        assert result == {"a.pdf": "doc-1", "b.pdf": "doc-2"}
        assert (digitize_dirs["docs_dir"] / "doc-1_metadata.json").exists()
        assert (digitize_dirs["jobs_dir"] / "job-1_status.json").exists()

    @pytest.mark.asyncio
    async def test_stage_upload_files_writes_files(self, digitize_dirs):
        await dg_util.stage_upload_files(
            job_id="job-1",
            files=["a.pdf", "b.pdf"],
            staging_dir=str(digitize_dirs["staging_dir"] / "job-1"),
            file_contents=[b"a", b"b"],
        )

        assert (digitize_dirs["staging_dir"] / "job-1" / "a.pdf").read_bytes() == b"a"
        assert (digitize_dirs["staging_dir"] / "job-1" / "b.pdf").read_bytes() == b"b"

    def test_read_job_file_valid_invalid_and_missing(self, digitize_dirs):
        valid_file = digitize_dirs["jobs_dir"] / "job-1_status.json"
        valid_file.write_text(
            json.dumps(
                {
                    "job_id": "job-1",
                    "operation": "ingestion",
                    "status": "accepted",
                    "submitted_at": "2024-01-01T00:00:00Z",
                    "documents": [],
                    "stats": {
                        "total_documents": 0,
                        "completed": 0,
                        "failed": 0,
                        "in_progress": 0,
                    },
                }
            )
        )
        invalid_file = digitize_dirs["jobs_dir"] / "bad_status.json"
        invalid_file.write_text("{bad")

        assert dg_util.read_job_file(valid_file).job_id == "job-1"
        assert dg_util.read_job_file(invalid_file) is None
        assert dg_util.read_job_file(digitize_dirs["jobs_dir"] / "missing_status.json") is None

    def test_read_all_job_files_filters_invalid(self, digitize_dirs):
        (digitize_dirs["jobs_dir"] / "job-1_status.json").write_text(
            json.dumps(
                {
                    "job_id": "job-1",
                    "operation": "ingestion",
                    "status": "accepted",
                    "submitted_at": "2024-01-01T00:00:00Z",
                    "documents": [],
                    "stats": {
                        "total_documents": 0,
                        "completed": 0,
                        "failed": 0,
                        "in_progress": 0,
                    },
                }
            )
        )
        (digitize_dirs["jobs_dir"] / "bad_status.json").write_text("{bad")

        result = dg_util.read_all_job_files()

        assert len(result) == 1
        assert result[0].job_id == "job-1"

    def test_get_all_documents_filters_and_sorts(self, digitize_dirs):
        status_mod.create_document_metadata(
            "alpha.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "ingestion",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )
        status_mod.create_document_metadata(
            "beta.pdf",
            "doc-2",
            "job-1",
            OutputFormat.JSON,
            "ingestion",
            "2024-01-02T00:00:00Z",
            digitize_dirs["docs_dir"],
        )

        manager = status_mod.StatusManager("job-1")
        manager.update_doc_metadata("doc-2", {"status": DocStatus.COMPLETED})

        result = dg_util.get_all_documents(status_filter="completed", name_filter="beta", docs_dir=digitize_dirs["docs_dir"])

        assert len(result) == 1
        assert result[0].id == "doc-2"

    def test_get_document_by_id_with_and_without_details(self, digitize_dirs):
        status_mod.create_document_metadata(
            "alpha.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "digitization",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )

        with_details = dg_util.get_document_by_id("doc-1", include_details=True, docs_dir=digitize_dirs["docs_dir"])
        without_details = dg_util.get_document_by_id("doc-1", include_details=False, docs_dir=digitize_dirs["docs_dir"])

        assert with_details.metadata is not None
        assert without_details.metadata is None

    def test_get_document_content_for_json_and_md(self, digitize_dirs):
        status_mod.create_document_metadata(
            "alpha.pdf",
            "doc-json",
            "job-1",
            OutputFormat.JSON,
            "digitization",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )
        (digitize_dirs["digitized_dir"] / "doc-json.json").write_text(json.dumps({"title": "hello"}))

        status_mod.create_document_metadata(
            "beta.pdf",
            "doc-md",
            "job-1",
            "md",
            "digitization",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )
        (digitize_dirs["digitized_dir"] / "doc-md.md").write_text("# hello")

        json_result = dg_util.get_document_content("doc-json", docs_dir=digitize_dirs["docs_dir"])
        md_result = dg_util.get_document_content("doc-md", docs_dir=digitize_dirs["docs_dir"])

        assert json_result.result == {"title": "hello"}
        assert json_result.output_format == "json"
        assert md_result.result == "# hello"
        assert md_result.output_format == "md"

    def test_is_document_in_active_job(self, digitize_dirs):
        (digitize_dirs["jobs_dir"] / "job-1_status.json").write_text(
            json.dumps(
                {
                    "job_id": "job-1",
                    "operation": "ingestion",
                    "status": "in_progress",
                    "submitted_at": "2024-01-01T00:00:00Z",
                    "documents": [],
                    "stats": {
                        "total_documents": 0,
                        "completed": 0,
                        "failed": 0,
                        "in_progress": 0,
                    },
                }
            )
        )

        assert dg_util.is_document_in_active_job("doc-1", "job-1", digitize_dirs["jobs_dir"]) is True
        assert dg_util.is_document_in_active_job("doc-1", None, digitize_dirs["jobs_dir"]) is False

    def test_delete_document_files_deletes_content_first_then_metadata(self, digitize_dirs):
        status_mod.create_document_metadata(
            "alpha.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "digitization",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )
        content_file = digitize_dirs["digitized_dir"] / "doc-1.json"
        content_file.write_text("{}")

        dg_util.delete_document_files("doc-1", "json", digitize_dirs["docs_dir"])

        assert not content_file.exists()
        assert not (digitize_dirs["docs_dir"] / "doc-1_metadata.json").exists()

    def test_has_active_jobs_with_operation_filter(self, digitize_dirs):
        (digitize_dirs["jobs_dir"] / "job-1_status.json").write_text(
            json.dumps(
                {
                    "job_id": "job-1",
                    "operation": "ingestion",
                    "status": "accepted",
                    "submitted_at": "2024-01-01T00:00:00Z",
                    "documents": [],
                    "stats": {
                        "total_documents": 0,
                        "completed": 0,
                        "failed": 0,
                        "in_progress": 0,
                    },
                }
            )
        )
        (digitize_dirs["jobs_dir"] / "job-2_status.json").write_text(
            json.dumps(
                {
                    "job_id": "job-2",
                    "operation": "digitization",
                    "status": "completed",
                    "submitted_at": "2024-01-01T00:00:00Z",
                    "documents": [],
                    "stats": {
                        "total_documents": 0,
                        "completed": 0,
                        "failed": 0,
                        "in_progress": 0,
                    },
                }
            )
        )

        assert dg_util.has_active_jobs("ingestion", digitize_dirs["jobs_dir"]) == (True, ["job-1"])
        assert dg_util.has_active_jobs("digitization", digitize_dirs["jobs_dir"]) == (False, [])

    def test_cleanup_digitized_files(self, digitize_dirs):
        (digitize_dirs["digitized_dir"] / "a.json").write_text("{}")
        (digitize_dirs["digitized_dir"] / "b.md").write_text("x")

        result = dg_util.cleanup_digitized_files()

        assert result["content_files_deleted"] == 2
        assert result["errors"] == []
        assert list(digitize_dirs["digitized_dir"].iterdir()) == []

    def test_bulk_delete_all_documents(self, digitize_dirs):
        status_mod.create_document_metadata(
            "alpha.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "digitization",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )
        (digitize_dirs["digitized_dir"] / "doc-1.json").write_text("{}")

        result = dg_util.bulk_delete_all_documents(digitize_dirs["docs_dir"])

        assert result["metadata_files_deleted"] == 1
        assert result["content_files_deleted"] == 1
        assert result["errors"] == []

    def test_scan_and_recover_orphan_jobs(self, digitize_dirs, monkeypatch):
        status_mod.create_document_metadata(
            "alpha.pdf",
            "doc-1",
            "job-1",
            OutputFormat.JSON,
            "ingestion",
            "2024-01-01T00:00:00Z",
            digitize_dirs["docs_dir"],
        )
        status_mod.create_job_state(
            "job-1",
            "ingestion",
            "2024-01-01T00:00:00Z",
            {"alpha.pdf": "doc-1"},
            ["alpha.pdf"],
            digitize_dirs["jobs_dir"],
        )

        fake_doc_utils = SimpleNamespace(clean_intermediate_files=Mock())
        fake_config = SimpleNamespace(settings=dg_util.settings)
        monkeypatch.setitem(__import__("sys").modules, "digitize.doc_utils", fake_doc_utils)
        monkeypatch.setitem(__import__("sys").modules, "digitize.settings", fake_config)

        result = dg_util.scan_and_recover_orphan_jobs(digitize_dirs["jobs_dir"])

        assert result == 1
        job_payload = json.loads((digitize_dirs["jobs_dir"] / "job-1_status.json").read_text())
        doc_payload = json.loads((digitize_dirs["docs_dir"] / "doc-1_metadata.json").read_text())
        assert job_payload["status"] == "failed"
        assert doc_payload["status"] == "failed"

    def test_cleanup_staging_directory(self, digitize_dirs):
        job_stage = digitize_dirs["staging_dir"] / "job-1"
        job_stage.mkdir()
        (job_stage / "a.pdf").write_text("x")

        assert dg_util.cleanup_staging_directory("job-1", digitize_dirs["staging_dir"]) is True
        assert not job_stage.exists()
        assert dg_util.cleanup_staging_directory("job-1", digitize_dirs["staging_dir"]) is True

# Made with Bob
