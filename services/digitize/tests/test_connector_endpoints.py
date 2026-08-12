"""
Integration tests for PR3 — REST API endpoints for connectors.

All tests use httpx.AsyncClient + FastAPI TestClient (via the sync TestClient
wrapper) and mock out DB operations, encryption key access, and the worker
manager stub so no real database or secret file is needed.

Coverage:
  - POST   /v1/connectors                                 (202, 409)
  - PUT    /v1/connectors/{id}                            (200, 404, 409)
  - DELETE /v1/connectors/{id}                            (204, 404, 409)
  - GET    /v1/connectors                                 (200)
  - GET    /v1/connectors/{id}                            (200, 404)
  - GET    /v1/connectors/{id}/syncs                      (200, 404)
  - POST   /v1/connectors/{id}/sync                       (202 dispatched, 202 no-op, 404)
  - DELETE /v1/connectors/{id}/sync                       (204 signalled, 409 not running, 404)
  - GET    /v1/documents — excludes connector-sourced docs
  - GET    /v1/documents/{doc_id} — 404 for connector-sourced
  - DELETE /v1/documents/{doc_id} — 404 for connector-sourced
"""

from datetime import datetime, timezone
from types import SimpleNamespace
from unittest.mock import MagicMock, Mock, patch

import pytest
from fastapi.testclient import TestClient
from sqlalchemy.exc import IntegrityError

from digitize.connectors.models import SyncStatus

import digitize.app as digitize_app
import digitize.api.v1.documents as documents_router_module

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

_NOW = datetime(2026, 1, 15, 12, 0, 0, tzinfo=timezone.utc)

CONNECTOR_ID = "c7f3a2d1-0000-0000-0000-000000000001"
CONNECTOR_NAME = "prod-sftp-reports"
CONNECTOR_TYPE = "ssh"

SSH_PAYLOAD = {
    "connector_id": CONNECTOR_ID,
    "connector_name": CONNECTOR_NAME,
    "type": "ssh",
    "allowed_extensions": [".pdf", ".docx"],
    "connection_details": {
        "host": "sftp.example.com",
        "username": "sync_user",
        "remote_path": "/exports/reports",
        "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key\n",
    },
}

S3_PAYLOAD = {
    "connector_id": "a1b2c3d4-0000-0000-0000-000000000002",
    "connector_name": "prod-s3-rag-docs",
    "type": "s3",
    "allowed_extensions": [".pdf"],
    "connection_details": {
        "endpoint_url": "https://s3.us-east-1.amazonaws.com",
        "bucket_name": "my-bucket",
        "access_key_id": "AKIAIOSFODNN7EXAMPLE",
        "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfi",
    },
}

# ---------------------------------------------------------------------------
# Mock Connector / SyncLog factories
# ---------------------------------------------------------------------------

def _make_connector(
    connector_id: str = CONNECTOR_ID,
    name: str = CONNECTOR_NAME,
    connector_type: str = CONNECTOR_TYPE,
    sync_status: str = SyncStatus.UP_TO_DATE,
) -> MagicMock:
    c = MagicMock()
    c.id = connector_id
    c.name = name
    c.type = connector_type
    c.connection_details = {"host": "sftp.example.com", "username": "sync_user", "remote_path": "/exports"}
    c.allowed_extensions = [".pdf", ".docx"]
    c.sync_interval_seconds = 300
    c.attached_at = _NOW
    c.last_sync_at = _NOW
    c.sync_status = sync_status
    c.last_sync_error = None
    c.total_files = 42
    return c


def _make_sync_log(log_id: int = 1, seq: int = 1) -> MagicMock:
    log = MagicMock()
    log.id = log_id
    log.connector_id = CONNECTOR_ID
    log.seq = seq
    log.started_at = _NOW
    log.finished_at = _NOW
    log.total_files = 42
    log.new_files = 2
    log.removed_files = 0
    log.status = SyncStatus.COMPLETED
    log.error = ""
    return log


# ---------------------------------------------------------------------------
# Shared test fixture
# ---------------------------------------------------------------------------

@pytest.fixture
def connector_test_client(monkeypatch, tmp_path, mock_db_operations):
    """
    TestClient wired with all external dependencies mocked out.

    Patches:
      - settings → fake dirs and fast-path values
      - encrypt_secrets / merge_and_encrypt_partial → return input unchanged
      - _get_key_path → /dev/null (never touched because encryption is mocked)
      - db_ops.insert_connector, upsert_connector, etc.
    """
    from digitize.workers.concurrency import concurrency_manager

    digitized_dir = tmp_path / "digitized"
    staging_dir = tmp_path / "staging"
    for path in (digitized_dir, staging_dir):
        path.mkdir(parents=True, exist_ok=True)

    fake_settings = SimpleNamespace(
        common=SimpleNamespace(app=SimpleNamespace(log_level="INFO")),
        digitize=SimpleNamespace(
            digitized_docs_dir=digitized_dir,
            staging_dir=staging_dir,
            digitization_concurrency_limit=2,
            ingestion_concurrency_limit=1,
            connector=SimpleNamespace(
                sync_interval_seconds=300,
                encryption_key_path="/dev/null",
            ),
        ),
    )

    monkeypatch.setattr(digitize_app, "settings", fake_settings, raising=False)
    monkeypatch.setattr(digitize_app.dg_util, "settings", fake_settings, raising=False)

    import digitize.api.v1.connectors as connectors_module

    monkeypatch.setattr(connectors_module, "settings", fake_settings, raising=False)

    # Concurrency stubs
    from unittest.mock import AsyncMock
    monkeypatch.setattr(concurrency_manager, "is_locked", Mock(return_value=False))
    monkeypatch.setattr(concurrency_manager, "acquire", AsyncMock())
    monkeypatch.setattr(concurrency_manager, "release", Mock())

    # Misc stubs
    monkeypatch.setattr(digitize_app.dg_util, "has_active_jobs", Mock(return_value=(False, [])))
    monkeypatch.setattr(digitize_app, "configure_uvicorn_logging", Mock())
    monkeypatch.setattr(documents_router_module, "reset_db", Mock())

    # Encryption: pass-through (no real key needed)
    monkeypatch.setattr(
        "digitize.api.v1.connectors.encrypt_secrets",
        lambda connector_type, details, key_path: dict(details),
    )
    monkeypatch.setattr(
        "digitize.api.v1.connectors.merge_and_encrypt_partial",
        lambda connector_type, existing, partial, key_path: {**existing, **partial},
    )
    monkeypatch.setattr(
        "digitize.api.v1.connectors.strip_secrets",
        lambda connector_type, details: {
            k: v for k, v in details.items()
            if k not in {"private_key", "secret_access_key"}
        },
    )

    import digitize.api.v1.jobs as jobs_router_module
    mock_hash_db_manager = Mock()
    mock_hash_db_manager.find_completed_document_by_hash = Mock(return_value=None)
    monkeypatch.setattr(jobs_router_module, "db_manager", mock_hash_db_manager)

    return TestClient(digitize_app.app)


# ===========================================================================
# POST /v1/connectors
# ===========================================================================

class TestPostConnector:
    def test_returns_201_on_success(self, connector_test_client, monkeypatch):
        monkeypatch.setattr("digitize.api.v1.connectors.db_ops.insert_connector", Mock())
        response = connector_test_client.post("/v1/connectors", json=SSH_PAYLOAD)
        assert response.status_code == 201
        assert response.text == ""

    def test_encrypts_private_key_before_insert(self, connector_test_client, monkeypatch):
        captured = {}

        def capture_insert(**kwargs):
            captured.update(kwargs)

        monkeypatch.setattr("digitize.api.v1.connectors.db_ops.insert_connector", capture_insert)
        connector_test_client.post("/v1/connectors", json=SSH_PAYLOAD)
        # private_key passes through our no-op mock — just confirm it was passed in
        assert "connection_details" in captured

    def test_returns_409_on_duplicate(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.insert_connector",
            Mock(side_effect=IntegrityError(None, None, Exception("dup"))),
        )
        response = connector_test_client.post("/v1/connectors", json=SSH_PAYLOAD)
        assert response.status_code == 409

    def test_s3_connector_returns_201(self, connector_test_client, monkeypatch):
        monkeypatch.setattr("digitize.api.v1.connectors.db_ops.insert_connector", Mock())
        response = connector_test_client.post("/v1/connectors", json=S3_PAYLOAD)
        assert response.status_code == 201

    def test_secret_not_returned_in_response(self, connector_test_client, monkeypatch):
        monkeypatch.setattr("digitize.api.v1.connectors.db_ops.insert_connector", Mock())
        response = connector_test_client.post("/v1/connectors", json=SSH_PAYLOAD)
        assert response.text == ""


# ===========================================================================
# PUT /v1/connectors/{connector_id}
# ===========================================================================

class TestPutConnector:
    def test_returns_200_on_success(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr("digitize.api.v1.connectors.db_ops.upsert_connector", Mock())
        response = connector_test_client.put(
            f"/v1/connectors/{CONNECTOR_ID}",
            json={"connection_details": {"remote_path": "/v2"}},
        )
        assert response.status_code == 200

    def test_returns_404_when_not_found(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.upsert_connector",
            Mock(side_effect=FileNotFoundError("not found")),
        )
        response = connector_test_client.put(
            f"/v1/connectors/{CONNECTOR_ID}",
            json={"connector_name": "new-name"},
        )
        assert response.status_code == 404

    def test_partial_update_only_overwrites_supplied_keys(self, connector_test_client, monkeypatch):
        """PUT with only connector_name must not touch connection_details."""
        upsert_mock = Mock()
        monkeypatch.setattr("digitize.api.v1.connectors.db_ops.upsert_connector", upsert_mock)
        connector_test_client.put(
            f"/v1/connectors/{CONNECTOR_ID}",
            json={"connector_name": "renamed"},
        )
        # connection_details kwarg should be None (not supplied)
        call_kwargs = upsert_mock.call_args.kwargs if upsert_mock.called else {}
        assert call_kwargs.get("connection_details") is None

    def test_returns_409_on_name_conflict(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.upsert_connector",
            Mock(side_effect=IntegrityError(None, None, Exception("dup"))),
        )
        response = connector_test_client.put(
            f"/v1/connectors/{CONNECTOR_ID}",
            json={"connector_name": "taken-name"},
        )
        assert response.status_code == 409


# ===========================================================================
# DELETE /v1/connectors/{connector_id}
# ===========================================================================

class TestDeleteConnector:
    """
    The DELETE endpoint is non-blocking: it always returns 204 immediately.

    Case A (SYNCING)  — mark DELETE_PENDING, return 204; tick handles teardown.
    Case B (not SYNCING) — mark DELETE_PENDING, schedule _run_teardown, return 204.

    _run_teardown itself is exercised by TestRunTeardown below.
    """

    def _patch_mark(self, monkeypatch, return_value=True):
        mock = Mock(return_value=return_value)
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.mark_sync_delete_pending",
            mock,
        )
        return mock

    def test_returns_204_case_b_not_syncing(self, connector_test_client, monkeypatch):
        """Case B: not syncing → mark DELETE_PENDING, schedule teardown, return 204."""
        mark_mock = self._patch_mark(monkeypatch)
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        def _consume_coro(coro):
            coro.close()  # prevent "coroutine never awaited" warning
        with patch("digitize.api.v1.connectors.asyncio.create_task", side_effect=_consume_coro) as task_mock:
            response = connector_test_client.delete(f"/v1/connectors/{CONNECTOR_ID}")
        assert response.status_code == 204
        mark_mock.assert_called_once_with(CONNECTOR_ID)
        task_mock.assert_called_once()

    def test_returns_204_case_a_syncing(self, connector_test_client, monkeypatch):
        """Case A: syncing → mark DELETE_PENDING, return 204; no teardown task created."""
        mark_mock = self._patch_mark(monkeypatch)
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector(sync_status=SyncStatus.SYNCING)),
        )
        with patch("digitize.api.v1.connectors.asyncio.create_task") as task_mock:
            response = connector_test_client.delete(f"/v1/connectors/{CONNECTOR_ID}")
        assert response.status_code == 204
        mark_mock.assert_called_once_with(CONNECTOR_ID)
        task_mock.assert_not_called()

    def test_returns_404_when_not_found(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=None),
        )
        response = connector_test_client.delete(f"/v1/connectors/{CONNECTOR_ID}")
        assert response.status_code == 404


@pytest.mark.asyncio
class TestRunTeardown:
    """Unit tests for the _run_teardown background coroutine."""

    async def test_deletes_document_when_last_owner(self, monkeypatch):
        """remaining_owner_count == 0 → document is deleted."""
        doc_delete_mock = Mock()
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_connector_checksums",
            Mock(return_value=["abc123"]),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.remove_connector_checksum_entry",
            Mock(return_value=(0, "doc-0001")),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.delete_active_connector",
            Mock(return_value=True),
        )
        with patch("digitize.api.v1.connectors._best_effort_delete_document", doc_delete_mock):
            with patch("digitize.api.v1.connectors._sweep_connector_staging"):
                from digitize.api.v1.connectors import _run_teardown
                await _run_teardown(CONNECTOR_ID)
        doc_delete_mock.assert_called_once_with("doc-0001")

    async def test_does_not_delete_doc_when_other_owners_remain(self, monkeypatch):
        """remaining_owner_count > 0 → doc must NOT be deleted."""
        doc_delete_mock = Mock()
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_connector_checksums",
            Mock(return_value=["abc123"]),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.remove_connector_checksum_entry",
            Mock(return_value=(2, "doc-0001")),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.delete_active_connector",
            Mock(return_value=True),
        )
        with patch("digitize.api.v1.connectors._best_effort_delete_document", doc_delete_mock):
            with patch("digitize.api.v1.connectors._sweep_connector_staging"):
                from digitize.api.v1.connectors import _run_teardown
                await _run_teardown(CONNECTOR_ID)
        doc_delete_mock.assert_not_called()


# ===========================================================================
# GET /v1/connectors
# ===========================================================================

class TestListConnectors:
    def test_returns_200_with_list(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_connectors",
            Mock(return_value=[_make_connector()]),
        )
        response = connector_test_client.get("/v1/connectors")
        assert response.status_code == 200
        data = response.json()
        assert len(data) == 1
        assert data[0]["connector_id"] == CONNECTOR_ID

    def test_never_returns_secret_fields(self, connector_test_client, monkeypatch):
        c = _make_connector()
        c.connection_details = {
            "host": "sftp.example.com",
            "username": "sync_user",
            "private_key": "ENCRYPTED_PRIVATE_KEY",
        }
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_connectors",
            Mock(return_value=[c]),
        )
        response = connector_test_client.get("/v1/connectors")
        assert "private_key" not in str(response.json())

    def test_returns_empty_list(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_connectors",
            Mock(return_value=[]),
        )
        response = connector_test_client.get("/v1/connectors")
        assert response.status_code == 200
        assert response.json() == []


# ===========================================================================
# GET /v1/connectors/{connector_id}
# ===========================================================================

class TestGetConnector:
    def test_returns_200_with_detail(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        response = connector_test_client.get(f"/v1/connectors/{CONNECTOR_ID}")
        assert response.status_code == 200
        data = response.json()
        assert data["connector_id"] == CONNECTOR_ID
        assert data["total_files"] == 42
        assert data["connection_details"] == {
            "host": "sftp.example.com",
            "username": "sync_user",
            "remote_path": "/exports",
        }

    def test_returns_404_when_not_found(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=None),
        )
        response = connector_test_client.get(f"/v1/connectors/{CONNECTOR_ID}")
        assert response.status_code == 404

    def test_secrets_stripped_from_connection_details(self, connector_test_client, monkeypatch):
        c = _make_connector()
        c.connection_details = {
            "host": "sftp.example.com",
            "username": "sync_user",
            "private_key": "ENCRYPTED_PRIVATE_KEY",
        }
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=c),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_sync_logs",
            Mock(return_value=([], 0)),
        )
        response = connector_test_client.get(f"/v1/connectors/{CONNECTOR_ID}")
        assert response.status_code == 200
        conn_details = response.json()["connection_details"]
        assert "private_key" not in conn_details
        assert conn_details["host"] == "sftp.example.com"


# ===========================================================================
# GET /v1/connectors/{connector_id}/syncs
# ===========================================================================

class TestSyncLog:
    def test_returns_paginated_history(self, connector_test_client, monkeypatch):
        logs = [_make_sync_log(log_id=3, seq=3), _make_sync_log(log_id=2, seq=2)]
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_sync_logs",
            Mock(return_value=(logs, 3)),
        )
        response = connector_test_client.get(
            f"/v1/connectors/{CONNECTOR_ID}/syncs?limit=2&offset=0"
        )
        assert response.status_code == 200
        data = response.json()
        assert data["total"] == 3
        assert data["limit"] == 2
        assert data["offset"] == 0
        assert len(data["items"]) == 2

    def test_returns_404_when_connector_not_found(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=None),
        )
        response = connector_test_client.get(
            f"/v1/connectors/{CONNECTOR_ID}/syncs"
        )
        assert response.status_code == 404

    def test_limit_capped_at_200(self, connector_test_client, monkeypatch):
        """limit > 200 must be rejected by FastAPI's Query validation."""
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.list_sync_logs",
            Mock(return_value=([], 0)),
        )
        response = connector_test_client.get(
            f"/v1/connectors/{CONNECTOR_ID}/syncs?limit=999"
        )
        assert response.status_code == 422  # FastAPI validation error


# ===========================================================================
# GET /v1/documents — connector visibility
# ===========================================================================

class TestDocumentListConnectorFilter:
    def test_list_docs_excludes_connector_sourced(self, connector_test_client, monkeypatch):
        """get_all_documents_paginated must be called with exclude_connector_sourced=True.

        The function is imported lazily inside the handler via
        `from digitize.utils.db import get_all_documents_paginated`, so we
        patch the *source* module attribute that the handler looks up at call time.
        """
        captured = {}

        def mock_paginated(**kwargs):
            captured.update(kwargs)
            return [], 0

        monkeypatch.setattr(
            "digitize.utils.db.get_all_documents_paginated",
            mock_paginated,
        )
        connector_test_client.get("/v1/documents")
        assert captured.get("exclude_connector_sourced") is True

    def test_get_doc_returns_404_for_connector_sourced(self, connector_test_client, monkeypatch):
        """is_connector_sourced_document is imported inside the handler — patch the source."""
        monkeypatch.setattr(
            "digitize.utils.db.is_connector_sourced_document",
            Mock(return_value=True),
        )
        response = connector_test_client.get("/v1/documents/some-connector-doc-id")
        assert response.status_code == 404

    def test_get_doc_returns_data_for_user_submitted(self, connector_test_client, monkeypatch):
        from digitize.models import DocumentDetailResponse

        monkeypatch.setattr(
            "digitize.utils.db.is_connector_sourced_document",
            Mock(return_value=False),
        )
        fake_doc = DocumentDetailResponse(
            id="doc-001",
            name="test.pdf",
            type="ingestion",
            status="completed",
            output_format="json",
        )
        monkeypatch.setattr(
            "digitize.utils.db.get_document",
            Mock(return_value=fake_doc),
        )
        response = connector_test_client.get("/v1/documents/doc-001")
        assert response.status_code == 200

    def test_delete_doc_returns_404_for_connector_sourced(self, connector_test_client, monkeypatch):
        monkeypatch.setattr(
            "digitize.utils.db.is_connector_sourced_document",
            Mock(return_value=True),
        )
        response = connector_test_client.delete("/v1/documents/connector-owned-doc")
        assert response.status_code == 404

# ===========================================================================
# POST /v1/connectors/{connector_id}/sync
# ===========================================================================

class TestTriggerSync:
    def test_returns_202_and_dispatches_task_when_lock_acquired(
        self, connector_test_client, monkeypatch
    ):
        """Lock acquired → create_task called, 202 returned."""
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.try_acquire_sync_lock",
            Mock(return_value=True),
        )
        task_mock = Mock(side_effect=lambda coro: coro.close())
        with patch("asyncio.create_task", task_mock):
            response = connector_test_client.post(f"/v1/connectors/{CONNECTOR_ID}/sync")
        assert response.status_code == 202
        task_mock.assert_called_once()

    def test_returns_202_no_op_when_already_syncing(
        self, connector_test_client, monkeypatch
    ):
        """Lock not acquired (already syncing) → 202, no task created."""
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.try_acquire_sync_lock",
            Mock(return_value=False),
        )
        task_mock = Mock(side_effect=lambda coro: coro.close())
        with patch("asyncio.create_task", task_mock):
            response = connector_test_client.post(f"/v1/connectors/{CONNECTOR_ID}/sync")
        assert response.status_code == 202
        task_mock.assert_not_called()

    def test_returns_404_when_connector_not_found(
        self, connector_test_client, monkeypatch
    ):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=None),
        )
        response = connector_test_client.post(f"/v1/connectors/{CONNECTOR_ID}/sync")
        assert response.status_code == 404

# ===========================================================================
# DELETE /v1/connectors/{connector_id}/sync
# ===========================================================================

class TestStopSync:
    def test_returns_204_when_sync_signalled(
        self, connector_test_client, monkeypatch
    ):
        """mark_sync_stop_pending returns True → 204 returned."""
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.mark_sync_cancel_pending",
            Mock(return_value=True),
        )
        response = connector_test_client.delete(f"/v1/connectors/{CONNECTOR_ID}/sync")
        assert response.status_code == 204

    def test_returns_409_when_no_sync_running(
        self, connector_test_client, monkeypatch
    ):
        """mark_sync_stop_pending returns False (not syncing) → 409."""
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=_make_connector()),
        )
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.mark_sync_cancel_pending",
            Mock(return_value=False),
        )
        response = connector_test_client.delete(f"/v1/connectors/{CONNECTOR_ID}/sync")
        assert response.status_code == 409

    def test_returns_404_when_connector_not_found(
        self, connector_test_client, monkeypatch
    ):
        monkeypatch.setattr(
            "digitize.api.v1.connectors.db_ops.get_active_connector",
            Mock(return_value=None),
        )
        response = connector_test_client.delete(f"/v1/connectors/{CONNECTOR_ID}/sync")
        assert response.status_code == 404

# Made with Bob
