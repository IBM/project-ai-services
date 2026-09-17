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
  - file_system connection_details validated against SSHConnectorConfig
  - object_storage connection_details validated against S3ConnectorConfig
  - password field on file_system raises ValidationError at parse time
  - extra field on file_system raises ValidationError at parse time

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

_FAKE_PEM = "-----BEGIN OPENSSH PRIVATE KEY-----\nfakekey\n-----END OPENSSH PRIVATE KEY-----"

_VALID_SSH_DETAILS = {
    "host": "sftp.example.com",
    "username": "sync_user",
    "private_key": _FAKE_PEM,
    "remote_path": "/exports",
}

_VALID_S3_DETAILS = {
    "bucket_name": "my-bucket",
    "access_key_id": "AKID",
    "secret_access_key": "SECRET",
    "endpoint_url": "https://s3.us-east-1.amazonaws.com",
}


class TestConnectorCreateRequest:
    def _ssh_payload(self, **overrides):
        base = {
            "name": "my-connector",
            "type": "file_system",
            "allowed_extensions": [".pdf", ".docx"],
            "connection_details": dict(_VALID_SSH_DETAILS),
        }
        base.update(overrides)
        return base

    def _s3_payload(self, **overrides):
        base = {
            "name": "my-s3-connector",
            "type": "object_storage",
            "allowed_extensions": [".pdf"],
            "connection_details": dict(_VALID_S3_DETAILS),
        }
        base.update(overrides)
        return base

    def test_valid_ssh_payload_accepted(self):
        req = ConnectorCreateRequest(**self._ssh_payload())
        assert req.name == "my-connector"
        assert req.type == "file_system"

    def test_valid_s3_payload_accepted(self):
        req = ConnectorCreateRequest(**self._s3_payload())
        assert req.name == "my-s3-connector"
        assert req.type == "object_storage"

    def test_none_id_accepted(self):
        req = ConnectorCreateRequest(**self._ssh_payload(id=None))
        assert req.id is None

    def test_valid_uuid_id_accepted(self):
        uid = "123e4567-e89b-12d3-a456-426614174000"
        req = ConnectorCreateRequest(**self._ssh_payload(id=uid))
        assert req.id == uid

    def test_invalid_id_raises(self):
        with pytest.raises(ValidationError, match="valid UUID"):
            ConnectorCreateRequest(**self._ssh_payload(id="not-a-uuid"))

    def test_missing_name_raises(self):
        payload = self._ssh_payload()
        del payload["name"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_missing_type_raises(self):
        payload = self._ssh_payload()
        del payload["type"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_missing_allowed_extensions_raises(self):
        payload = self._ssh_payload()
        del payload["allowed_extensions"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_missing_connection_details_raises(self):
        payload = self._ssh_payload()
        del payload["connection_details"]
        with pytest.raises(ValidationError):
            ConnectorCreateRequest(**payload)

    def test_id_string_coerced(self):
        """A valid UUID passed as a stringified UUID object must be accepted."""
        uid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
        req = ConnectorCreateRequest(**self._ssh_payload(id=str(uid)))
        assert req.id == uid

    # ------------------------------------------------------------------
    # connection_details schema validation
    # ------------------------------------------------------------------

    def test_file_system_password_field_rejected(self):
        """password is not a valid field for file_system — must raise at parse time."""
        bad_details = {**_VALID_SSH_DETAILS, "password": "hunter2"}
        with pytest.raises(ValidationError, match="Invalid connection_details"):
            ConnectorCreateRequest(**self._ssh_payload(connection_details=bad_details))

    def test_file_system_extra_field_rejected(self):
        """Any unrecognised field on file_system must raise at parse time."""
        bad_details = {**_VALID_SSH_DETAILS, "api_token": "tok"}
        with pytest.raises(ValidationError, match="Invalid connection_details"):
            ConnectorCreateRequest(**self._ssh_payload(connection_details=bad_details))

    def test_file_system_missing_required_field_rejected(self):
        """Omitting a required field (private_key) must raise at parse time."""
        bad_details = {"host": "sftp.example.com", "username": "u", "remote_path": "/"}
        with pytest.raises(ValidationError, match="Invalid connection_details"):
            ConnectorCreateRequest(**self._ssh_payload(connection_details=bad_details))

    def test_object_storage_extra_field_rejected(self):
        """Any unrecognised field on object_storage must raise at parse time."""
        bad_details = {**_VALID_S3_DETAILS, "password": "oops"}
        with pytest.raises(ValidationError, match="Invalid connection_details"):
            ConnectorCreateRequest(**self._s3_payload(connection_details=bad_details))

    def test_object_storage_missing_bucket_name_rejected(self):
        """bucket_name is required for object_storage."""
        bad_details = {k: v for k, v in _VALID_S3_DETAILS.items() if k != "bucket_name"}
        with pytest.raises(ValidationError, match="Invalid connection_details"):
            ConnectorCreateRequest(**self._s3_payload(connection_details=bad_details))


# ---------------------------------------------------------------------------
# ConnectorUpdateRequest
# ---------------------------------------------------------------------------

class TestConnectorUpdateRequest:
    def test_empty_body_accepted(self):
        req = ConnectorUpdateRequest()
        assert req.name is None
        assert req.allowed_extensions is None
        assert req.connection_details is None

    def test_partial_name_only(self):
        req = ConnectorUpdateRequest(name="new-name")
        assert req.name == "new-name"
        assert req.allowed_extensions is None

    def test_partial_connection_details(self):
        req = ConnectorUpdateRequest(connection_details={"host": "new-host"})
        assert req.connection_details == {"host": "new-host"}

    def test_all_fields_supplied(self):
        req = ConnectorUpdateRequest(
            name="updated",
            allowed_extensions=[".pdf"],
            connection_details={"host": "h", "port": 22},
        )
        assert req.name == "updated"
        assert req.allowed_extensions == [".pdf"]
        assert req.connection_details["port"] == 22


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------

class TestConnectorListItem:
    def test_construction(self):
        item = ConnectorListItem(
            id="c1",
            name="my-conn",
            type="s3",
            attached_at="2024-01-01T00:00:00Z",
            last_sync_at=None,
            status="up to date",
            total_files=42,
            message=None,
        )
        assert item.total_files == 42
        assert item.last_sync_at is None
        assert item.message is None

    def test_construction_with_message(self):
        item = ConnectorListItem(
            id="c1",
            name="my-conn",
            type="s3",
            attached_at="2024-01-01T00:00:00Z",
            last_sync_at=None,
            status="syncing",
            total_files=42,
            message="Processing 3/10 files",
        )
        assert item.message == "Processing 3/10 files"


class TestConnectorDetailResponse:
    def test_construction(self):
        resp = ConnectorDetailResponse(
            id="c1",
            name="my-conn",
            type="ssh",
            allowed_extensions=[".pdf"],
            sync_interval_seconds=300,
            attached_at="2024-01-01T00:00:00Z",
            last_sync_at=None,
            status="syncing",
            connection_details={"host": "sftp.example.com"},
            total_files=10,
            message=None,
        )
        assert resp.sync_interval_seconds == 300
        assert resp.connection_details["host"] == "sftp.example.com"
        assert resp.message is None


class TestSyncLogItem:
    def test_construction(self):
        item = SyncLogItem(
            seq=1,
            started_at="2024-01-01T00:00:00Z",
            finished_at="2024-01-01T00:05:00Z",
            total_files=100,
            new_files=10,
            completed_files=10,
            removed_files=2,
            status="completed",
            error="",
        )
        assert item.seq == 1
        assert item.error == ""
        assert item.completed_files == 10


class TestSyncLogResponse:
    def test_construction_with_items(self):
        item = SyncLogItem(
            seq=1,
            started_at="2024-01-01T00:00:00Z",
            finished_at=None,
            total_files=5,
            new_files=5,
            completed_files=3,
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


class TestSyncLogDetailEndpointShape:
    def test_single_item_in_list_envelope(self):
        resp = SyncLogResponse(
            total=1,
            limit=1,
            offset=0,
            items=[
                SyncLogItem(
                    seq=3,
                    started_at="2024-01-01T00:00:00Z",
                    finished_at=None,
                    total_files=0,
                    new_files=0,
                    completed_files=0,
                    removed_files=0,
                    status="failed",
                    error="something went wrong",
                )
            ],
        )
        assert resp.total == 1
        assert len(resp.items) == 1
        assert resp.items[0].status == "failed"
        assert resp.items[0].error == "something went wrong"

# Made with Bob
