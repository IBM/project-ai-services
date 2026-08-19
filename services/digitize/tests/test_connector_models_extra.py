"""
Unit tests for Pydantic request/response models in
services/digitize/connectors/models.py

Coverage
--------
ConnectorCreateRequest
  - valid UUID connector_id is accepted
  - None connector_id is accepted (auto-generated on server side)
  - invalid connector_id raises ValidationError
  - all required fields (connector_name, type, allowed_extensions, connection_details)

ConnectorUpdateRequest
  - all fields are optional (empty body accepted)
  - partial fields accepted
  - connection_details may be partial dict

ConnectorListItem / ConnectorDetailResponse / SyncLogItem / SyncLogResponse
  - basic construction and serialisation

ConnectorStatus / SyncLogStatus / ConnectorError
  - enum value identity (inherits from str)
  - status comparison with raw string

SyncTriggerResponse
  - sync_seq field is required
"""

from __future__ import annotations

import pytest
from pydantic import ValidationError

from digitize.connectors.models import (
    ConnectorCreateRequest,
    ConnectorDetailResponse,
    ConnectorError,
    ConnectorListItem,
    ConnectorStatus,
    ConnectorUpdateRequest,
    SyncLogDetailResponse,
    SyncLogItem,
    SyncLogResponse,
    SyncLogStatus,
    SyncTriggerResponse,
)


# ---------------------------------------------------------------------------
# ConnectorStatus
# ---------------------------------------------------------------------------

class TestConnectorStatus:
    def test_up_to_date_value(self):
        assert ConnectorStatus.UP_TO_DATE == "up to date"

    def test_syncing_value(self):
        assert ConnectorStatus.SYNCING == "syncing"

    def test_out_of_sync_value(self):
        assert ConnectorStatus.OUT_OF_SYNC == "out of sync"

    def test_delete_pending_value(self):
        assert ConnectorStatus.DELETE_PENDING == "delete pending"

    def test_is_str_subclass(self):
        for member in ConnectorStatus:
            assert isinstance(member, str)

    def test_equality_with_raw_string(self):
        assert ConnectorStatus.SYNCING == "syncing"
        assert ConnectorStatus.DELETE_PENDING == "delete pending"


# ---------------------------------------------------------------------------
# SyncLogStatus
# ---------------------------------------------------------------------------

class TestSyncLogStatus:
    def test_started_value(self):
        assert SyncLogStatus.STARTED == "started"

    def test_cancel_pending_value(self):
        assert SyncLogStatus.CANCEL_PENDING == "cancel pending"

    def test_completed_value(self):
        assert SyncLogStatus.COMPLETED == "completed"

    def test_failed_value(self):
        assert SyncLogStatus.FAILED == "failed"

    def test_cancelled_value(self):
        assert SyncLogStatus.CANCELLED == "cancelled"

    def test_is_str_subclass(self):
        for member in SyncLogStatus:
            assert isinstance(member, str)


# ---------------------------------------------------------------------------
# ConnectorError
# ---------------------------------------------------------------------------

class TestConnectorError:
    def test_credential_error_msg_is_str(self):
        assert isinstance(ConnectorError.CREDENTIAL_ERROR_MSG, str)

    def test_credential_error_msg_contains_authentication(self):
        assert "Authentication failed" in ConnectorError.CREDENTIAL_ERROR_MSG


# ---------------------------------------------------------------------------
# ConnectorCreateRequest
# ---------------------------------------------------------------------------

class TestConnectorCreateRequest:
    def _valid_payload(self, **overrides):
        base = {
            "connector_name": "my-connector",
            "type": "s3",
            "allowed_extensions": [".pdf", ".docx"],
            "connection_details": {"bucket": "my-bucket"},
        }
        base.update(overrides)
        return base

    def test_valid_payload_accepted(self):
        req = ConnectorCreateRequest(**self._valid_payload())
        assert req.connector_name == "my-connector"

    def test_none_connector_id_accepted(self):
        req = ConnectorCreateRequest(**self._valid_payload(connector_id=None))
        assert req.connector_id is None

    def test_valid_uuid_connector_id_accepted(self):
        uid = "123e4567-e89b-12d3-a456-426614174000"
        req = ConnectorCreateRequest(**self._valid_payload(connector_id=uid))
        assert req.connector_id == uid

    def test_invalid_connector_id_raises(self):
        with pytest.raises(ValidationError, match="valid UUID"):
            ConnectorCreateRequest(**self._valid_payload(connector_id="not-a-uuid"))

    def test_missing_connector_name_raises(self):
        payload = self._valid_payload()
        del payload["connector_name"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_missing_type_raises(self):
        payload = self._valid_payload()
        del payload["type"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_missing_allowed_extensions_raises(self):
        payload = self._valid_payload()
        del payload["allowed_extensions"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_missing_connection_details_raises(self):
        payload = self._valid_payload()
        del payload["connection_details"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_connector_id_string_coerced(self):
        """A valid UUID passed as a stringified UUID object must be accepted."""
        uid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
        req = ConnectorCreateRequest(**self._valid_payload(connector_id=str(uid)))
        assert req.connector_id == uid


# ---------------------------------------------------------------------------
# ConnectorUpdateRequest
# ---------------------------------------------------------------------------

class TestConnectorUpdateRequest:
    def test_empty_body_accepted(self):
        req = ConnectorUpdateRequest()
        assert req.connector_name is None
        assert req.allowed_extensions is None
        assert req.connection_details is None

    def test_partial_name_only(self):
        req = ConnectorUpdateRequest(connector_name="new-name")
        assert req.connector_name == "new-name"
        assert req.allowed_extensions is None

    def test_partial_connection_details(self):
        req = ConnectorUpdateRequest(connection_details={"host": "new-host"})
        assert req.connection_details == {"host": "new-host"}

    def test_all_fields_supplied(self):
        req = ConnectorUpdateRequest(
            connector_name="updated",
            allowed_extensions=[".pdf"],
            connection_details={"host": "h", "port": 22},
        )
        assert req.connector_name == "updated"
        assert req.allowed_extensions == [".pdf"]
        assert req.connection_details["port"] == 22


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------

class TestConnectorListItem:
    def test_construction(self):
        item = ConnectorListItem(
            connector_id="c1",
            connector_name="my-conn",
            type="s3",
            attached_at="2024-01-01T00:00:00Z",
            last_sync_at=None,
            sync_status="up to date",
            error=None,
            total_files=42,
        )
        assert item.total_files == 42
        assert item.last_sync_at is None


class TestConnectorDetailResponse:
    def test_construction(self):
        resp = ConnectorDetailResponse(
            connector_id="c1",
            connector_name="my-conn",
            type="ssh",
            allowed_extensions=[".pdf"],
            sync_interval_seconds=300,
            attached_at="2024-01-01T00:00:00Z",
            last_sync_at=None,
            sync_status="syncing",
            error=None,
            connection_details={"host": "sftp.example.com"},
            total_files=10,
        )
        assert resp.sync_interval_seconds == 300
        assert resp.connection_details["host"] == "sftp.example.com"


class TestSyncLogItem:
    def test_construction(self):
        item = SyncLogItem(
            seq=1,
            started_at="2024-01-01T00:00:00Z",
            finished_at="2024-01-01T00:05:00Z",
            total_files=100,
            new_files=10,
            removed_files=2,
            status="completed",
            error="",
        )
        assert item.seq == 1
        assert item.error == ""


class TestSyncLogResponse:
    def test_construction_with_items(self):
        item = SyncLogItem(
            seq=1,
            started_at="2024-01-01T00:00:00Z",
            finished_at=None,
            total_files=5,
            new_files=5,
            removed_files=0,
            status="started",
            error="",
        )
        resp = SyncLogResponse(total=1, limit=50, offset=0, items=[item])
        assert resp.total == 1
        assert len(resp.items) == 1


class TestSyncTriggerResponse:
    def test_construction(self):
        resp = SyncTriggerResponse(sync_seq=7)
        assert resp.sync_seq == 7

    def test_sync_seq_required(self):
        with pytest.raises(ValidationError):
            SyncTriggerResponse()


class TestSyncLogDetailResponse:
    def test_construction(self):
        resp = SyncLogDetailResponse(
            seq=3,
            started_at="2024-01-01T00:00:00Z",
            finished_at=None,
            total_files=0,
            new_files=0,
            removed_files=0,
            status="failed",
            error="something went wrong",
        )
        assert resp.status == "failed"
        assert resp.error == "something went wrong"

# Made with Bob
