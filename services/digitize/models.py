from enum import Enum
from typing import List, Optional, Dict, Any, Union
from pydantic import BaseModel, Field, field_validator, model_validator, ConfigDict


class OutputFormat(str, Enum):
    TEXT = "txt"
    MD = "md"
    JSON = "json"


class OperationType(str, Enum):
    INGESTION = "ingestion"
    DIGITIZATION = "digitization"


class JobStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    COMPLETED_WITH_ERRORS = "completed_with_errors"
    FAILED = "failed"
    CANCEL_PENDING = "cancel_pending"
    CANCELLED = "cancelled"


class DocStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    DIGITIZED = "digitized"
    PROCESSED = "processed"
    CHUNKED = "chunked"
    COMPLETED = "completed"
    COMPLETED_WITH_ERRORS = "completed_with_errors"
    FAILED = "failed"
    ALREADY_EXISTS = "already_exists"
    CANCELLED = "cancelled"


class AlreadyExistsFile(BaseModel):
    """Describes a single file that was skipped because it already exists."""
    filename: str = Field(..., description="Original filename of the skipped file")
    existing_doc_id: str = Field(..., description="doc_id of the already-ingested document")
    existing_doc_name: str = Field(..., description="Name of the already-ingested document")
    file_hash: str = Field(..., description="MD5 hash that matched.")


class PaginationInfo(BaseModel):
    total: int
    limit: int
    offset: int

class JobsListResponse(BaseModel):
    pagination: PaginationInfo
    data: List[dict]

class JobCreatedResponse(BaseModel):
    """Response model for job creation."""
    job_id: str



class DocumentListItem(BaseModel):
    """Minimal document information for list responses."""
    id: str
    name: str
    type: str
    status: str
    submitted_at: Optional[str] = None


class DocumentsListResponse(BaseModel):
    """Response model for documents list endpoint with pagination."""
    pagination: PaginationInfo
    data: List[DocumentListItem]


class DocumentDetailResponse(BaseModel):
    """Detailed document information response."""
    id: str
    job_id: Optional[str] = None
    name: str
    type: str
    status: str
    output_format: str
    submitted_at: Optional[str] = None
    completed_at: Optional[str] = None
    error: Optional[str] = None
    metadata: Optional[Dict[str, Any]] = None
    duplicate_names: List[str] = Field(
        default_factory=list,
        description=(
            "Names of shadow 'already_exists' document entries that point to this "
            "document as their original. These duplicates will also be removed on delete."
        ),
    )


class DocumentContentResponse(BaseModel):
    """Document content response with format information."""
    result: Union[Dict[str, Any], str]
    output_format: str


class JobDocumentSummary(BaseModel):
    """Compact per-document entry for job status responses."""
    model_config = ConfigDict(use_enum_values=True)

    id: str
    name: str
    status: str
    message: Optional[str] = Field(
        default=None,
        description="Human-readable message providing additional context about the document status",
    )


class JobStats(BaseModel):
    """Statistics for documents in a job."""
    model_config = ConfigDict(use_enum_values=True)
    
    total_documents: int = Field(default=0, ge=0, description="Total number of documents")
    completed: int = Field(default=0, ge=0, description="Number of completed documents")
    failed: int = Field(default=0, ge=0, description="Number of failed documents")
    in_progress: int = Field(default=0, ge=0, description="Number of in-progress documents")


class JobState(BaseModel):
    """
    Represents the overall state of a job for API responses.

    This model is used to validate and serialize job data from the database.
    """
    model_config = ConfigDict(use_enum_values=True)
    
    job_id: str
    job_name: Optional[str] = None
    operation: str
    status: JobStatus
    submitted_at: str
    completed_at: Optional[str] = None
    documents: List[JobDocumentSummary] = Field(default_factory=list)
    stats: JobStats = Field(default_factory=JobStats)
    error: Optional[str] = None

    @field_validator('status', mode='before')
    @classmethod
    def validate_status(cls, v):
        """Convert string to JobStatus enum, default to ACCEPTED if invalid."""
        if isinstance(v, JobStatus):
            return v
        try:
            return JobStatus(v)
        except (ValueError, TypeError):
            return JobStatus.ACCEPTED

    @field_validator('documents', mode='before')
    @classmethod
    def validate_documents(cls, v):
        """Ensure documents is a list and filter out invalid entries."""
        if not isinstance(v, list):
            return []

        valid_docs = []
        for doc in v:
            if isinstance(doc, dict) and all(k in doc for k in ['id', 'name', 'status']):
                try:
                    valid_docs.append(JobDocumentSummary(**doc))
                except Exception:
                    continue
            elif isinstance(doc, JobDocumentSummary):
                valid_docs.append(doc)
        return valid_docs

    @field_validator('stats', mode='before')
    @classmethod
    def validate_stats(cls, v):
        """Ensure stats is valid, return default if not."""
        if isinstance(v, JobStats):
            return v
        if isinstance(v, dict):
            try:
                return JobStats(**v)
            except Exception:
                return JobStats()
        return JobStats()

    def to_dict(self) -> dict:
        """
        Serialize the job state to a JSON-compatible dictionary.

        Returns:
            Dictionary representation of the job state
        """
        return self.model_dump()


class ImportExportData(BaseModel):
    """Shared payload structure for import/export APIs."""
    jobs: List["ExportJobRecord"] = Field(default_factory=list)
    documents: List["ExportDocumentRecord"] = Field(default_factory=list)
    connectors: List["ExportConnectorRecord"] = Field(default_factory=list)
    sync_logs: List["ExportSyncLogRecord"] = Field(default_factory=list)


class ExportJobRecord(BaseModel):
    """Serializable job record for export/import APIs."""
    job_id: str
    operation: str
    status: str
    source: str = "user"
    job_name: Optional[str] = None
    submitted_at: str
    completed_at: Optional[str] = None
    stats: Dict[str, int] = Field(default_factory=dict)
    error: Optional[str] = None


class ExportDocumentRecord(BaseModel):
    """Serializable document record for export/import APIs."""
    id: str
    job_id: Optional[str] = None
    name: str
    type: str
    status: str
    source: str = "user"
    output_format: str
    submitted_at: str
    completed_at: Optional[str] = None
    error: Optional[str] = None
    metadata: Dict[str, Any] = Field(default_factory=dict)


class ExportConnectorRecord(BaseModel):
    """Serializable connector record for export/import APIs.

    connection_details is intentionally excluded — encrypted credentials
    cannot be round-tripped; connectors must be re-registered with fresh
    credentials after restore. The record carries all other config fields
    so the connector shell (id, name, type, extensions, interval, state)
    is preserved.
    """
    id: str
    name: str
    type: str
    allowed_extensions: List[str] = Field(default_factory=list)
    sync_interval_seconds: int = 300
    attached_at: str
    last_sync_at: Optional[str] = None
    status: str = "up to date"
    total_files: int = 0
    message: Optional[str] = None


class ExportSyncLogRecord(BaseModel):
    """Serializable connector sync-log record for export/import APIs."""
    connector_id: str
    seq: int
    started_at: str
    finished_at: Optional[str] = None
    total_files: int = 0
    new_files: int = 0
    completed_files: int = 0
    removed_files: int = 0
    status: str
    error: str = ""


class ImportRequest(BaseModel):
    """Request model for metadata import."""
    data: ImportExportData
    validate_only: bool = False

    @model_validator(mode="after")
    def validate_non_empty_payload(self):
        if (
            not self.data.jobs
            and not self.data.documents
            and not self.data.connectors
            and not self.data.sync_logs
        ):
            raise ValueError(
                "At least one job, document, connector, or sync_log record must be provided"
            )
        return self


class ImportRecordIssue(BaseModel):
    """Per-record warning or error returned by import API."""
    record_type: str
    record_id: str
    type: str
    message: str


class ImportEntitySummary(BaseModel):
    """Import summary for a single entity type."""
    total_received: int = 0
    imported: int = 0
    skipped: int = 0
    failed: int = 0


class ImportSummary(BaseModel):
    """Import summary grouped by entity type."""
    jobs: ImportEntitySummary
    documents: ImportEntitySummary
    connectors: ImportEntitySummary = Field(default_factory=ImportEntitySummary)
    sync_logs: ImportEntitySummary = Field(default_factory=ImportEntitySummary)


class ImportResponse(BaseModel):
    """Response model for metadata import."""
    status: str
    summary: ImportSummary
    duration_seconds: float
    errors: List[ImportRecordIssue] = Field(default_factory=list)
    warnings: List[ImportRecordIssue] = Field(default_factory=list)


class ExportEntitySummary(BaseModel):
    """Export summary for a single entity type."""
    total_exported: int = 0
    completed: int = 0
    failed: int = 0


class ExportSummary(BaseModel):
    """Export summary grouped by entity type."""
    jobs: ExportEntitySummary
    documents: ExportEntitySummary
    connectors: ExportEntitySummary = Field(default_factory=ExportEntitySummary)
    sync_logs: ExportEntitySummary = Field(default_factory=ExportEntitySummary)


class ExportPagination(BaseModel):
    """Pagination metadata for export API."""
    limit: int
    offset: int
    has_more: bool
    total_records: int
    returned_records: int


class ExportResponse(BaseModel):
    """Response model for metadata export."""
    status: str
    data: ImportExportData
    summary: ExportSummary
    export_timestamp: str
    duration_seconds: float
    pagination: ExportPagination


# Made with Bob
