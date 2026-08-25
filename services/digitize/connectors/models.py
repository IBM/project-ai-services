"""
Pydantic request/response models for the connector REST API.

Covers:
  POST /v1/connectors
  PUT  /v1/connectors/{connector_id}
  GET  /v1/connectors
  GET  /v1/connectors/{connector_id}
  GET  /v1/connectors/{connector_id}/syncs
  GET  /v1/connectors/{connector_id}/syncs/{sync_seq}
"""

import uuid
from enum import Enum
from typing import Any, Dict, List, Optional
from pydantic import BaseModel, Field, field_validator


# ---------------------------------------------------------------------------
# Connector / sync-log status constants
# ---------------------------------------------------------------------------

class ConnectorStatus(str, Enum):
    """String enum for the Connector.sync_status column.

    Inherits from str so values can be passed directly to SQLAlchemy and
    compared with raw DB strings without calling .value.

    Lifecycle:
        UP_TO_DATE ──► SYNCING       ──► UP_TO_DATE   (tick completed cleanly)
                           └──► OUT_OF_SYNC            (tick finished with errors)
        UP_TO_DATE ──► DELETE_PENDING                  (DELETE, no active sync)
        SYNCING    ──► DELETE_PENDING                  (DELETE arrived mid-sync)
        SYNCING    ──► OUT_OF_SYNC                     (cancel honoured; finalize_sync_log_and_update_connector
                                                        reverts connector after CANCELLED)
    """

    UP_TO_DATE = "up to date"
    SYNCING = "syncing"
    OUT_OF_SYNC = "out of sync"
    DELETE_PENDING = "delete pending"


class SyncLogStatus(str, Enum):
    """String enum for the ConnectorSyncLog.status column.

    Inherits from str so values can be passed directly to SQLAlchemy and
    compared with raw DB strings without calling .value.

    Lifecycle:
        STARTED ──► CANCEL_PENDING ──► CANCELLED  (cancel-sync request received mid-tick;
                │                                   finalize_sync_log_and_update_connector sets connector OUT_OF_SYNC)
                ├──► COMPLETED                    (all files processed successfully)
                ├──► FAILED                       (fatal tick error or partial failure)
                └──► CANCELLED                    (tick interrupted by DELETE_PENDING)

    Note: CANCEL_PENDING is written to connector_sync_logs.status (not to connectors).
    The connector stays SYNCING while the tick winds down; finalize_sync_log_and_update_connector() transitions
    it to OUT_OF_SYNC when it writes the terminal CANCELLED status.
    """

    STARTED = "started"
    CANCEL_PENDING = "cancel pending"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"


# ---------------------------------------------------------------------------
# Error message constants
# ---------------------------------------------------------------------------

class ConnectorError(str, Enum):
    """String enum of well-known error messages stored on the Connector row.

    Inherits from str so values can be compared directly against DB strings.
    """

    CREDENTIAL_ERROR_MSG = "Authentication failed: unable to connect with the provided credentials"
    """Written by run_tick when scanner.connect() raises a ConnectionError.

    Cleared automatically when a subsequent sync tick connects successfully
    (finalize_sync_log_and_update_connector with COMPLETED status sets error=None).
    """

# ---------------------------------------------------------------------------
# Connector type enum
# ---------------------------------------------------------------------------

class ConnectorType(str, Enum):
    """Allowed connector transport types.

    Inherits from str so values can be passed directly to SQLAlchemy and
    compared with raw DB strings without calling .value.
    """

    FILE_SYSTEM = "file_system"
    OBJECT_STORAGE = "object_storage"


# ---------------------------------------------------------------------------
# Request models
# ---------------------------------------------------------------------------

class ConnectorCreateRequest(BaseModel):
    """Body accepted by POST /v1/connectors."""

    id: Optional[str] = Field(
        None,
        description=(
            "Stable catalog UUID for this connector. "
            "If omitted, a UUID v4 is generated automatically."
        ),
    )
    name: str = Field(..., description="Human-readable unique name, e.g. 'prod-sftp-reports'")
    type: str = Field(..., description="Connector transport type: 'file_system' or 'object_storage'")
    allowed_extensions: List[str] = Field(..., description="File extensions to accept, e.g. ['.pdf', '.docx']")
    connection_details: Dict[str, Any] = Field(..., description="Transport-specific connection parameters")

    model_config = {
        "json_schema_extra": {
            "example": {
                "name": "prod-sftp-reports",
                "type": "file_system",
                "allowed_extensions": [".pdf", ".docx"],
                "connection_details": {
                    "host": "sftp.example.com",
                    "port": 22,
                    "username": "sync_user",
                    "password": "secret_password",
                    "remote_path": "/exports",
                },
            }
        }
    }

    @field_validator("allowed_extensions")
    @classmethod
    def validate_allowed_extensions(cls, v: List[str]) -> List[str]:
        bad_format = [e for e in v if not e.startswith(".")]
        if bad_format:
            raise ValueError(
                f"Each extension must start with '.', got: {bad_format!r}. "
                f"Use '.pdf' not 'pdf'."
            )
        normalised = [e.lower() for e in v]
        supported = {".pdf", ".docx"}
        unsupported = [e for e in normalised if e not in supported]
        if unsupported:
            raise ValueError(
                f"Unsupported extension(s): {unsupported!r}. "
                f"Supported extensions: {sorted(supported)}"
            )
        return normalised

    @field_validator("type", mode="before")
    @classmethod
    def validate_type(cls, v: str) -> str:
        allowed = {t.value for t in ConnectorType}
        if v not in allowed:
            raise ValueError(
                f"Invalid connector type {v!r}. Allowed values: {sorted(allowed)}"
            )
        return v

    @field_validator("id", mode="before")
    @classmethod
    def validate_id(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return v
        try:
            uuid.UUID(str(v))
        except ValueError:
            raise ValueError(f"id must be a valid UUID, got {v!r}")
        return str(v)


class ConnectorUpdateRequest(BaseModel):
    """Body accepted by PUT /v1/connectors/{connector_id}.

    All fields are optional — only supplied fields are written.
    connection_details is merged at the key level, not replaced wholesale.
    type and sync_interval_seconds cannot be changed via this endpoint.
    """

    name: Optional[str] = Field(None, description="New human-readable name (must be unique)")
    allowed_extensions: Optional[List[str]] = Field(None, description="Replacement allowed-extensions list")
    connection_details: Optional[Dict[str, Any]] = Field(
        None,
        description="Partial connection details — only supplied keys are overwritten",
    )

    model_config = {
        "json_schema_extra": {
            "example": {
                "name": "prod-sftp-reports-updated",
                "allowed_extensions": [".pdf", ".docx"],
                "connection_details": {
                    "remote_path": "/exports/v2",
                },
            }
        }
    }


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------

class ConnectorCreateResponse(BaseModel):
    """Response body for POST /v1/connectors (202 Accepted)."""

    id: str = Field(..., description="Unique identifier of the created connector")

    model_config = {
        "json_schema_extra": {
            "example": {
                "id": "c1d2e3f4-a5b6-7890-abcd-ef1234567890"
            }
        }
    }


class ConnectorListItem(BaseModel):
    """One connector in GET /v1/connectors list."""

    id: str
    name: str
    type: str
    attached_at: Optional[str]
    last_sync_at: Optional[str]
    sync_status: str
    error: Optional[str]
    total_files: int

    model_config = {
        "json_schema_extra": {
            "example": {
                "id": "c1d2e3f4-a5b6-7890-abcd-ef1234567890",
                "name": "prod-sftp-reports",
                "type": "file_system",
                "attached_at": "2025-01-15T10:00:00Z",
                "last_sync_at": "2025-01-15T10:30:00Z",
                "sync_status": "up to date",
                "error": None,
                "total_files": 15,
            }
        }
    }


class ConnectorDetailResponse(BaseModel):
    """Single connector returned by GET /v1/connectors/{connector_id}."""

    id: str
    name: str
    type: str
    allowed_extensions: List[str]
    sync_interval_seconds: int
    attached_at: Optional[str]
    last_sync_at: Optional[str]
    sync_status: str
    error: Optional[str]
    connection_details: Dict[str, Any]
    total_files: int

    model_config = {
        "json_schema_extra": {
            "example": {
                "id": "c1d2e3f4-a5b6-7890-abcd-ef1234567890",
                "name": "prod-sftp-reports",
                "type": "file_system",
                "allowed_extensions": [".pdf", ".docx"],
                "sync_interval_seconds": 60,
                "attached_at": "2025-01-15T10:00:00Z",
                "last_sync_at": "2025-01-15T10:30:00Z",
                "sync_status": "up to date",
                "error": None,
                "connection_details": {
                    "host": "sftp.example.com",
                    "port": 22,
                    "username": "sync_user",
                    "remote_path": "/exports",
                },
                "total_files": 15,
            }
        }
    }


class SyncLogItem(BaseModel):
    """One tick entry in GET /v1/connectors/{connector_id}/syncs."""

    seq: int
    started_at: str
    finished_at: Optional[str]
    total_files: int
    new_files: int
    removed_files: int
    status: str
    error: str

    model_config = {
        "json_schema_extra": {
            "example": {
                "seq": 1,
                "started_at": "2025-01-15T10:30:00Z",
                "finished_at": "2025-01-15T10:30:15Z",
                "total_files": 15,
                "new_files": 3,
                "removed_files": 0,
                "status": "completed",
                "error": "",
            }
        }
    }


class SyncLogResponse(BaseModel):
    """Paginated response for GET /v1/connectors/{connector_id}/syncs."""

    total: int
    limit: int
    offset: int
    items: List[SyncLogItem]

    model_config = {
        "json_schema_extra": {
            "example": {
                "total": 1,
                "limit": 50,
                "offset": 0,
                "items": [
                    {
                        "seq": 1,
                        "started_at": "2025-01-15T10:30:00Z",
                        "finished_at": "2025-01-15T10:30:15Z",
                        "total_files": 15,
                        "new_files": 3,
                        "removed_files": 0,
                        "status": "completed",
                        "error": "",
                    }
                ],
            }
        }
    }


class SyncLogDetailResponse(BaseModel):
    """Single sync-log item returned by GET /v1/connectors/{connector_id}/syncs/{sync_seq}."""

    seq: int
    started_at: str
    finished_at: Optional[str]
    total_files: int
    new_files: int
    removed_files: int
    status: str
    error: str

    model_config = {
        "json_schema_extra": {
            "example": {
                "seq": 1,
                "started_at": "2025-01-15T10:30:00Z",
                "finished_at": "2025-01-15T10:30:15Z",
                "total_files": 15,
                "new_files": 3,
                "removed_files": 0,
                "status": "completed",
                "error": "",
            }
        }
    }


class SyncTriggerResponse(BaseModel):
    """Response body for POST /v1/connectors/{connector_id}/sync."""

    sync_seq: int = Field(..., description="Sequence number of the active or newly-started sync")

    model_config = {
        "json_schema_extra": {
            "example": {
                "sync_seq": 1
            }
        }
    }

# Made with Bob
