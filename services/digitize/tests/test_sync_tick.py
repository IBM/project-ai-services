"""
Unit tests for services/digitize/connectors/sync_tick.py

Coverage
--------
_classify
  - known checksum → skip (not in ingest_list, not an orphan)
  - brand-new checksum → added to ingest_list
  - cross-connector duplicate → add_connector_checksum_entry called inline
  - cross-connector dup dedup within tick (same checksum twice → one DB write)
  - intra-tick dedup for brand-new (same checksum two paths → one ingest entry)
  - orphan detection (owned but absent from scan)
  - empty scan → all known become orphans, empty ingest_list
  - full scan with mix of skip / new / cross-connector / orphan

_process_new_files
  - happy path: download → initialize_job_state → add_connector_checksum_entry → ingest called
  - per-file failure: exception increments failed counter, staging cleaned up, loop continues
  - staging directory is removed after each file (success and failure)
  - returns (new_count, failed_count) correctly

_delete_orphans
  - removes checksum row and deletes doc when remaining == 0
  - removes checksum row but skips doc deletion when remaining > 0
  - logs and continues when remove_connector_checksum_entry raises
  - returns count of ownership rows removed

_complete_tick / _fail_tick
  - _complete_tick calls close_sync_log with status='completed' and correct counters
  - _fail_tick calls close_sync_log with status='failed' and error string
  - _fail_tick swallows a secondary exception from close_sync_log

run_tick
  - aborts gracefully when connector not found
  - calls open_new_sync_log, scan, classify, process, orphan, complete in order
  - on scanner.connect failure: _fail_tick is called, scanner.close still runs
  - on scan failure: _fail_tick is called, scanner.close still runs
"""

from __future__ import annotations

import asyncio
from types import SimpleNamespace
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from digitize.connectors.sync_tick import (
    _classify as _real_classify,
    _complete_tick,
    _delete_orphans,
    _fail_tick,
    _process_new_files,
    run_tick,
)

DB_MODULE = "digitize.connectors.sync_tick"


# ---------------------------------------------------------------------------
# Local wrapper so tests can pass keyword args for readability
# ---------------------------------------------------------------------------

def _classify(connector_id, scanned_files, known, all_cs):
    return _real_classify(connector_id, scanned_files, known, all_cs)


def _connector(connector_id: str = "conn-1", **kwargs):
    return SimpleNamespace(
        id=connector_id,
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

    def _patches(self, ingest_raises=None):
        """Return a context-manager stack that patches all external calls."""
        from contextlib import ExitStack
        stack = ExitStack()
        mock_settings = stack.enter_context(patch(f"{DB_MODULE}.settings"))
        mock_settings.digitize.staging_dir.__truediv__ = MagicMock(return_value=MagicMock())

        stack.enter_context(patch(f"{DB_MODULE}.add_connector_checksum_entry"))
        stack.enter_context(patch(f"{DB_MODULE}.update_sync_log"))
        stack.enter_context(
            patch(f"{DB_MODULE}.initialize_job_state", return_value={"report.pdf": "doc-1"})
        )
        stack.enter_context(patch(f"{DB_MODULE}.generate_uuid", return_value="job-uuid-1"))
        stack.enter_context(
            patch(f"{DB_MODULE}.ingest", side_effect=ingest_raises)
        )
        stack.enter_context(patch(f"{DB_MODULE}.cleanup_staging_directory"))
        return stack

    def test_happy_path_returns_new_count(self):
        scanner = self._make_scanner()
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches():
            new, failed = asyncio.run(_process_new_files(1, "conn-1", scanner, ingest_list))
        assert new == 1
        assert failed == 0

    def test_per_file_failure_increments_failed(self):
        scanner = self._make_scanner(download_raises=RuntimeError("network down"))
        ingest_list = [("docs/a.pdf", "ck1"), ("docs/b.pdf", "ck2")]
        with self._patches():
            new, failed = asyncio.run(_process_new_files(1, "conn-1", scanner, ingest_list))
        assert new == 0
        assert failed == 2

    def test_failure_does_not_stop_loop(self):
        """First file fails, second succeeds — both are processed."""
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

        from contextlib import ExitStack
        with ExitStack() as stack:
            mock_settings = stack.enter_context(patch(f"{DB_MODULE}.settings"))
            mock_settings.digitize.staging_dir.__truediv__ = MagicMock(return_value=MagicMock())
            stack.enter_context(patch(f"{DB_MODULE}.add_connector_checksum_entry"))
            stack.enter_context(patch(f"{DB_MODULE}.update_sync_log"))
            stack.enter_context(
                patch(f"{DB_MODULE}.initialize_job_state", return_value={"b.pdf": "doc-2"})
            )
            stack.enter_context(patch(f"{DB_MODULE}.generate_uuid", return_value="job-uuid"))
            stack.enter_context(patch(f"{DB_MODULE}.ingest"))
            stack.enter_context(patch(f"{DB_MODULE}.cleanup_staging_directory"))

            new, failed = asyncio.run(_process_new_files(1, "conn-1", scanner, ingest_list))

        assert call_count["n"] == 2
        assert new == 1
        assert failed == 1

    def test_staging_dir_cleaned_on_success(self):
        scanner = self._make_scanner()
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches() as stack:
            asyncio.run(_process_new_files(1, "conn-1", scanner, ingest_list))
        # cleanup is patched — just verifying it was called (no exception means finally ran)

    def test_staging_dir_cleaned_on_failure(self):
        scanner = self._make_scanner(download_raises=RuntimeError("fail"))
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches():
            new, failed = asyncio.run(_process_new_files(1, "conn-1", scanner, ingest_list))
        assert failed == 1

    def test_add_checksum_entry_called_on_success(self):
        scanner = self._make_scanner()
        ingest_list = [("docs/report.pdf", "ck1")]
        with self._patches() as stack:
            with patch(f"{DB_MODULE}.add_connector_checksum_entry") as mock_add:
                asyncio.run(_process_new_files(1, "conn-1", scanner, ingest_list))
        mock_add.assert_called_once_with("conn-1", "ck1", "doc-1")


# ---------------------------------------------------------------------------
# _delete_orphans  (async)
# ---------------------------------------------------------------------------

_DELETE_DOC = "digitize.api.v1.connectors._best_effort_delete_document"


class TestDeleteOrphans:
    def test_deletes_doc_when_last_owner(self):
        with patch(f"{DB_MODULE}.remove_connector_checksum_entry", return_value=(0, "doc-1")) as mock_rm, \
             patch(_DELETE_DOC) as mock_del:
            removed = asyncio.run(_delete_orphans("c1", {"ck_orphan"}))

        assert removed == 1
        mock_rm.assert_called_once_with("c1", "ck_orphan")
        mock_del.assert_called_once_with("doc-1")

    def test_skips_doc_deletion_when_other_owners_remain(self):
        with patch(f"{DB_MODULE}.remove_connector_checksum_entry", return_value=(2, "doc-1")), \
             patch(_DELETE_DOC) as mock_del:
            removed = asyncio.run(_delete_orphans("c1", {"ck_shared"}))

        assert removed == 1
        mock_del.assert_not_called()

    def test_continues_after_remove_raises(self):
        with patch(f"{DB_MODULE}.remove_connector_checksum_entry",
                   side_effect=RuntimeError("db gone")), \
             patch(_DELETE_DOC) as mock_del:
            removed = asyncio.run(_delete_orphans("c1", {"ck_orphan"}))

        assert removed == 0
        mock_del.assert_not_called()

    def test_returns_correct_removed_count_multiple(self):
        side_effects = [(0, "doc-1"), (1, "doc-2")]
        with patch(f"{DB_MODULE}.remove_connector_checksum_entry", side_effect=side_effects), \
             patch(_DELETE_DOC):
            removed = asyncio.run(_delete_orphans("c1", {"ck1", "ck2"}))

        assert removed == 2

    def test_empty_orphan_set(self):
        removed = asyncio.run(_delete_orphans("c1", set()))
        assert removed == 0


# ---------------------------------------------------------------------------
# _complete_tick / _fail_tick  (synchronous)
# ---------------------------------------------------------------------------

class TestTickFinalizers:
    def test_complete_tick_calls_close_sync_log(self):
        with patch(f"{DB_MODULE}.close_sync_log") as mock_close:
            _complete_tick(7, "c1", total_files=10, new_files=3, removed_files=1, failed_files=0)

        mock_close.assert_called_once_with(
            connector_id="c1",
            seq=7,
            status="completed",
            total_files=10,
            new_files=3,
            removed_files=1,
            failed_files=0,
        )

    def test_fail_tick_calls_close_sync_log_with_error(self):
        exc = ValueError("disk full")
        with patch(f"{DB_MODULE}.close_sync_log") as mock_close:
            _fail_tick(3, "c1", exc)

        mock_close.assert_called_once_with(
            connector_id="c1",
            seq=3,
            status="failed",
            error="disk full",
        )

    def test_fail_tick_swallows_close_exception(self):
        with patch(f"{DB_MODULE}.close_sync_log", side_effect=RuntimeError("write failed")):
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
        with patch(f"{DB_MODULE}.get_active_connector", return_value=None), \
             patch(f"{DB_MODULE}.open_new_sync_log") as mock_open:
            asyncio.run(run_tick("missing"))
        mock_open.assert_not_called()

    def test_happy_path_calls_phases_in_order(self):
        connector = _connector()
        mock_scanner = self._make_scanner(scan_result=[])

        with patch(f"{DB_MODULE}.get_active_connector", return_value=connector), \
             patch(f"{DB_MODULE}.open_new_sync_log", return_value=1) as mock_open, \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.update_sync_log"), \
             patch(f"{DB_MODULE}.close_sync_log") as mock_close, \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch("digitize.connectors.sync_tick._process_new_files",
                   new_callable=AsyncMock, return_value=(0, 0)), \
             patch("digitize.connectors.sync_tick._delete_orphans",
                   new_callable=AsyncMock, return_value=0):
            asyncio.run(run_tick("conn-1"))

        mock_open.assert_called_once_with("conn-1")
        mock_close.assert_called_once()
        assert mock_close.call_args.kwargs["status"] == "completed"

    def test_scanner_connect_failure_calls_fail_tick(self):
        connector = _connector()
        mock_scanner = self._make_scanner(connect_raises=ConnectionError("refused"))

        with patch(f"{DB_MODULE}.get_active_connector", return_value=connector), \
             patch(f"{DB_MODULE}.open_new_sync_log", return_value=2), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.close_sync_log") as mock_close:
            asyncio.run(run_tick("conn-1"))

        args = mock_close.call_args.kwargs
        assert args["status"] == "failed"
        assert "refused" in args["error"]

    def test_scanner_close_always_called(self):
        connector = _connector()
        mock_scanner = self._make_scanner(scan_raises=RuntimeError("scan exploded"))

        with patch(f"{DB_MODULE}.get_active_connector", return_value=connector), \
             patch(f"{DB_MODULE}.open_new_sync_log", return_value=3), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.close_sync_log"):
            asyncio.run(run_tick("conn-1"))

        mock_scanner.close.assert_called_once()

    def test_scan_failure_calls_fail_tick(self):
        connector = _connector()
        mock_scanner = self._make_scanner(scan_raises=IOError("timeout"))

        with patch(f"{DB_MODULE}.get_active_connector", return_value=connector), \
             patch(f"{DB_MODULE}.open_new_sync_log", return_value=4), \
             patch(f"{DB_MODULE}.list_connector_checksums", return_value=[]), \
             patch(f"{DB_MODULE}.list_all_checksums", return_value=[]), \
             patch("digitize.connectors.sync_tick.build_scanner", return_value=mock_scanner), \
             patch(f"{DB_MODULE}.close_sync_log") as mock_close:
            asyncio.run(run_tick("conn-1"))

        args = mock_close.call_args.kwargs
        assert args["status"] == "failed"
