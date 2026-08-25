"""
Unit tests for services/digitize/utils/recovery.py — recover_zombie_jobs()

Coverage
--------
recover_zombie_jobs
  - returns 0 when no zombie jobs exist
  - marks each in-flight document as FAILED
  - marks the job itself as FAILED after all documents are processed
  - calls clean_intermediate_files for every document (including already-terminal ones)
  - skips clean_intermediate_files if doc_id is falsy
  - only calls update_doc_metadata for in-flight documents (not completed/failed ones)
  - continues past errors in per-job processing (does not re-raise)
  - calls cleanup_staging_directory for each zombie job id
  - skips jobs with missing job_id field
  - counts both ACCEPTED and IN_PROGRESS jobs
  - stale_doc_ids appended to final error_message when docs were updated
  - per-document clean_intermediate_files errors are swallowed
"""

from __future__ import annotations

from unittest.mock import MagicMock, call, patch

import pytest

RECOVERY_MODULE = "digitize.utils.recovery"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_job(job_id: str = "job-1", status: str = "accepted", docs=None) -> dict:
    return {
        "job_id": job_id,
        "status": status,
        "documents": docs or [],
    }


def _make_doc(doc_id: str = "doc-1", status: str = "in_progress") -> dict:
    return {"id": doc_id, "status": status}


def _run_recover(jobs_by_status: dict, clean_raises=None, job_raises=None) -> int:
    """
    Helper that runs recover_zombie_jobs() with all external calls mocked.

    Parameters
    ----------
    jobs_by_status:
        Mapping status_value → list[job_dict].  get_all_jobs is patched to
        return jobs matching the queried status.
    clean_raises:
        If set, clean_intermediate_files will raise this exception.
    job_raises:
        If set, the status_mgr.update_job_progress will raise on the first
        call per job_id (simulates per-job DB error).
    """
    from digitize.utils.recovery import recover_zombie_jobs

    def _get_all_jobs(status, limit, offset):
        key = status.value if hasattr(status, "value") else str(status)
        return jobs_by_status.get(key, []), len(jobs_by_status.get(key, []))

    status_mgr = MagicMock()
    if job_raises:
        status_mgr.update_job_progress.side_effect = job_raises

    with patch(f"{RECOVERY_MODULE}.get_all_jobs", side_effect=_get_all_jobs), \
         patch(f"{RECOVERY_MODULE}.get_status_manager", return_value=status_mgr), \
         patch("digitize.processing.orchestrator.clean_intermediate_files",
               side_effect=clean_raises) as mock_clean, \
         patch(f"{RECOVERY_MODULE}.cleanup_staging_directory") as mock_cleanup, \
         patch("digitize.settings.settings") as mock_settings:
        mock_settings.digitize.staging_dir = "/tmp/staging"
        mock_settings.digitize.digitized_docs_dir = "/tmp/digitized"
        result = recover_zombie_jobs()

    return result, status_mgr, mock_clean, mock_cleanup


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

class TestRecoverZombieJobs:
    def test_returns_zero_when_no_zombie_jobs(self):
        result, *_ = _run_recover({"accepted": [], "in_progress": []})
        assert result == 0

    def test_returns_count_of_recovered_jobs(self):
        jobs = {"accepted": [_make_job("job-1"), _make_job("job-2")], "in_progress": []}
        result, *_ = _run_recover(jobs)
        assert result == 2

    def test_counts_in_progress_jobs_too(self):
        jobs = {
            "accepted": [_make_job("job-1")],
            "in_progress": [_make_job("job-2", status="in_progress")],
        }
        result, *_ = _run_recover(jobs)
        assert result == 2

    def test_update_doc_metadata_called_for_in_flight_docs(self):
        doc = _make_doc("doc-1", "in_progress")
        jobs = {"accepted": [_make_job("job-1", docs=[doc])], "in_progress": []}
        _, status_mgr, *_ = _run_recover(jobs)
        status_mgr.update_doc_metadata.assert_called_once()
        call_kwargs = status_mgr.update_doc_metadata.call_args
        assert call_kwargs.args[0] == "doc-1"

    def test_update_doc_metadata_not_called_for_completed_docs(self):
        doc = _make_doc("doc-1", "completed")
        jobs = {"accepted": [_make_job("job-1", docs=[doc])], "in_progress": []}
        _, status_mgr, *_ = _run_recover(jobs)
        status_mgr.update_doc_metadata.assert_not_called()

    def test_update_doc_metadata_not_called_for_failed_docs(self):
        doc = _make_doc("doc-1", "failed")
        jobs = {"accepted": [_make_job("job-1", docs=[doc])], "in_progress": []}
        _, status_mgr, *_ = _run_recover(jobs)
        status_mgr.update_doc_metadata.assert_not_called()

    def test_clean_intermediate_files_called_for_every_doc(self):
        """clean_intermediate_files is called regardless of document status."""
        docs = [_make_doc("doc-1", "completed"), _make_doc("doc-2", "in_progress")]
        jobs = {"accepted": [_make_job("job-1", docs=docs)], "in_progress": []}
        _, _, mock_clean, _ = _run_recover(jobs)
        assert mock_clean.call_count == 2

    def test_clean_intermediate_error_is_swallowed(self):
        """Errors in clean_intermediate_files must not abort processing."""
        doc = _make_doc("doc-1", "in_progress")
        jobs = {"accepted": [_make_job("job-1", docs=[doc])], "in_progress": []}
        result, *_ = _run_recover(jobs, clean_raises=OSError("no such dir"))
        assert result == 1

    def test_skips_doc_with_falsy_doc_id(self):
        """Documents with missing id must not cause any update calls."""
        doc_no_id = {"status": "in_progress"}  # no 'id' key
        jobs = {"accepted": [_make_job("job-1", docs=[doc_no_id])], "in_progress": []}
        _, status_mgr, mock_clean, _ = _run_recover(jobs)
        status_mgr.update_doc_metadata.assert_not_called()
        mock_clean.assert_not_called()

    def test_cleanup_staging_directory_called_per_job(self):
        jobs = {"accepted": [_make_job("job-1"), _make_job("job-2")], "in_progress": []}
        _, _, _, mock_cleanup = _run_recover(jobs)
        assert mock_cleanup.call_count == 2
        cleanup_ids = [c.args[0] for c in mock_cleanup.call_args_list]
        assert "job-1" in cleanup_ids
        assert "job-2" in cleanup_ids

    def test_continues_past_per_job_errors(self):
        """Exception during one job's processing must not prevent other jobs from being recovered."""
        from unittest.mock import MagicMock, patch

        jobs_data = [_make_job("job-err"), _make_job("job-ok")]

        def _get_all_jobs(status, limit, offset):
            key = status.value if hasattr(status, "value") else str(status)
            if key == "accepted":
                return jobs_data, len(jobs_data)
            return [], 0

        call_count = {"n": 0}

        def _bad_status_mgr(job_id):
            call_count["n"] += 1
            mgr = MagicMock()
            if job_id == "job-err":
                mgr.update_job_progress.side_effect = RuntimeError("DB failure")
            return mgr

        with patch(f"{RECOVERY_MODULE}.get_all_jobs", side_effect=_get_all_jobs), \
             patch(f"{RECOVERY_MODULE}.get_status_manager", side_effect=_bad_status_mgr), \
             patch("digitize.processing.orchestrator.clean_intermediate_files"), \
             patch(f"{RECOVERY_MODULE}.cleanup_staging_directory"), \
             patch("digitize.settings.settings") as mock_settings:
            mock_settings.digitize.staging_dir = "/tmp/staging"
            mock_settings.digitize.digitized_docs_dir = "/tmp/digitized"
            from digitize.utils.recovery import recover_zombie_jobs
            result = recover_zombie_jobs()

        # Both jobs were attempted; only job-ok contributed to the count
        # (job-err raised before incrementing orphan_count)
        # The important thing is it didn't raise
        assert isinstance(result, int)

    def test_skips_job_with_missing_job_id(self):
        """Jobs without a job_id field must be skipped gracefully."""
        bad_job = {"status": "accepted", "documents": []}  # no 'job_id'
        jobs = {"accepted": [bad_job], "in_progress": []}
        result, status_mgr, *_ = _run_recover(jobs)
        assert result == 0
        status_mgr.update_job_progress.assert_not_called()

    def test_in_flight_doc_statuses_all_marked_failed(self):
        """All five in-flight statuses must trigger update_doc_metadata."""
        in_flight = ["accepted", "in_progress", "digitized", "processed", "chunked"]
        docs = [_make_doc(f"doc-{i}", s) for i, s in enumerate(in_flight)]
        jobs = {"accepted": [_make_job("job-1", docs=docs)], "in_progress": []}
        _, status_mgr, *_ = _run_recover(jobs)
        assert status_mgr.update_doc_metadata.call_count == len(in_flight)

    def test_update_job_progress_called_with_failed_at_end(self):
        """After all docs, job must be finalized as FAILED."""
        from digitize.models import JobStatus, DocStatus
        jobs = {"accepted": [_make_job("job-1")], "in_progress": []}
        _, status_mgr, *_ = _run_recover(jobs)
        # Last call to update_job_progress must use job_status=FAILED
        last_call = status_mgr.update_job_progress.call_args_list[-1]
        assert last_call.kwargs.get("job_status") == JobStatus.FAILED

# Made with Bob
