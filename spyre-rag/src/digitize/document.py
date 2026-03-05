import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import LiteralString, Optional

from digitize.types import DocStatus, OutputFormat


CACHE_DIR = "/var/cache"
DOCS_DIR: LiteralString = f"{CACHE_DIR}/docs"

@dataclass
class TimingInfo:
    """Holds stage-wise processing durations (in seconds) for a document."""
    digitizing: Optional[float] = None
    processing: Optional[float] = None
    chunking: Optional[float] = None
    indexing: Optional[float] = None

    def to_dict(self) -> dict:
        return {
            "digitizing": self.digitizing,
            "processing": self.processing,
            "chunking": self.chunking,
            "indexing": self.indexing,
        }


@dataclass
class DocumentMetadata:
    """
    Represents the metadata for a single document being processed.
    Persisted as <doc_id>_metadata.json under DOCS_DIR.
    """
    id: str
    name: str
    type: str
    status: DocStatus = DocStatus.ACCEPTED
    output_format: OutputFormat = OutputFormat.JSON
    submitted_at: Optional[str] = None
    completed_at: Optional[str] = None
    error: Optional[str] = None
    job_id: Optional[str] = None
    metadata: dict = field(default_factory=dict)

    def to_dict(self) -> dict:
        """Serialize the document metadata to a JSON-compatible dictionary."""
        return {
            "id": self.id,
            "name": self.name,
            "type": self.type,
            "status": self.status.value if hasattr(self.status, "value") else self.status,
            "output_format": self.output_format.value if hasattr(self.output_format, "value") else self.output_format,
            "submitted_at": self.submitted_at,
            "completed_at": self.completed_at,
            "error": self.error,
            "job_id": self.job_id,
            "metadata": self.metadata,
        }

    def save(self, docs_dir: str = DOCS_DIR) -> Path:
        """
        Persist the document metadata as <doc_id>_metadata.json.

        Args:
            docs_dir: Directory where the metadata file will be written.

        Returns:
            Path to the written metadata file.
        """
        Path(docs_dir).mkdir(parents=True, exist_ok=True)
        meta_path = Path(docs_dir) / f"{self.id}_metadata.json"
        with open(meta_path, "w") as f:
            json.dump(self.to_dict(), f, indent=4)
        return meta_path

    def job_summary(self) -> dict:
        """
        Returns a summary dictionary suitable for embedding inside a job status file.
        """
        return {
            "id": self.id,
            "name": self.name,
            "status": self.status.value if hasattr(self.status, "value") else self.status,
        }
