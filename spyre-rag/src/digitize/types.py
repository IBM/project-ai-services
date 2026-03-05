from enum import Enum


class OutputFormat(str, Enum):
    TEXT = "text"
    MD = "md"
    JSON = "json"


class OperationType(str, Enum):
    INGESTION = "ingestion"
    DIGITIZATION = "digitization"


class JobStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"


class DocStatus(str, Enum):
    ACCEPTED = "accepted"
    IN_PROGRESS = "in_progress"
    DIGITIZED = "digitized"
    PROCESSED = "processed"
    CHUNKED = "chunked"
    COMPLETED = "completed"
    FAILED = "failed"
