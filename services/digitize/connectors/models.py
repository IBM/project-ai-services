"""
Pydantic request/response models for the connector REST API.

Covers:
  POST /v1/connectors
  PUT  /v1/connectors/{connector_id}
  GET  /v1/connectors
  GET  /v1/connectors/{connector_id}
  GET  /v1/connectors/{connector_id}/syncs
"""

from enum import Enum
from typing import Any, Dict, List, Optional
from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Connector / sync-log status constants
# ---------------------------------------------------------------------------

class SyncStatus(str, Enum):
    """String enum for connector sync_status and sync-log status columns.

    Inherits from str so values can be passed directly to SQLAlchemy and
    compared with raw DB strings without calling .value.

    Connector.sync_status lifecycle:
        UP_TO_DATE ──► SYNCING       ──► UP_TO_DATE   (tick completed cleanly)
                           └──► OUT_OF_SYNC            (tick finished with errors)
        UP_TO_DATE ──► DELETE_PENDING                  (DELETE, no active sync)
        SYNCING    ──► DELETE_PENDING                  (DELETE arrived mid-sync)
        SYNCING    ──► OUT_OF_SYNC                     (cancel honoured; close_sync_log
                                                        reverts connector after CANCELLED)

    ConnectorSyncLog.status lifecycle:
        STARTED ──► CANCEL_PENDING ──► CANCELLED  (cancel-sync request received mid-tick;
                │                                   close_sync_log sets connector OUT_OF_SYNC)
                ├──► COMPLETED                    (all files processed successfully)
                ├──► FAILED                       (fatal tick error or partial failure)
                └──► CANCELLED                    (tick interrupted by DELETE_PENDING)

    Note: CANCEL_PENDING is written to connector_sync_logs.status (not to connectors).
    The connector stays SYNCING while the tick winds down; close_sync_log() transitions
    it to OUT_OF_SYNC when it writes the terminal CANCELLED status.
    """

    # Connector.sync_status values
    UP_TO_DATE = "up to date"
    SYNCING = "syncing"
    OUT_OF_SYNC = "out of sync"
    DELETE_PENDING = "delete pending"

    # ConnectorSyncLog.status values
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

    connector_id: str = Field(..., description="Stable catalog UUID for this connector")
    connector_name: str = Field(..., description="Human-readable unique name, e.g. 'prod-sftp-reports'")
    type: str = Field(..., description="Connector transport type: 'ssh' or 's3'")
    allowed_extensions: List[str] = Field(..., description="File extensions to accept, e.g. ['.pdf', '.docx']")
    connection_details: Dict[str, Any] = Field(..., description="Transport-specific connection parameters")


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


class SyncTriggerResponse(BaseModel):
    """Response body for POST /v1/connectors/{connector_id}/sync."""

    sync_seq: int = Field(..., description="Sequence number of the active or newly-started sync")

# Made with Bob
