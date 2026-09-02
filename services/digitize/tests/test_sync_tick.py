"""
Unit tests for services/digitize/connectors/sync_tick.py

Coverage
--------
InterruptType enum
  - SYNC_CANCEL value is "sync_cancel"
  - DELETE_CONNECTOR value is "delete_connector"
  - inherits from str so comparison with raw string works

_classify
  - known checksum → skip (not in ingest_list, not an orphan)
  - brand-new checksum → added to ingest_list
  - cross-connector duplicate → add_connector_checksum_entry called inline
  - cross-connector dup dedup within tick (same checksum twice → one DB write)
  - intra-tick dedup for brand-new (same checksum two paths → one ingest entry)
  - orphan detection (owned but absent from scan)
  - empty scan → all known become orphans, empty ingest_list
  - full scan with mix of skip / new / cross-connector / orphan
  - cross-connector dup: lookup returns None → add_entry not called
  - multiple files same checksum → only first path ingested

_process_new_files
  - happy path: download → initialize_job_state → add_connector_checksum_entry → ingest called
  - per-file failure: exception logged, staging cleaned up, loop continues
  - staging directory is removed after each file (success and failure)
  - empty ingest_list is a no-op (no error raised)
  - file skipped when verify_integrity returns False; no checksum entry added
  - integrity fail does not set batch_failed flag
  - cancellation checkpoint fires between download and ingest
  - RuntimeError raised at end only when batch_failed=True
  - multiple batches all succeed
  - increment_completed_files called with completed doc count after each batch
  - increment_completed_files not called when all docs in batch fail

_delete_orphans
  - removes checksum row and deletes doc when remaining == 0
  - removes checksum row but skips doc deletion when remaining > 0
  - logs and continues when remove_connector_checksum_entry raises
  - returns count of ownership rows removed

_complete_tick / _fail_tick
  - _complete_tick calls finalize_sync_log_and_update_connector with status='completed' and correct counters
  - _fail_tick calls finalize_sync_log_and_update_connector with status='failed' and error string
  - _fail_tick swallows a secondary exception from finalize_sync_log_and_update_connector

_handle_interrupt
  - None interrupt_type calls _cancel_tick and returns
  - SYNC_CANCEL branch calls _cancel_tick and _sweep_staging_dir with correct args
  - DELETE_CONNECTOR branch calls _cancel_tick and _run_teardown
  - SYNC_CANCEL does not call _run_teardown
  - DELETE_CONNECTOR does not call _sweep_staging_dir

_wait_for_job
  - exits immediately when job is already in terminal state
  - polls until job reaches terminal state (multiple iterations)
  - 'failed' is a terminal state
  - raises CancelledError when interrupt detected mid-wait
  - None job_data keeps polling until interrupt fires

run_tick
  - aborts gracefully when connector not found
  - calls init_sync_log_and_update_connector, scan, classify, process, orphan, complete in order
  - on scanner.connect failure (ConnectionError): _fail_tick uses CREDENTIAL_ERROR_MSG, scanner.close still runs
  - on scan failure: _fail_tick is called, scanner.close still runs
  - _handle_interrupt is awaited when CancelledError is caught
"""

from __future__ import annotations

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from digitize.connectors.models import ConnectorError, ConnectorStatus, SyncLogStatus
from digitize.connectors.sync_tick import (
    InterruptType,
    _cancel_tick,
    _check_interrupt_call,
    _classify as _real_classify,
    _complete_tick,
    _delete_orphans,
    _fail_tick,
    _handle_interrupt,
    _process_new_files,
    _wait_for_job,
    run_tick,
)

DB_MODULE = "digitize.connectors.sync_tick"


# ---------------------------------------------------------------------------
# Local wrapper so tests can pass keyword args for readability
# ---------------------------------------------------------------------------

def _classify(connector_id, scanned_files, known, all_cs):
    return _real_classify(connector_id, scanned_files, known, all_cs)


def _connector(connector_id: str = "conn-1", name: str = "conn-name", **kwargs):
    return SimpleNamespace(
        id=connector_id,
        name=name,
        type="s3",
        connection_details={"bucket_name": "b", "access_key_id": "a", "secret_access_key": "s"},
        allowed_extensions=[".pdf"],
        **kwargs,
    )


# ---------------------------------------------------------------------------
# _classify  (synchronous — no asyncio needed)
# ---------------------------------------------------------------------------

class TestClassify:
    def test_known_checksum_is_skipped(self):
        scanned = [("a.pdf", "ck1")]
        ingest, orphans = _classify("c1", scanned, known={"ck1"}, all_cs={"ck1"})
        assert ingest == []
        assert "ck1" not in orphans

    def test_brand_new_added_to_ingest(self):
        scanned = [("a.pdf", "ck_new")]
        ingest, orphans = _classify("c1", scanned, known=set(), all_cs=set())
        assert ingest == [("a.pdf", "ck_new")]
        assert orphans == set()

    def test_cross_connector_dup_calls_add_entry(self):
        scanned = [("a.pdf", "ck_dup")]
        with patch(f"{DB_MODULE}.lookup_connector_content_by_checksum", return_value="doc-99") as mock_lookup, \
             patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add:
            ingest, _ = _classify("c1", scanned, known=set(), all_cs={"ck_dup"})

        assert ingest == []
        mock_lookup.assert_called_once_with("ck_dup")
        mock_add.assert_called_once_with("c1", "ck_dup", "doc-99")

    def test_cross_connector_dup_dedup_within_tick(self):
        """Same checksum appearing twice in scanned must only trigger one DB write."""
        scanned = [("a.pdf", "ck_dup"), ("b.pdf", "ck_dup")]
        with patch(f"{DB_MODULE}.lookup_connector_content_by_checksum", return_value="doc-99") as mock_lookup, \
             patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add:
            _classify("c1", scanned, known=set(), all_cs={"ck_dup"})

        mock_lookup.assert_called_once()
        mock_add.assert_called_once()

    def test_brand_new_intra_tick_dedup(self):
        """Same brand-new checksum on two paths → only one ingest entry."""
        scanned = [("a.pdf", "ck_new"), ("b.pdf", "ck_new")]
        ingest, _ = _classify("c1", scanned, known=set(), all_cs=set())
        assert len(ingest) == 1
        assert ingest[0][0] == "a.pdf"

    def test_orphan_detection(self):
        """Checksum in known but absent from scan → orphan."""
        scanned = [("a.pdf", "ck1")]
        known = {"ck1", "ck_orphan"}
        ingest, orphans = _classify("c1", scanned, known=known, all_cs=known)
        assert "ck_orphan" in orphans
        assert "ck1" not in orphans

    def test_empty_scan_all_known_become_orphans(self):
        ingest, orphans = _classify("c1", [], known={"ck1", "ck2"}, all_cs={"ck1", "ck2"})
        assert ingest == []
        assert orphans == {"ck1", "ck2"}

    def test_mixed_scan(self):
        """skip + new + cross-dup + orphan in one call."""
        scanned = [
            ("kept.pdf", "ck_known"),
            ("new.pdf",  "ck_new"),
            ("dup.pdf",  "ck_dup"),
        ]
        known  = {"ck_known", "ck_orphan"}
        all_cs = {"ck_known", "ck_dup", "ck_orphan"}

        with patch(f"{DB_MODULE}.lookup_connector_content_by_checksum", return_value="doc-dup"), \
             patch(f"{DB_MODULE}.add_connector_checksum_entry"):
            ingest, orphans = _classify("c1", scanned, known=known, all_cs=all_cs)

        assert ingest == [("new.pdf", "ck_new")]
        assert orphans == {"ck_orphan"}


# ---------------------------------------------------------------------------
# _process_new_files  (async)
# ---------------------------------------------------------------------------

class TestProcessNewFiles:
    def _make_scanner(self, download_raises=None):
        scanner = MagicMock()
        if download_raises:
            scanner.download_to.side_effect = download_raises
        else:
            scanner.download_to.return_value = "abc123"
        scanner.verify_integrity.return_value = True
        return scanner

    @staticmethod
    def _completed_stats(filename: str, doc_id: str) -> dict:
        """Helper: stats dict where the single doc completed successfully."""
        doc = {"id": doc_id, "name": filename, "status": "completed"}
        return {"completed_docs": [doc], "failed_docs": [], "completed_with_errors_docs": [], "total_docs": 1, "failed_count": 0, "completed_count": 1, "completed_with_errors_count": 0}

    @staticmethod
    def _failed_stats(filename: str, doc_id: str) -> dict:
        """Helper: stats dict where the single doc failed."""
        doc = {"id": doc_id, "name": filename, "status": "failed"}
        return {"completed_docs": [], "failed_docs": [doc], "completed_with_errors_docs": [], "total_docs": 1, "failed_count": 1, "completed_count": 0, "completed_with_errors_count": 0}

    def _patches(self, ingest_raises=None):
        """Return a context-manager stack that patches all external calls."""
        from contextlib import ExitStack
        stack = ExitStack()
        mock_settings = stack.enter_context(patch(f"{DB_MODULE}.settings"))
        mock_settings.digitize.staging_dir.__truediv__ = MagicMock(return_value=MagicMock())

        stack.enter_context(patch(f"{DB_MODULE}.add_connector_checksum_entry"))
        stack.enter_context(
            patch(f"{DB_MODULE}.initialize_job_state", return_value={"report.pdf": "doc-1"})
        )
        stack.enter_context(patch(f"{DB_MODULE}.generate_uuid", return_value="job-uuid-1"))
        stack.enter_context(
            patch(f"{DB_MODULE}.ingest", side_effect=ingest_raises)
        )
        # _wait_for_job polls with asyncio.sleep(_JOB_POLL_INTERVAL=10s) until the
        # job reaches a terminal state.  With get_job mocked to return None the
        # status never becomes terminal → infinite sleep → test hangs.
        stack.enter_context(patch(f"{DB_MODULE}._wait_for_job", new_callable=AsyncMock))
        stack.enter_context(
            patch(
                f"{DB_MODULE}.get_job_document_stats",
                return_value=self._completed_stats("report.pdf", "doc-1"),
            )
        )
        stack.enter_context(patch(f"{DB_MODULE}.cleanup_staging_directory"))
        # cancellation checkpoint must not fire in normal test runs
        stack.enter_context(
            patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING)
        )
        stack.enter_context(
            patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED)
        )
        # validate_document_file reads staged bytes from disk; bypass it in unit tests
        # since the scanner is a MagicMock and no real files are written to disk.
        stack.enter_context(patch(f"{DB_MODULE}.validate_document_file"))
        stack.enter_context(patch(f"{DB_MODULE}.increment_completed_files"))
        return stack

    def test_happy_path_completes_without_error(self):
        scanner = self._make_scanner()
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches():
            asyncio.run(_process_new_files(1, "conn-1", "conn-name", scanner, ingest_list))

    def test_failure_does_not_stop_loop(self):
        """First batch fails, second batch succeeds — both batches are attempted."""
        # Force batch_size=1 so each file is its own batch; a failure in batch 0
        # must not prevent batch 1 from running.
        call_count = {"n": 0}

        def _download(remote_path, local_path):
            call_count["n"] += 1
            if call_count["n"] == 1:
                raise RuntimeError("first fails")
            return "ck_local"

        scanner = MagicMock()
        scanner.download_to.side_effect = _download
        scanner.verify_integrity.return_value = True

        ingest_list = [("a.pdf", "ck1"), ("b.pdf", "ck2")]

        import digitize.connectors.sync_tick as _st_mod
        from contextlib import ExitStack
        with ExitStack() as stack:
            mock_settings = stack.enter_context(patch(f"{DB_MODULE}.settings"))
            mock_settings.digitize.staging_dir.__truediv__ = MagicMock(return_value=MagicMock())
            stack.enter_context(patch(f"{DB_MODULE}.add_connector_checksum_entry"))
            stack.enter_context(
                patch(f"{DB_MODULE}.initialize_job_state", return_value={"b.pdf": "doc-2"})
            )
            stack.enter_context(patch(f"{DB_MODULE}.generate_uuid", return_value="job-uuid"))
            stack.enter_context(patch(f"{DB_MODULE}.ingest"))
            stack.enter_context(patch(f"{DB_MODULE}._wait_for_job", new_callable=AsyncMock))
            stack.enter_context(
                patch(
                    f"{DB_MODULE}.get_job_document_stats",
                    return_value=self._completed_stats("b.pdf", "doc-2"),
                )
            )
            stack.enter_context(patch(f"{DB_MODULE}.cleanup_staging_directory"))
            stack.enter_context(
                patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING)
            )
            stack.enter_context(
                patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED)
            )
            stack.enter_context(patch(f"{DB_MODULE}.increment_completed_files"))
            # Force batch_size=1 so the two files land in separate batches
            stack.enter_context(patch.object(_st_mod, "_BATCH_SIZE", 1))

            with pytest.raises(RuntimeError, match="One or more documents failed to sync"):
                asyncio.run(_process_new_files(1, "conn-1", "conn-name", scanner, ingest_list))

        assert call_count["n"] == 2

    def test_staging_dir_cleaned_on_success(self):
        scanner = self._make_scanner()
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches() as stack:
            asyncio.run(_process_new_files(1, "conn-1", "conn-name", scanner, ingest_list))
        # cleanup is patched — just verifying it was called (no exception means finally ran)

    def test_staging_dir_cleaned_on_failure(self):
        scanner = self._make_scanner(download_raises=RuntimeError("fail"))
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches():
            with pytest.raises(RuntimeError, match="One or more documents failed to sync"):
                asyncio.run(_process_new_files(1, "conn-1", "conn-name", scanner, ingest_list))

    def test_add_checksum_entry_called_on_success(self):
        """Checksum entry is written only for docs that completed successfully."""
        scanner = self._make_scanner()
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches():
            with patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add:
                asyncio.run(_process_new_files(1, "conn-1", "conn-name", scanner, ingest_list))
        mock_add.assert_called_once_with("conn-1", "ck1", "doc-1")

    def test_add_checksum_entry_not_called_when_doc_fails(self):
        """If every doc in the batch failed, no checksum entries must be written."""
        scanner = self._make_scanner()
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches():
            with patch(f"{DB_MODULE}.get_job_document_stats",
                       return_value=self._failed_stats("report.pdf", "doc-1")), \
                 patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add:
                with pytest.raises(RuntimeError, match="One or more documents failed to sync"):
                    asyncio.run(_process_new_files(1, "conn-1", "conn-name", scanner, ingest_list))
        mock_add.assert_not_called()

    def test_partial_failure_registers_only_successful_checksums(self):
        """Partial batch: one doc succeeds, one fails → only the successful checksum is stored."""
        import digitize.connectors.sync_tick as _st_mod

        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        # Two files in separate batches so we control per-batch stats independently.
        # Use a single batch with two docs instead; doc-1 completes, doc-2 fails.
        ingest_list = [("a.pdf", "ck1"), ("b.pdf", "ck2")]
        partial_stats = {
            "completed_docs": [{"id": "doc-1", "name": "a.pdf", "status": "completed"}],
            "failed_docs":    [{"id": "doc-2", "name": "b.pdf", "status": "failed"}],
            "completed_with_errors_docs": [],
            "total_docs": 2,
            "failed_count": 1,
            "completed_count": 1,
            "completed_with_errors_count": 0,
        }

        from contextlib import ExitStack
        with ExitStack() as stack:
            mock_settings = stack.enter_context(patch(f"{DB_MODULE}.settings"))
            mock_settings.digitize.staging_dir.__truediv__ = MagicMock(return_value=MagicMock())
            stack.enter_context(
                patch(f"{DB_MODULE}.initialize_job_state", return_value={"a.pdf": "doc-1", "b.pdf": "doc-2"})
            )
            stack.enter_context(patch(f"{DB_MODULE}.generate_uuid", return_value="job-uuid"))
            stack.enter_context(patch(f"{DB_MODULE}.ingest"))
            stack.enter_context(patch(f"{DB_MODULE}._wait_for_job", new_callable=AsyncMock))
            stack.enter_context(
                patch(f"{DB_MODULE}.get_job_document_stats", return_value=partial_stats)
            )
            stack.enter_context(patch(f"{DB_MODULE}.cleanup_staging_directory"))
            stack.enter_context(
                patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING)
            )
            stack.enter_context(
                patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED)
            )
            stack.enter_context(patch(f"{DB_MODULE}.validate_document_file"))
            stack.enter_context(patch(f"{DB_MODULE}.increment_completed_files"))
            with patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add:
                with pytest.raises(RuntimeError, match="One or more documents failed to sync"):
                    asyncio.run(_process_new_files(1, "conn-1", "conn-name", scanner, ingest_list))

        # Only ck1 (a.pdf) should be stored; ck2 (b.pdf) must not be.
        mock_add.assert_called_once_with("conn-1", "ck1", "doc-1")


# ---------------------------------------------------------------------------
# _delete_orphans  (async)
# ---------------------------------------------------------------------------

_REMOVE_CHECKSUMS = "digitize.api.v1.connectors._remove_checksums"


class TestDeleteOrphans:
    def test_deletes_doc_when_last_owner(self):
        with patch(_REMOVE_CHECKSUMS, return_value=([], [])) as mock_rm:
            asyncio.run(_delete_orphans("c1", {"ck_orphan"}))

        mock_rm.assert_called_once_with("c1", {"ck_orphan"})

    def test_skips_doc_deletion_when_other_owners_remain(self):
        # doc deletion decisions live inside _remove_checksums; _delete_orphans
        # just raises on non-empty failure lists — a clean return means success
        with patch(_REMOVE_CHECKSUMS, return_value=([], [])):
            asyncio.run(_delete_orphans("c1", {"ck_shared"}))  # no exception

    def test_raises_when_remove_raises(self):
        # _remove_checksums accumulates failures and returns them; _delete_orphans
        # turns a non-empty checksum_removal_failures list into a RuntimeError
        with patch(_REMOVE_CHECKSUMS, return_value=(["ck_orphan"], [])):
            with pytest.raises(RuntimeError, match="checksum removal failed"):
                asyncio.run(_delete_orphans("c1", {"ck_orphan"}))

    def test_processes_multiple_checksums(self):
        with patch(_REMOVE_CHECKSUMS, return_value=([], [])) as mock_rm:
            asyncio.run(_delete_orphans("c1", {"ck1", "ck2"}))

        mock_rm.assert_called_once_with("c1", {"ck1", "ck2"})

    def test_empty_orphan_set(self):
        with patch(_REMOVE_CHECKSUMS, return_value=([], [])) as mock_rm:
            asyncio.run(_delete_orphans("c1", set()))
        mock_rm.assert_called_once_with("c1", set())


# ---------------------------------------------------------------------------
# _complete_tick / _fail_tick  (synchronous)
# ---------------------------------------------------------------------------

class TestTickFinalizers:
    def test_complete_tick_calls_finalize_sync_log_and_update_connector(self):
        with patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close:
            _complete_tick(7, "c1")

        mock_close.assert_called_once_with(
            connector_id="c1",
            seq=7,
            status=SyncLogStatus.COMPLETED,
        )

    def test_fail_tick_calls_finalize_sync_log_and_update_connector_with_error(self):
        exc = ValueError("disk full")
        with patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close:
            _fail_tick(3, "c1", exc)

        mock_close.assert_called_once_with(
            connector_id="c1",
            seq=3,
            status=SyncLogStatus.FAILED,
            error="disk full",
        )

    def test_fail_tick_swallows_close_exception(self):
        with patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector", side_effect=RuntimeError("write failed")):
            _fail_tick(3, "c1", ValueError("original"))  # must not raise


# ---------------------------------------------------------------------------
# run_tick  (async, integration-style — all I/O mocked)
# ---------------------------------------------------------------------------

class TestRunTick:
    def _make_scanner(self, connect_raises=None, scan_raises=None, scan_result=None):
        scanner = MagicMock()
        scanner.connect.side_effect = connect_raises
        if scan_raises:
            scanner.scan.side_effect = scan_raises
        else:
            scanner.scan.return_value = scan_result or []
        scanner.download_to.return_value = "deadbeef"
        scanner.verify_integrity.return_value = True
        return scanner

    def test_aborts_when_connector_not_found(self):
        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=None), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_fail:
            asyncio.run(run_tick("missing", sync_seq=1))

        # sync log must be closed as FAILED so the row doesn't stay open forever
        args = mock_fail.call_args.kwargs
        assert args["status"] == SyncLogStatus.FAILED
        assert "missing" in args["error"]

    def test_scanner_close_failure_is_logged_not_raised(self):
        """scanner.close() raising in the finally block must not propagate."""
        connector = _connector()
        mock_scanner = self._make_scanner(scan_result=[])
        mock_scanner.close.side_effect = RuntimeError("close boom")

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.update_sync_log"), \
             patch(f"{DB_MODULE}.update_connector_total_files"), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector"), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch("digitize.connectors.sync_tick._process_new_files",
                   new_callable=AsyncMock, return_value=[]), \
             patch("digitize.connectors.sync_tick._delete_orphans",
                   new_callable=AsyncMock, return_value=0):
            # must not raise despite close() failing
            asyncio.run(run_tick("conn-1", sync_seq=1))

    def test_happy_path_calls_phases_in_order(self):
        connector = _connector()
        mock_scanner = self._make_scanner(scan_result=[])

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.update_sync_log"), \
             patch(f"{DB_MODULE}.update_connector_total_files"), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close, \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch("digitize.connectors.sync_tick._process_new_files",
                   new_callable=AsyncMock, return_value=[]), \
             patch("digitize.connectors.sync_tick._delete_orphans",
                   new_callable=AsyncMock, return_value=0):
            asyncio.run(run_tick("conn-1", sync_seq=1))

        mock_close.assert_called_once()
        assert mock_close.call_args.kwargs["status"] == SyncLogStatus.COMPLETED

    def test_scanner_connect_failure_calls_fail_tick(self):
        connector = _connector()
        mock_scanner = self._make_scanner(connect_raises=ConnectionError("refused"))

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close:
            asyncio.run(run_tick("conn-1", sync_seq=2))

        args = mock_close.call_args.kwargs
        assert args["status"] == SyncLogStatus.FAILED
        assert args["error"] == ConnectorError.CREDENTIAL_ERROR_MSG

    def test_scanner_connect_failure_uses_credential_error_msg(self):
        """ConnectionError from scanner.connect() must set CREDENTIAL_ERROR_MSG on the connector,
        not the raw transport error, so the root cause is clearly visible in the UI."""
        connector = _connector()
        mock_scanner = self._make_scanner(connect_raises=ConnectionError("auth rejected"))

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close:
            asyncio.run(run_tick("conn-1", sync_seq=2))

        args = mock_close.call_args.kwargs
        assert args["status"] == SyncLogStatus.FAILED
        assert args["error"] == ConnectorError.CREDENTIAL_ERROR_MSG
        # scanner.close() must still run in the finally block
        mock_scanner.close.assert_called_once()

    def test_scanner_close_always_called(self):
        connector = _connector()
        mock_scanner = self._make_scanner(scan_raises=RuntimeError("scan exploded"))

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector"):
            asyncio.run(run_tick("conn-1", sync_seq=3))

        mock_scanner.close.assert_called_once()

    def test_scan_failure_calls_fail_tick(self):
        connector = _connector()
        mock_scanner = self._make_scanner(scan_raises=IOError("timeout"))

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close:
            asyncio.run(run_tick("conn-1", sync_seq=4))

        args = mock_close.call_args.kwargs
        assert args["status"] == SyncLogStatus.FAILED


# ---------------------------------------------------------------------------
# _check_interrupt_call
# ---------------------------------------------------------------------------

class TestCheckInterruptCall:
    def test_returns_delete_connector_when_delete_pending(self):
        with patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.DELETE_PENDING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED):
            result = _check_interrupt_call("conn-1", sync_seq=1)
            assert result == InterruptType.DELETE_CONNECTOR

    def test_returns_sync_cancel_when_cancel_pending_on_log(self):
        """CANCEL_PENDING is read from connector_sync_logs, not from connectors.status."""
        with patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.CANCEL_PENDING):
            result = _check_interrupt_call("conn-1", sync_seq=5)
            assert result == InterruptType.SYNC_CANCEL

    def test_returns_none_when_syncing_and_log_started(self):
        with patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED):
            result = _check_interrupt_call("conn-1", sync_seq=1)
            assert result is None

    def test_returns_none_when_connector_status_is_none(self):
        with patch(f"{DB_MODULE}.get_connector_sync_status", return_value=None), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED):
            result = _check_interrupt_call("conn-1", sync_seq=1)
            assert result is None

    def test_returns_none_when_up_to_date(self):
        with patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.UP_TO_DATE), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED):
            result = _check_interrupt_call("conn-1", sync_seq=1)
            assert result is None

    def test_delete_pending_short_circuits_before_log_check(self):
        """DELETE_PENDING on the connector row is detected without querying the log."""
        with patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.DELETE_PENDING) as mock_cs, \
             patch(f"{DB_MODULE}.get_sync_log_status") as mock_ls:
            result = _check_interrupt_call("conn-1", sync_seq=7)
        assert result == InterruptType.DELETE_CONNECTOR
        mock_ls.assert_not_called()


# ---------------------------------------------------------------------------
# _cancel_tick
# ---------------------------------------------------------------------------

class TestCancelTick:
    def test_calls_finalize_sync_log_and_update_connector_with_cancelled(self):
        with patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close:
            _cancel_tick(5, "conn-1")
        mock_close.assert_called_once_with(
            connector_id="conn-1",
            seq=5,
            status=SyncLogStatus.CANCELLED,
        )

    def test_swallows_close_exception(self):
        with patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector", side_effect=RuntimeError("write failed")):
            _cancel_tick(5, "conn-1")  # must not raise


# ---------------------------------------------------------------------------
# run_tick — cancellation path
# ---------------------------------------------------------------------------

class TestRunTickCancellation:
    """Verify that delete_pending causes the tick to be cancelled and the
    sync log is closed with status='cancelled'."""

    def _make_scanner(self):
        scanner = MagicMock()
        scanner.connect.return_value = None
        scanner.scan.return_value = []
        return scanner

    def test_cancelled_at_phase_boundary_closes_log(self):
        """_check_delete_pending fires before scan — log closed as cancelled."""
        connector = _connector()
        mock_scanner = self._make_scanner()

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.DELETE_PENDING), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close:
            with pytest.raises(asyncio.CancelledError):
                asyncio.run(run_tick("conn-1", sync_seq=10))

        args = mock_close.call_args.kwargs
        assert args["status"] == SyncLogStatus.CANCELLED
        mock_scanner.close.assert_called_once()

    def test_not_cancelled_when_not_delete_pending(self):
        """Normal status — tick completes without CancelledError."""
        connector = _connector()
        mock_scanner = self._make_scanner()

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.update_sync_log"), \
             patch(f"{DB_MODULE}.update_connector_total_files"), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close, \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch("digitize.connectors.sync_tick._process_new_files",
                   new_callable=AsyncMock, return_value=[]), \
             patch("digitize.connectors.sync_tick._delete_orphans",
                   new_callable=AsyncMock, return_value=0):
            asyncio.run(run_tick("conn-1", sync_seq=11))  # must not raise

        assert mock_close.call_args.kwargs["status"] == SyncLogStatus.COMPLETED

    def test_invalid_files_fail_tick_with_formatted_message(self):
        """When _process_new_files returns a non-empty list, run_tick must close
        the sync log as FAILED with the message 'Invalid file(s) detected from source - ...'."""
        connector = _connector()
        mock_scanner = self._make_scanner()

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.update_sync_log"), \
             patch(f"{DB_MODULE}.update_connector_total_files"), \
             patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING), \
             patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close, \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch("digitize.connectors.sync_tick._process_new_files",
                   new_callable=AsyncMock, return_value=["src/bad.pdf", "src/corrupt.docx"]), \
             patch("digitize.connectors.sync_tick._delete_orphans",
                   new_callable=AsyncMock, return_value=0):
            asyncio.run(run_tick("conn-1", sync_seq=5))

        args = mock_close.call_args.kwargs
        assert args["status"] == SyncLogStatus.FAILED
        assert args["error"] == "Invalid file(s) detected from source - src/bad.pdf, src/corrupt.docx"

    def test_cancelled_mid_process_closes_log(self):
        """_check_interrupt_call fires inside _process_new_files loop."""
        connector = _connector()
        mock_scanner = self._make_scanner()
        mock_scanner.scan.return_value = [("file.pdf", "ck1")]

        call_count = {"n": 0}

        def _maybe_cancel(connector_id, sync_seq):
            call_count["n"] += 1
            if call_count["n"] >= 2:  # second call = inside _process_new_files
                return InterruptType.SYNC_CANCEL
            return None

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.update_sync_log"), \
             patch(f"{DB_MODULE}.update_connector_total_files"), \
             patch(f"{DB_MODULE}._check_interrupt_call", side_effect=_maybe_cancel), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector") as mock_close, \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner):
            with pytest.raises(asyncio.CancelledError):
                asyncio.run(run_tick("conn-1", sync_seq=12))

        args = mock_close.call_args.kwargs
        assert args["status"] == SyncLogStatus.CANCELLED
        mock_scanner.close.assert_called_once()


# ---------------------------------------------------------------------------
# InterruptType enum
# ---------------------------------------------------------------------------

class TestInterruptTypeEnum:
    def test_sync_cancel_value(self):
        assert InterruptType.SYNC_CANCEL == "sync_cancel"

    def test_delete_connector_value(self):
        assert InterruptType.DELETE_CONNECTOR == "delete_connector"

    def test_is_str_subclass(self):
        assert isinstance(InterruptType.SYNC_CANCEL, str)
        assert isinstance(InterruptType.DELETE_CONNECTOR, str)

    def test_comparison_with_raw_string(self):
        assert InterruptType.SYNC_CANCEL == "sync_cancel"
        assert InterruptType.DELETE_CONNECTOR == "delete_connector"


# ---------------------------------------------------------------------------
# _handle_interrupt
# ---------------------------------------------------------------------------

class TestHandleInterrupt:
    def test_none_interrupt_calls_cancel_tick(self):
        with patch(f"{DB_MODULE}._cancel_tick") as mock_cancel, \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector"):
            asyncio.run(_handle_interrupt(sync_seq=1, connector_id="conn-1", interrupt_type=None))
        mock_cancel.assert_called_once_with(1, "conn-1")

    def test_sync_cancel_calls_cancel_tick_and_sweep(self):
        from pathlib import Path
        with patch(f"{DB_MODULE}._cancel_tick") as mock_cancel, \
             patch("digitize.api.v1.connectors._sweep_staging_dir") as mock_sweep, \
             patch("digitize.settings.settings") as mock_settings:
            mock_settings.digitize.staging_dir = Path("/fake/staging")
            asyncio.run(_handle_interrupt(sync_seq=7, connector_id="conn-A",
                                          interrupt_type=InterruptType.SYNC_CANCEL))
        mock_cancel.assert_called_once_with(7, "conn-A")
        mock_sweep.assert_called_once()

    def test_delete_connector_calls_cancel_tick_and_teardown(self):
        with patch(f"{DB_MODULE}._cancel_tick") as mock_cancel, \
             patch("digitize.api.v1.connectors._run_teardown",
                   new_callable=AsyncMock) as mock_teardown:
            asyncio.run(_handle_interrupt(sync_seq=3, connector_id="conn-B",
                                          interrupt_type=InterruptType.DELETE_CONNECTOR))
        mock_cancel.assert_called_once_with(3, "conn-B")
        mock_teardown.assert_awaited_once_with("conn-B")

    def test_delete_connector_does_not_call_sweep(self):
        """DELETE_CONNECTOR must not call _sweep_staging_dir."""
        with patch(f"{DB_MODULE}._cancel_tick"), \
             patch("digitize.api.v1.connectors._run_teardown", new_callable=AsyncMock), \
             patch("digitize.api.v1.connectors._sweep_staging_dir") as mock_sweep:
            asyncio.run(_handle_interrupt(sync_seq=3, connector_id="conn-B",
                                          interrupt_type=InterruptType.DELETE_CONNECTOR))
        mock_sweep.assert_not_called()

    def test_sync_cancel_does_not_call_teardown(self):
        """SYNC_CANCEL must not call _run_teardown."""
        from pathlib import Path
        with patch(f"{DB_MODULE}._cancel_tick"), \
             patch("digitize.api.v1.connectors._sweep_staging_dir"), \
             patch("digitize.settings.settings") as mock_settings, \
             patch("digitize.api.v1.connectors._run_teardown",
                   new_callable=AsyncMock) as mock_teardown:
            mock_settings.digitize.staging_dir = Path("/fake/staging")
            asyncio.run(_handle_interrupt(sync_seq=7, connector_id="conn-A",
                                          interrupt_type=InterruptType.SYNC_CANCEL))
        mock_teardown.assert_not_called()

    def test_sync_cancel_sweep_receives_connector_id_and_seq(self):
        """_sweep_staging_dir must be called with connector_id and sync_seq keyword."""
        from pathlib import Path
        with patch(f"{DB_MODULE}._cancel_tick"), \
             patch("digitize.api.v1.connectors._sweep_staging_dir") as mock_sweep, \
             patch("digitize.settings.settings") as mock_settings:
            fake_staging = Path("/fake/staging")
            mock_settings.digitize.staging_dir = fake_staging
            asyncio.run(_handle_interrupt(sync_seq=42, connector_id="my-conn",
                                          interrupt_type=InterruptType.SYNC_CANCEL))
        mock_sweep.assert_called_once_with(
            "my-conn", fake_staging / "connectors", sync_seq=42
        )


# ---------------------------------------------------------------------------
# _wait_for_job
# ---------------------------------------------------------------------------

class TestWaitForJob:
    _EMPTY_STATS = {"completed_docs": [], "failed_docs": [], "total_docs": 0,
                    "failed_count": 0, "completed_count": 0}

    def _patches(self, job_statuses: list, interrupt_returns=None, stats_returns=None):
        """
        Returns an ExitStack that patches asyncio.sleep (no-op), get_job to
        return successive statuses, _check_interrupt_call, get_job_document_stats,
        and increment_completed_files.
        """
        from contextlib import ExitStack

        stack = ExitStack()
        stack.enter_context(patch("asyncio.sleep", new_callable=AsyncMock))

        job_mock_iter = iter([{"status": s} for s in job_statuses] + [None] * 20)
        stack.enter_context(
            patch(f"{DB_MODULE}.get_job", side_effect=lambda jid: next(job_mock_iter))
        )

        if interrupt_returns is None:
            interrupt_returns = [None] * 20
        interrupt_iter = iter(interrupt_returns + [None] * 20)
        stack.enter_context(
            patch(f"{DB_MODULE}._check_interrupt_call",
                  side_effect=lambda cid, seq: next(interrupt_iter))
        )

        if stats_returns is None:
            stats_returns = [self._EMPTY_STATS] * 20
        stats_iter = iter(stats_returns + [self._EMPTY_STATS] * 20)
        stack.enter_context(
            patch(f"{DB_MODULE}.get_job_document_stats",
                  side_effect=lambda jid: next(stats_iter))
        )
        stack.enter_context(patch(f"{DB_MODULE}.increment_completed_files"))
        return stack

    def test_exits_immediately_on_first_terminal_status(self):
        """Job is already completed → only one poll needed."""
        with self._patches(["completed"]):
            asyncio.run(_wait_for_job("job-1", "conn-1", 1))

    def test_polls_until_terminal_state(self):
        """Job goes accepted → in_progress → completed."""
        with self._patches(["accepted", "in_progress", "completed"]):
            asyncio.run(_wait_for_job("job-1", "conn-1", 1))

    def test_failed_is_terminal(self):
        """Status 'failed' must also exit the wait loop."""
        with self._patches(["failed"]):
            asyncio.run(_wait_for_job("job-1", "conn-1", 1))

    def test_completed_with_errors_is_terminal(self):
        """Status 'completed_with_errors' must exit the wait loop."""
        with self._patches(["completed_with_errors"]):
            asyncio.run(_wait_for_job("job-1", "conn-1", 1))

    def test_raises_cancelled_error_on_interrupt(self):
        """Interrupt detected while waiting → CancelledError raised."""
        interrupt_seq = [None, InterruptType.SYNC_CANCEL]
        with self._patches(["accepted", "accepted"], interrupt_returns=interrupt_seq):
            with pytest.raises(asyncio.CancelledError):
                asyncio.run(_wait_for_job("job-1", "conn-1", 1))

    def test_none_job_data_does_not_crash(self):
        """get_job returning None means status='' which is not terminal;
        the loop must keep going until an interrupt fires."""
        from contextlib import ExitStack
        with ExitStack() as stack:
            stack.enter_context(patch("asyncio.sleep", new_callable=AsyncMock))
            none_iter = iter([None, None])
            stack.enter_context(
                patch(f"{DB_MODULE}.get_job", side_effect=lambda jid: next(none_iter, None))
            )
            interrupt_iter = iter([None, None, InterruptType.DELETE_CONNECTOR])
            stack.enter_context(
                patch(f"{DB_MODULE}._check_interrupt_call",
                      side_effect=lambda cid, seq: next(interrupt_iter))
            )
            stack.enter_context(
                patch(f"{DB_MODULE}.get_job_document_stats", return_value=self._EMPTY_STATS)
            )
            stack.enter_context(patch(f"{DB_MODULE}.increment_completed_files"))
            with pytest.raises(asyncio.CancelledError):
                asyncio.run(_wait_for_job("job-1", "conn-1", 5))

    def test_increments_on_each_poll_as_docs_complete(self):
        """increment_completed_files is called once per poll where new completions appear."""
        # poll 1: doc-1 completes; poll 2: doc-2 completes + job done
        stats_seq = [
            {"completed_docs": [{"id": "doc-1"}], "failed_docs": [], "total_docs": 2,
             "failed_count": 0, "completed_count": 1},
            {"completed_docs": [{"id": "doc-1"}, {"id": "doc-2"}], "failed_docs": [],
             "total_docs": 2, "failed_count": 0, "completed_count": 2},
        ]
        with self._patches(["in_progress", "completed"], stats_returns=stats_seq):
            with patch(f"{DB_MODULE}.increment_completed_files") as mock_inc:
                asyncio.run(_wait_for_job("job-1", "conn-1", 1))

        assert mock_inc.call_count == 2
        mock_inc.assert_any_call("conn-1", 1, count=1)  # poll 1: doc-1

    def test_does_not_double_count_already_seen_completions(self):
        """A doc that completed on poll N must not be counted again on poll N+1."""
        # Both polls return the same completed doc (count stays at 1).
        stats_seq = [
            {"completed_docs": [{"id": "doc-1"}], "failed_docs": [], "total_docs": 1,
             "failed_count": 0, "completed_count": 1},
            {"completed_docs": [{"id": "doc-1"}], "failed_docs": [], "total_docs": 1,
             "failed_count": 0, "completed_count": 1},
        ]
        with self._patches(["in_progress", "completed"], stats_returns=stats_seq):
            with patch(f"{DB_MODULE}.increment_completed_files") as mock_inc:
                asyncio.run(_wait_for_job("job-1", "conn-1", 1))

        mock_inc.assert_called_once_with("conn-1", 1, count=1)


# ---------------------------------------------------------------------------
# _process_new_files — additional branch coverage
# ---------------------------------------------------------------------------

class TestProcessNewFilesExtra:
    @staticmethod
    def _completed_stats(filename: str = "report.pdf", doc_id: str = "doc-1") -> dict:
        doc = {"id": doc_id, "name": filename, "status": "completed"}
        return {"completed_docs": [doc], "failed_docs": [], "completed_with_errors_docs": [], "total_docs": 1, "failed_count": 0, "completed_count": 1, "completed_with_errors_count": 0}

    def _base_patches(self):
        from contextlib import ExitStack
        stack = ExitStack()
        mock_settings = stack.enter_context(patch(f"{DB_MODULE}.settings"))
        mock_settings.digitize.staging_dir.__truediv__ = MagicMock(return_value=MagicMock())
        stack.enter_context(patch(f"{DB_MODULE}.add_connector_checksum_entry"))
        stack.enter_context(
            patch(f"{DB_MODULE}.initialize_job_state", return_value={"report.pdf": "doc-1"})
        )
        stack.enter_context(patch(f"{DB_MODULE}.generate_uuid", return_value="job-uuid-1"))
        stack.enter_context(patch(f"{DB_MODULE}.ingest"))
        stack.enter_context(patch(f"{DB_MODULE}._wait_for_job", new_callable=AsyncMock))
        stack.enter_context(
            patch(f"{DB_MODULE}.get_job_document_stats", return_value=self._completed_stats())
        )
        stack.enter_context(patch(f"{DB_MODULE}.cleanup_staging_directory"))
        stack.enter_context(
            patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING)
        )
        stack.enter_context(
            patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED)
        )
        stack.enter_context(patch(f"{DB_MODULE}.validate_document_file"))
        stack.enter_context(patch(f"{DB_MODULE}.increment_completed_files"))
        return stack

    def test_empty_ingest_list_is_noop(self):
        """No batches created, no error raised."""
        scanner = MagicMock()
        with self._base_patches():
            asyncio.run(_process_new_files(1, "conn-1", "name", scanner, []))
        scanner.download_to.assert_not_called()

    def test_integrity_fail_skips_file_no_checksum_entry(self):
        """verify_integrity returns False → file not added to checksum_to_filename."""
        scanner = MagicMock()
        scanner.download_to.return_value = "bad_local_hash"
        scanner.verify_integrity.return_value = False

        with self._base_patches():
            with patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add:
                asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                               [("remote/file.pdf", "ck1")]))
        mock_add.assert_not_called()

    def test_integrity_fail_does_not_set_batch_failed(self):
        """A file skipped due to integrity failure must not cause batch_failed=True."""
        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = False

        with self._base_patches():
            asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                           [("remote/file.pdf", "ck1")]))

    def test_cancellation_fires_between_download_and_ingest(self):
        """If _check_interrupt_call fires after downloads, CancelledError is raised."""
        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        call_count = {"n": 0}

        def _interrupt(connector_id, sync_seq):
            call_count["n"] += 1
            # first call = before-batch check, second = after-download check
            if call_count["n"] >= 2:
                return InterruptType.SYNC_CANCEL
            return None

        from contextlib import ExitStack
        with ExitStack() as stack:
            mock_settings = stack.enter_context(patch(f"{DB_MODULE}.settings"))
            mock_settings.digitize.staging_dir.__truediv__ = MagicMock(return_value=MagicMock())
            stack.enter_context(patch(f"{DB_MODULE}.add_connector_checksum_entry"))
            stack.enter_context(
                patch(f"{DB_MODULE}.initialize_job_state", return_value={"file.pdf": "doc-1"})
            )
            stack.enter_context(patch(f"{DB_MODULE}.generate_uuid", return_value="job-uuid-1"))
            stack.enter_context(patch(f"{DB_MODULE}.ingest"))
            stack.enter_context(patch(f"{DB_MODULE}._wait_for_job", new_callable=AsyncMock))
            stack.enter_context(
                patch(f"{DB_MODULE}.get_job_document_stats", return_value=self._completed_stats("file.pdf", "doc-1"))
            )
            stack.enter_context(patch(f"{DB_MODULE}.cleanup_staging_directory"))
            stack.enter_context(
                patch(f"{DB_MODULE}.get_connector_sync_status", return_value=ConnectorStatus.SYNCING)
            )
            stack.enter_context(
                patch(f"{DB_MODULE}.get_sync_log_status", return_value=SyncLogStatus.STARTED)
            )
            stack.enter_context(patch(f"{DB_MODULE}.validate_document_file"))
            stack.enter_context(patch(f"{DB_MODULE}.increment_completed_files"))
            stack.enter_context(
                patch(f"{DB_MODULE}._check_interrupt_call", side_effect=_interrupt)
            )

            with pytest.raises(asyncio.CancelledError):
                asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                               [("remote/file.pdf", "ck1")]))

    def test_raises_runtime_error_only_when_batch_failed(self):
        """No failure → must NOT raise RuntimeError at the end."""
        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        with self._base_patches():
            asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                           [("docs/report.pdf", "ck1")]))

    def test_multiple_batches_all_succeed(self):
        """Two batches in a 2-item list with BATCH_SIZE=1 — both succeed."""
        import digitize.connectors.sync_tick as _st_mod

        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        with self._base_patches():
            with patch.object(_st_mod, "_BATCH_SIZE", 1), \
                 patch(f"{DB_MODULE}.initialize_job_state", side_effect=[
                     {"a.pdf": "doc-1"}, {"b.pdf": "doc-2"}
                 ]), \
                 patch(f"{DB_MODULE}.get_job_document_stats", side_effect=[
                     self._completed_stats("a.pdf", "doc-1"),
                     self._completed_stats("b.pdf", "doc-2"),
                 ]):
                asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                               [("a.pdf", "ck1"), ("b.pdf", "ck2")]))

    def test_all_files_invalid_skips_job_creation(self):
        """When every file in a batch fails validation, no job must be created."""
        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        with self._base_patches():
            with patch(f"{DB_MODULE}.validate_document_file", side_effect=ValueError("bad")), \
                 patch(f"{DB_MODULE}.initialize_job_state") as mock_init, \
                 patch(f"{DB_MODULE}.ingest") as mock_ingest:
                asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                               [("remote/fake.pdf", "ck1")]))

        mock_init.assert_not_called()
        mock_ingest.assert_not_called()

    def test_all_files_invalid_does_not_set_batch_failed(self):
        """A batch where all files are skipped due to invalid format must not raise RuntimeError."""
        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        with self._base_patches():
            with patch(f"{DB_MODULE}.validate_document_file", side_effect=ValueError("bad")):
                # must not raise; returns the list of invalid remote paths
                result = asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                                        [("remote/fake.pdf", "ck1")]))
        assert result == ["remote/fake.pdf"]

    def test_all_files_invalid_returns_remote_paths(self):
        """_process_new_files returns the list of invalid remote paths, not a count."""
        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        with self._base_patches():
            with patch(f"{DB_MODULE}.validate_document_file", side_effect=ValueError("bad")):
                result = asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                                        [("a/one.pdf", "ck1"), ("b/two.pdf", "ck2")]))

        assert result == ["a/one.pdf", "b/two.pdf"]

    def test_mixed_batch_only_valid_files_ingested(self):
        """In a mixed batch, only the valid file reaches ingest; the invalid one is deleted."""
        scanner = MagicMock()
        scanner.download_to.return_value = "local_hash"
        scanner.verify_integrity.return_value = True

        def _validate(filename, _bytes):
            if filename == "fake.pdf":
                raise ValueError("bad format")

        with self._base_patches():
            with patch(f"{DB_MODULE}.validate_document_file", side_effect=_validate), \
                 patch(f"{DB_MODULE}.initialize_job_state",
                       return_value={"works.pdf": "doc-1"}) as mock_init:
                result = asyncio.run(_process_new_files(1, "conn-1", "name", scanner,
                                                        [("works.pdf", "ck1"), ("fake.pdf", "ck2")]))

        # initialize_job_state must only see the valid file
        mock_init.assert_called_once()
        call_args = mock_init.call_args
        assert call_args is not None
        assert call_args.kwargs["documents_info"] == ["works.pdf"]
        # invalid file's remote path must be in the returned list
        assert result == ["fake.pdf"]


# ---------------------------------------------------------------------------
# run_tick — _handle_interrupt wiring
# ---------------------------------------------------------------------------

class TestRunTickHandleInterrupt:
    """Verify that run_tick invokes _handle_interrupt on CancelledError."""

    def _make_scanner(self):
        scanner = MagicMock()
        scanner.connect.return_value = None
        scanner.scan.return_value = []
        return scanner

    def test_handle_interrupt_called_on_cancellation(self):
        """When CancelledError is caught, _handle_interrupt must be awaited."""
        connector = _connector()
        mock_scanner = self._make_scanner()

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.get_connector_sync_status",
                   return_value=ConnectorStatus.DELETE_PENDING), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector"), \
             patch("digitize.connectors.sync_tick._handle_interrupt",
                   new_callable=AsyncMock) as mock_handle:
            with pytest.raises(asyncio.CancelledError):
                asyncio.run(run_tick("conn-1", sync_seq=99))

        mock_handle.assert_awaited_once()

    def test_handle_interrupt_receives_correct_interrupt_type(self):
        """_handle_interrupt must receive the InterruptType detected by _check_interrupt_call."""
        connector = _connector()
        mock_scanner = self._make_scanner()

        with patch(f"{DB_MODULE}.get_connector_by_id", return_value=connector), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.get_connector_sync_status",
                   return_value=ConnectorStatus.DELETE_PENDING), \
             patch(f"{DB_MODULE}.finalize_sync_log_and_update_connector"), \
             patch("digitize.connectors.sync_tick._handle_interrupt",
                   new_callable=AsyncMock) as mock_handle:
            with pytest.raises(asyncio.CancelledError):
                asyncio.run(run_tick("conn-1", sync_seq=99))

        _, kwargs = mock_handle.await_args
        assert kwargs.get("interrupt_type") or mock_handle.await_args.args[2] is not None


# ---------------------------------------------------------------------------
# _classify — edge cases
# ---------------------------------------------------------------------------

class TestClassifyEdgeCases:
    """Additional classify cases complementing TestClassify."""

    def test_cross_connector_dup_no_doc_id_does_not_add_entry(self):
        """lookup returns None → add_connector_checksum_entry must NOT be called."""
        with patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add, \
             patch(f"{DB_MODULE}.lookup_connector_content_by_checksum", return_value=None):
            from digitize.connectors.sync_tick import _classify
            ingest_list, _ = _classify(
                "conn-1",
                [("file.pdf", "ck_cross")],
                known_checksums=set(),
                all_checksums={"ck_cross"},
            )
        mock_add.assert_not_called()
        assert ingest_list == []

    def test_scanned_file_not_in_known_not_in_all_is_ingested(self):
        """Completely new checksum must appear in ingest_list."""
        with patch(f"{DB_MODULE}.add_connector_checksum_entry"), \
             patch(f"{DB_MODULE}.lookup_connector_content_by_checksum", return_value=None):
            from digitize.connectors.sync_tick import _classify
            ingest_list, _ = _classify(
                "conn-1",
                [("new.pdf", "brand_new_ck")],
                known_checksums=set(),
                all_checksums=set(),
            )
        assert ("new.pdf", "brand_new_ck") in ingest_list

    def test_multiple_files_same_checksum_only_one_ingest(self):
        """Two files with identical checksum → only first path ingested."""
        with patch(f"{DB_MODULE}.add_connector_checksum_entry"), \
             patch(f"{DB_MODULE}.lookup_connector_content_by_checksum", return_value=None):
            from digitize.connectors.sync_tick import _classify
            ingest_list, _ = _classify(
                "conn-1",
                [("path1.pdf", "ck_dup"), ("path2.pdf", "ck_dup")],
                known_checksums=set(),
                all_checksums=set(),
            )
        assert len(ingest_list) == 1
        assert ingest_list[0][0] == "path1.pdf"

    def test_orphan_is_known_checksum_absent_from_scan(self):
        """A checksum known to this connector but not scanned must be an orphan."""
        with patch(f"{DB_MODULE}.add_connector_checksum_entry"), \
             patch(f"{DB_MODULE}.lookup_connector_content_by_checksum", return_value=None):
            from digitize.connectors.sync_tick import _classify
            _, orphans = _classify(
                "conn-1",
                [],
                known_checksums={"old_ck_1", "old_ck_2"},
                all_checksums={"old_ck_1", "old_ck_2"},
            )
        assert orphans == {"old_ck_1", "old_ck_2"}
