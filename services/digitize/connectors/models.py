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
# Request models
# ---------------------------------------------------------------------------

class ConnectorCreateRequest(BaseModel):
    """Body accepted by POST /v1/connectors."""

    connector_id: Optional[str] = Field(
        None,
        description=(
            "Stable catalog UUID for this connector. "
            "If omitted, a UUID v4 is generated automatically."
        ),
    )
    connector_name: str = Field(..., description="Human-readable unique name, e.g. 'prod-sftp-reports'")
    type: str = Field(..., description="Connector transport type: 'ssh' or 's3'")
    allowed_extensions: List[str] = Field(..., description="File extensions to accept, e.g. ['.pdf', '.docx']")
    connection_details: Dict[str, Any] = Field(..., description="Transport-specific connection parameters")

    @field_validator("connector_id", mode="before")
    @classmethod
    def validate_connector_id(cls, v: Optional[str]) -> Optional[str]:
        if v is None:
            return v
        try:
            uuid.UUID(str(v))
        except ValueError:
            raise ValueError(f"connector_id must be a valid UUID, got {v!r}")
        return str(v)


class ConnectorUpdateRequest(BaseModel):
    """Body accepted by PUT /v1/connectors/{connector_id}.

    All fields are optional — only supplied fields are written.
    connection_details is merged at the key level, not replaced wholesale.
    type and sync_interval_seconds cannot be changed via this endpoint.
    """

    connector_name: Optional[str] = Field(None, description="New human-readable name (must be unique)")
    allowed_extensions: Optional[List[str]] = Field(None, description="Replacement allowed-extensions list")
    connection_details: Optional[Dict[str, Any]] = Field(
        None,
        description="Partial connection details — only supplied keys are overwritten",
    )


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------

class ConnectorListItem(BaseModel):
    """One connector in GET /v1/connectors list."""

    connector_id: str
    connector_name: str
    type: str
    attached_at: Optional[str]
    last_sync_at: Optional[str]
    sync_status: str
    last_sync_error: Optional[str]
    error: Optional[str]
    total_files: int


class ConnectorDetailResponse(BaseModel):
    """Single connector returned by GET /v1/connectors/{connector_id}."""

    connector_id: str
    connector_name: str
    type: str
    allowed_extensions: List[str]
    sync_interval_seconds: int
    attached_at: Optional[str]
    last_sync_at: Optional[str]
    sync_status: str
    last_sync_error: Optional[str]
    error: Optional[str]
    connection_details: Dict[str, Any]
    total_files: int


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


class SyncLogResponse(BaseModel):
    """Paginated response for GET /v1/connectors/{connector_id}/syncs."""

    total: int
    limit: int
    offset: int
    items: List[SyncLogItem]


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


class SyncTriggerResponse(BaseModel):
    """Response body for POST /v1/connectors/{connector_id}/sync."""

    sync_seq: int = Field(..., description="Sequence number of the active or newly-started sync")

# Made with Bob
