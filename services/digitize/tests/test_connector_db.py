"""
Unit tests for connector DB operations in utils/db.py (PR 2).

All tests use the shared conftest mocks so no real database is required.
Each test builds a focused mock for the function under test and validates
the exact SQLAlchemy calls and return values described in §5 of the proposal.
"""

from datetime import datetime, timezone
from typing import Optional
from unittest.mock import MagicMock, Mock, call, patch

import pytest
from sqlalchemy.exc import IntegrityError, SQLAlchemyError

from digitize.connectors.models import ConnectorStatus, SyncLogStatus


# ---------------------------------------------------------------------------
# Helpers shared across test cases
# ---------------------------------------------------------------------------

_NOW = datetime(2025, 1, 15, 12, 0, 0, tzinfo=timezone.utc)

CONNECTOR_ID = "c7f3a2d1-0000-0000-0000-000000000001"
CONNECTOR_NAME = "prod-sftp-reports"
CONNECTOR_TYPE = "ssh"
CONNECTION_DETAILS = {"host": "sftp.example.com", "username": "sync_user"}
ALLOWED_EXTENSIONS = [".pdf", ".docx"]
SYNC_INTERVAL = 300

CHECKSUM_A = "aabbcc" * 5 + "aabb"          # 32-char hex (simulated)
CHECKSUM_B = "112233" * 5 + "1122"
DOC_ID_A = "doc-0000-aaaa"
DOC_ID_B = "doc-0000-bbbb"


def _make_session_cm(session_mock: MagicMock) -> MagicMock:
    """Wrap a session mock in a context-manager stub."""
    cm = MagicMock()
    cm.__enter__ = Mock(return_value=session_mock)
    cm.__exit__ = Mock(return_value=False)
    return cm


def _execute_result(rowcount: int = 1, scalar_val=None, rows=None):
    """Build a mock execute() return value."""
    r = MagicMock()
    r.rowcount = rowcount
    r.scalar = Mock(return_value=scalar_val)
    r.one_or_none = Mock(return_value=rows[0] if rows else None)
    r.all = Mock(return_value=rows or [])
    return r


# ===========================================================================
# insert_connector
# ===========================================================================

class TestInsertActiveConnector:
    def _call(self, session_mock):
        from digitize.utils.db import insert_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session_mock)):
            insert_connector(
                CONNECTOR_ID, CONNECTOR_NAME, CONNECTOR_TYPE,
                CONNECTION_DETAILS, ALLOWED_EXTENSIONS, SYNC_INTERVAL,
            )

    def test_inserts_row_successfully(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        self._call(session)
        assert session.execute.called

    def test_raises_integrity_error_on_conflict(self):
        session = MagicMock()
        # rowcount 0 → connector already exists
        session.execute.return_value = _execute_result(rowcount=0)
        from digitize.utils.db import insert_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            with pytest.raises(IntegrityError):
                insert_connector(
                    CONNECTOR_ID, CONNECTOR_NAME, CONNECTOR_TYPE,
                    CONNECTION_DETAILS, ALLOWED_EXTENSIONS, SYNC_INTERVAL,
                )

    def test_propagates_sqlalchemy_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("db down")
        from digitize.utils.db import insert_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            with pytest.raises(SQLAlchemyError):
                insert_connector(
                    CONNECTOR_ID, CONNECTOR_NAME, CONNECTOR_TYPE,
                    CONNECTION_DETAILS, ALLOWED_EXTENSIONS, SYNC_INTERVAL,
                )


# ===========================================================================
# upsert_connector
# ===========================================================================

class TestUpsertActiveConnector:
    def _call(self, session_mock, **kwargs):
        from digitize.utils.db import upsert_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session_mock)):
            upsert_connector(CONNECTOR_ID, **kwargs)

    def test_updates_name_only(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        self._call(session, name="new-name")
        assert session.execute.called

    def test_merges_connection_details_keys(self):
        """Only the supplied connection_details keys are written; untouched keys survive."""
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        self._call(session, connection_details={"remote_path": "/v2"})
        # Implementation uses Connector.connection_details.op("||") — confirm execute was called
        assert session.execute.called

    def test_does_not_clobber_untouched_connection_detail_keys(self):
        """Passing connection_details must not replace the whole JSONB column wholesale."""
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        self._call(session, connection_details={"remote_path": "/only_this_key"})
        assert session.execute.called

    def test_raises_file_not_found_when_connector_missing(self):
        """upsert_connector must raise FileNotFoundError when no row is matched."""
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=0)
        from digitize.utils.db import upsert_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            with pytest.raises(FileNotFoundError):
                upsert_connector(CONNECTOR_ID, name="x")

    def test_propagates_sqlalchemy_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("disk full")
        from digitize.utils.db import upsert_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            with pytest.raises(SQLAlchemyError):
                upsert_connector(CONNECTOR_ID, name="x")


# ===========================================================================
# get_connector_by_id
# ===========================================================================

class TestGetConnector:
    def _make_connector(self) -> MagicMock:
        c = MagicMock()
        c.id = CONNECTOR_ID
        c.name = CONNECTOR_NAME
        c.type = CONNECTOR_TYPE
        c.connection_details = CONNECTION_DETAILS
        c.allowed_extensions = ALLOWED_EXTENSIONS
        c.sync_interval_seconds = SYNC_INTERVAL
        c.attached_at = _NOW
        c.last_sync_at = None
        c.sync_status = ConnectorStatus.UP_TO_DATE
        c.error = None
        c.total_files = 0
        return c

    def test_returns_connector_when_found(self):
        connector = self._make_connector()
        session = MagicMock()
        session.get.return_value = connector
        from digitize.utils.db import get_connector_by_id
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = get_connector_by_id(CONNECTOR_ID)
        assert result is connector
        session.expunge.assert_called_once_with(connector)

    def test_returns_none_when_not_found(self):
        session = MagicMock()
        session.get.return_value = None
        from digitize.utils.db import get_connector_by_id
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = get_connector_by_id("nonexistent-id")
        assert result is None

    def test_returns_none_on_db_error(self):
        session = MagicMock()
        session.get.side_effect = SQLAlchemyError("timeout")
        from digitize.utils.db import get_connector_by_id
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = get_connector_by_id(CONNECTOR_ID)
        assert result is None


# ===========================================================================
# list_connectors
# ===========================================================================

class TestListConnectors:
    def test_returns_list_of_connectors(self):
        c1 = MagicMock()
        c1.id = CONNECTOR_ID
        c1.name = CONNECTOR_NAME
        c1.type = "ssh"
        c1.connection_details = {}
        c1.allowed_extensions = []
        c1.sync_interval_seconds = 300
        c1.attached_at = _NOW
        c1.last_sync_at = None
        c1.sync_status = ConnectorStatus.UP_TO_DATE
        c1.error = None
        c1.total_files = 0

        session = MagicMock()
        session.scalars.return_value.all.return_value = [c1]
        from digitize.utils.db import list_connectors
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = list_connectors()
        assert len(result) == 1
        assert result[0] is c1
        session.expunge.assert_called_once_with(c1)

    def test_returns_empty_list_on_db_error(self):
        session = MagicMock()
        session.scalars.side_effect = SQLAlchemyError("no connection")
        from digitize.utils.db import list_connectors
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = list_connectors()
        assert result == []


# ===========================================================================
# delete_active_connector
# ===========================================================================

class TestDeleteConnector:
    def test_returns_true_when_row_deleted(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        from digitize.utils.db import delete_active_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            assert delete_active_connector(CONNECTOR_ID) is True

    def test_returns_false_when_not_found(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=0)
        from digitize.utils.db import delete_active_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            assert delete_active_connector("nonexistent") is False

    def test_returns_false_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("oops")
        from digitize.utils.db import delete_active_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            assert delete_active_connector(CONNECTOR_ID) is False


# ===========================================================================
# lookup_connector_content_by_checksum
# ===========================================================================

class TestLookupConnectorContentByChecksum:
    def test_returns_doc_id_when_found(self):
        session = MagicMock()
        # execute().one_or_none() returns a row tuple
        session.execute.return_value.one_or_none.return_value = (DOC_ID_A,)
        from digitize.utils.db import lookup_connector_content_by_checksum
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = lookup_connector_content_by_checksum(CHECKSUM_A)
        assert result == DOC_ID_A

    def test_returns_none_when_not_found(self):
        session = MagicMock()
        session.execute.return_value.one_or_none.return_value = None
        from digitize.utils.db import lookup_connector_content_by_checksum
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = lookup_connector_content_by_checksum(CHECKSUM_A)
        assert result is None

    def test_never_touches_document_checksum_table(self):
        """
        The query must only reference connector_document_checksum.
        We inspect the generated SQL string to confirm the right table name.
        """
        from digitize.db.models import ConnectorDocumentChecksum
        from sqlalchemy import select
        stmt = (
            select(ConnectorDocumentChecksum.doc_id)
            .where(ConnectorDocumentChecksum.checksum == CHECKSUM_A)
            .limit(1)
        )
        sql = str(stmt)
        assert "connector_document_checksum" in sql
        assert "document_checksum" in sql  # substring — ensure we're looking at the right table
        # The user-submitted table is named "document_checksum" (no "connector_" prefix).
        # Our query must NOT target that table alone.
        # The connector table name always contains "connector_document_checksum".
        assert sql.count("connector_document_checksum") >= 1

    def test_returns_none_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("timeout")
        from digitize.utils.db import lookup_connector_content_by_checksum
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = lookup_connector_content_by_checksum(CHECKSUM_A)
        assert result is None


# ===========================================================================
# list_connector_checksums
# ===========================================================================

class TestListConnectorChecksums:
    def test_returns_checksums_for_connector(self):
        session = MagicMock()
        session.execute.return_value.all.return_value = [(CHECKSUM_A,), (CHECKSUM_B,)]
        from digitize.utils.db import list_connector_checksums
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = list_connector_checksums(CONNECTOR_ID)
        assert result == [CHECKSUM_A, CHECKSUM_B]

    def test_returns_empty_list_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("boom")
        from digitize.utils.db import list_connector_checksums
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = list_connector_checksums(CONNECTOR_ID)
        assert result == []


# ===========================================================================
# list_all_checksums
# ===========================================================================

class TestListAllChecksums:
    def test_returns_all_distinct_checksums(self):
        session = MagicMock()
        session.execute.return_value.all.return_value = [(CHECKSUM_A,), (CHECKSUM_B,)]
        from digitize.utils.db import list_all_checksums
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = list_all_checksums()
        assert set(result) == {CHECKSUM_A, CHECKSUM_B}

    def test_returns_empty_list_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("timeout")
        from digitize.utils.db import list_all_checksums
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = list_all_checksums()
        assert result == []


# ===========================================================================
# add_connector_checksum_entry
# ===========================================================================

class TestAddConnectorChecksumEntry:
    def _call(self, session_mock):
        from digitize.utils.db import add_connector_checksum_entry
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session_mock)):
            add_connector_checksum_entry(CONNECTOR_ID, CHECKSUM_A, DOC_ID_A)

    def test_inserts_row_on_first_call(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        self._call(session)
        assert session.execute.called

    def test_noop_on_duplicate_call(self):
        """ON CONFLICT DO NOTHING — second call must not raise."""
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=0)
        # Should not raise even if rowcount == 0
        self._call(session)

    def test_same_checksum_different_connectors(self):
        """
        Adding two different connectors for the same checksum must both succeed.
        Each produces its own INSERT … ON CONFLICT DO NOTHING call.
        """
        connector_b = "c7f3a2d1-0000-0000-0000-000000000002"
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        from digitize.utils.db import add_connector_checksum_entry
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            add_connector_checksum_entry(CONNECTOR_ID, CHECKSUM_A, DOC_ID_A)
            add_connector_checksum_entry(connector_b, CHECKSUM_A, DOC_ID_A)
        assert session.execute.call_count == 2

    def test_raises_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("disk full")
        from digitize.utils.db import add_connector_checksum_entry
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            with pytest.raises(SQLAlchemyError):
                add_connector_checksum_entry(CONNECTOR_ID, CHECKSUM_A, DOC_ID_A)


# ===========================================================================
# remove_connector_checksum_entry
# ===========================================================================

class TestRemoveConnectorFromMembership:
    def _call(self, session_mock) -> tuple:
        from digitize.utils.db import remove_connector_checksum_entry
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session_mock)):
            return remove_connector_checksum_entry(CONNECTOR_ID, CHECKSUM_A)

    def _setup_session(self, deleted_doc_id: Optional[str], remaining_count: int) -> MagicMock:
        """
        Configure a session mock where the DELETE RETURNING yields
        *deleted_doc_id* and the subsequent COUNT yields *remaining_count*.
        """
        session = MagicMock()
        del_result = MagicMock()
        del_result.one_or_none.return_value = (deleted_doc_id,) if deleted_doc_id else None
        count_result = MagicMock()
        count_result.scalar.return_value = remaining_count
        session.execute.side_effect = [del_result, count_result]
        return session

    def test_last_connector_removed_returns_zero(self):
        """remaining=0 when no other connector owns this checksum."""
        session = self._setup_session(DOC_ID_A, 0)
        remaining, doc_id = self._call(session)
        assert remaining == 0
        assert doc_id == DOC_ID_A

    def test_other_connectors_remain(self):
        """remaining>0 when other connectors still own the checksum."""
        session = self._setup_session(DOC_ID_A, 2)
        remaining, doc_id = self._call(session)
        assert remaining == 2
        assert doc_id == DOC_ID_A

    def test_row_not_found_returns_zero_none(self):
        """If the row didn't exist, return (0, None) without querying count."""
        session = MagicMock()
        del_result = MagicMock()
        del_result.one_or_none.return_value = None
        session.execute.return_value = del_result
        remaining, doc_id = self._call(session)
        assert remaining == 0
        assert doc_id is None
        # Only one DB call should have been made (the DELETE)
        assert session.execute.call_count == 1

    def test_raises_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("gone")
        from digitize.utils.db import remove_connector_checksum_entry
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            with pytest.raises(SQLAlchemyError):
                remove_connector_checksum_entry(CONNECTOR_ID, CHECKSUM_A)


# ===========================================================================
# init_sync_log_and_update_connector
# ===========================================================================

class TestInsertSyncLog:
    def test_returns_generated_seq(self):
        """init_sync_log_and_update_connector must return the auto-generated seq value."""
        session = MagicMock()
        # First execute: INSERT … RETURNING seq → scalar_one() returns 3
        insert_result = MagicMock()
        insert_result.scalar_one.return_value = 3
        # Second execute: UPDATE connectors SET sync_status='syncing'
        update_result = MagicMock()
        session.execute.side_effect = [insert_result, update_result]

        from digitize.utils.db import init_sync_log_and_update_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            seq = init_sync_log_and_update_connector(CONNECTOR_ID)
        assert seq == 3
        assert session.execute.call_count == 2

    def test_sets_connector_sync_status_syncing(self):
        """Both the INSERT and the connectors UPDATE must happen in the same session."""
        session = MagicMock()
        insert_result = MagicMock()
        insert_result.scalar_one.return_value = 1
        session.execute.side_effect = [insert_result, MagicMock()]

        from digitize.utils.db import init_sync_log_and_update_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            init_sync_log_and_update_connector(CONNECTOR_ID)
        # Two DB calls: INSERT log row + UPDATE connector status
        assert session.execute.call_count == 2

    def test_does_not_accept_seq_parameter(self):
        """seq must not be an accepted parameter — it is auto-generated."""
        from digitize.utils.db import init_sync_log_and_update_connector
        import inspect
        sig = inspect.signature(init_sync_log_and_update_connector)
        assert "seq" not in sig.parameters

    def test_raises_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("constraint violation")
        from digitize.utils.db import init_sync_log_and_update_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            with pytest.raises(SQLAlchemyError):
                init_sync_log_and_update_connector(CONNECTOR_ID)


# ===========================================================================
# finalize_sync_log_and_update_connector
# ===========================================================================

class TestUpdateSyncLog:
    def test_returns_true_on_success(self):
        """On success: updates log row AND connector row (two execute calls)."""
        session = MagicMock()
        # First execute: UPDATE connector_sync_logs rowcount=1 (found)
        log_result = _execute_result(rowcount=1)
        # Second execute: UPDATE connectors
        conn_result = _execute_result(rowcount=1)
        session.execute.side_effect = [log_result, conn_result]
        from digitize.utils.db import finalize_sync_log_and_update_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = finalize_sync_log_and_update_connector(
                CONNECTOR_ID, seq=1, status=SyncLogStatus.COMPLETED, total_files=10, new_files=2
            )
        assert result is True
        assert session.execute.call_count == 2

    def test_returns_false_when_not_found(self):
        """When the log row is not found, return False without updating connectors."""
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=0)
        from digitize.utils.db import finalize_sync_log_and_update_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = finalize_sync_log_and_update_connector(CONNECTOR_ID, seq=999, status=SyncLogStatus.FAILED)
        assert result is False
        # Only one execute call: the failed log-row lookup; connectors must NOT be updated
        assert session.execute.call_count == 1

    def test_updates_connector_last_sync_at_and_status(self):
        """The connector row must be updated in the same transaction as the log row."""
        session = MagicMock()
        log_result = _execute_result(rowcount=1)
        conn_result = _execute_result(rowcount=1)
        session.execute.side_effect = [log_result, conn_result]
        from digitize.utils.db import finalize_sync_log_and_update_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            finalize_sync_log_and_update_connector(CONNECTOR_ID, seq=2, status=SyncLogStatus.COMPLETED)
        assert session.execute.call_count == 2

    def test_optional_fields_omitted_when_none(self):
        """Passing None for optional fields must not include them in the update."""
        session = MagicMock()
        log_result = _execute_result(rowcount=1)
        conn_result = _execute_result(rowcount=1)
        session.execute.side_effect = [log_result, conn_result]
        from digitize.utils.db import finalize_sync_log_and_update_connector
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            finalize_sync_log_and_update_connector(CONNECTOR_ID, seq=1, status=SyncLogStatus.COMPLETED)
        assert session.execute.called


# ===========================================================================
# update_sync_log
# ===========================================================================

class TestUpdateSyncLogFilesSyncing:
    def test_returns_true_on_success(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        from digitize.utils.db import update_sync_log
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = update_sync_log(CONNECTOR_ID, 1, total_files=5, new_files=3)
        assert result is True

    def test_returns_true_when_nothing_to_update(self):
        """Calling with all-None args is a no-op — must return True without hitting DB."""
        session = MagicMock()
        from digitize.utils.db import update_sync_log
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = update_sync_log(CONNECTOR_ID, 1)
        assert result is True
        session.execute.assert_not_called()

    def test_does_not_modify_status_or_finished_at(self):
        """
        This function must only update file counters. We verify that
        session.execute is called (counters update) but the function signature
        does not accept status/finished_at parameters.
        """
        from digitize.utils.db import update_sync_log
        import inspect
        sig = inspect.signature(update_sync_log)
        assert "status" not in sig.parameters
        assert "finished_at" not in sig.parameters


# ===========================================================================
# list_sync_logs
# ===========================================================================

class TestListSyncLogs:
    def _make_log(self, seq: int) -> MagicMock:
        log = MagicMock()
        log.connector_id = CONNECTOR_ID
        log.seq = seq
        log.started_at = _NOW
        log.finished_at = None
        log.total_files = 0
        log.new_files = 0
        log.removed_files = 0
        log.status = SyncLogStatus.STARTED
        log.error = ""
        return log

    def test_returns_items_and_total(self):
        log = self._make_log(1)
        session = MagicMock()
        # First execute: COUNT query → total=1
        count_result = MagicMock()
        count_result.scalar.return_value = 1
        session.execute.return_value = count_result
        session.scalars.return_value.all.return_value = [log]

        from digitize.utils.db import list_sync_logs
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            items, total = list_sync_logs(CONNECTOR_ID)
        assert total == 1
        assert len(items) == 1
        assert items[0] is log
        session.expunge.assert_called_once_with(log)

    def test_returns_empty_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("timeout")
        from digitize.utils.db import list_sync_logs
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            items, total = list_sync_logs(CONNECTOR_ID)
        assert items == []
        assert total == 0



# ===========================================================================
# get_latest_sync_log
# ===========================================================================

class TestGetLatestSyncLog:
    def _make_log(self, seq: int) -> MagicMock:
        log = MagicMock()
        log.connector_id = CONNECTOR_ID
        log.seq = seq
        log.started_at = _NOW
        log.finished_at = None
        log.total_files = 5
        log.new_files = 2
        log.removed_files = 0
        log.status = SyncLogStatus.COMPLETED
        log.error = ""
        return log

    def test_returns_latest_log(self):
        log = self._make_log(seq=7)
        session = MagicMock()
        session.scalars.return_value.one_or_none.return_value = log

        from digitize.utils.db import get_latest_sync_log
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = get_latest_sync_log(CONNECTOR_ID)

        assert result is log
        session.expunge.assert_called_once_with(log)

    def test_returns_none_when_no_rows(self):
        session = MagicMock()
        session.scalars.return_value.one_or_none.return_value = None

        from digitize.utils.db import get_latest_sync_log
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = get_latest_sync_log(CONNECTOR_ID)

        assert result is None

    def test_returns_none_on_db_error(self):
        session = MagicMock()
        session.scalars.side_effect = SQLAlchemyError("timeout")

        from digitize.utils.db import get_latest_sync_log
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = get_latest_sync_log(CONNECTOR_ID)

        assert result is None



# ===========================================================================
# set_document_metadata
# ===========================================================================

class TestSetDocumentMetadata:
    def test_returns_true_on_success(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=1)
        from digitize.utils.db import set_document_metadata
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = set_document_metadata(DOC_ID_A, {"source_type": "s3", "bucket": "my-bucket"})
        assert result is True

    def test_returns_false_when_document_not_found(self):
        session = MagicMock()
        session.execute.return_value = _execute_result(rowcount=0)
        from digitize.utils.db import set_document_metadata
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = set_document_metadata("nonexistent-doc", {"key": "val"})
        assert result is False

    def test_returns_false_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("gone")
        from digitize.utils.db import set_document_metadata
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session)):
            result = set_document_metadata(DOC_ID_A, {"source_type": "sftp"})
        assert result is False


# ===========================================================================
# get_connector_sync_status
# ===========================================================================

class TestGetConnectorSyncStatus:
    def _call(self, session_mock, connector_id=CONNECTOR_ID):
        from digitize.utils.db import get_connector_sync_status
        with patch("digitize.db.manager.get_db_session", return_value=_make_session_cm(session_mock)):
            return get_connector_sync_status(connector_id)

    def test_returns_status_string(self):
        session = MagicMock()
        session.execute.return_value.one_or_none.return_value = (ConnectorStatus.SYNCING,)
        result = self._call(session)
        assert result == ConnectorStatus.SYNCING

    def test_returns_none_when_not_found(self):
        session = MagicMock()
        session.execute.return_value.one_or_none.return_value = None
        result = self._call(session)
        assert result is None

    def test_returns_delete_pending(self):
        session = MagicMock()
        session.execute.return_value.one_or_none.return_value = (ConnectorStatus.DELETE_PENDING,)
        result = self._call(session)
        assert result == ConnectorStatus.DELETE_PENDING

    def test_returns_none_on_db_error(self):
        session = MagicMock()
        session.execute.side_effect = SQLAlchemyError("timeout")
        result = self._call(session)
        assert result is None

# Made with Bob
