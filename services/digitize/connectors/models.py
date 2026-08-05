"""
Pydantic request/response models for the connector REST API.

Covers:
  POST /v1/connectors
  PUT  /v1/connectors/{connector_id}
  GET  /v1/connectors
  GET  /v1/connectors/{connector_id}
  GET  /v1/connectors/{connector_id}/syncs
"""

from typing import Any, Dict, List, Optional
from pydantic import BaseModel, Field


# ---------------------------------------------------------------------------
# Connector / sync-log status constants
# ---------------------------------------------------------------------------

class SyncStatus:
    """String constants for connector sync_status and sync-log status columns.

    Connector.sync_status lifecycle:
        IDLE  ──► SYNCING  ──► IDLE        (tick completed cleanly)
                          └──► OUT_OF_SYNC (tick finished with errors)

    ConnectorSyncLog.status lifecycle:
        STARTED ──► COMPLETED  (all files processed successfully)
                └──► FAILED    (fatal tick error or partial failure)
    """

    # Connector.sync_status values
    IDLE = "up to date"
    SYNCING = "syncing"
    OUT_OF_SYNC = "out of sync"

    # ConnectorSyncLog.status values
    STARTED = "started"
    COMPLETED = "completed"
    FAILED = "failed"


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

    id: int
    started_at: str
    finished_at: Optional[str]
    total_files: int
    new_files: int
    removed_files: int
    failed_files: int
    status: str
    error: str


class SyncLogResponse(BaseModel):
    """Paginated response for GET /v1/connectors/{connector_id}/syncs."""

    total: int
    limit: int
    offset: int
    items: List[SyncLogItem]

# Made with Bob
