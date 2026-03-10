from pathlib import Path
from typing import Optional, Dict, Any

from pydantic import BaseModel, Field, field_validator

from digitize.config import DOCS_DIR
from digitize.types import DocStatus, OutputFormat


class TimingInfo(BaseModel):
    """Holds stage-wise processing durations (in seconds) for a document."""
    digitizing: Optional[float] = None
    processing: Optional[float] = None
    chunking: Optional[float] = None
    indexing: Optional[float] = None

    class Config:
        """Pydantic configuration."""
        use_enum_values = True


class DocumentMetadata(BaseModel):
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
    metadata: Dict[str, Any] = Field(default_factory=dict)

    @field_validator('status', mode='before')
    @classmethod
    def validate_status(cls, v):
        """Convert string to DocStatus enum, default to ACCEPTED if invalid."""
        if isinstance(v, DocStatus):
            return v
        try:
            return DocStatus(v)
        except (ValueError, TypeError):
            return DocStatus.ACCEPTED

    @field_validator('output_format', mode='before')
    @classmethod
    def validate_output_format(cls, v):
        """Convert string to OutputFormat enum, default to JSON if invalid."""
        if isinstance(v, OutputFormat):
            return v
        try:
            return OutputFormat(v)
        except (ValueError, TypeError):
            return OutputFormat.JSON

    class Config:
        """Pydantic configuration."""
        use_enum_values = True

    def to_dict(self) -> dict:
        """
        Serialize the document metadata to a JSON-compatible dictionary.
        
        Returns:
            Dictionary representation of the document metadata
        """
        return self.dict()

    def save(self, docs_dir: Path = DOCS_DIR) -> Path:
        """
        Persist the document metadata as <doc_id>_metadata.json.

        Args:
            docs_dir: Directory where the metadata file will be written.

        Returns:
            Path to the written metadata file.
        """
        docs_dir.mkdir(parents=True, exist_ok=True)
        meta_path = docs_dir / f"{self.id}_metadata.json"
        with open(meta_path, "w", encoding="utf-8") as f:
            f.write(self.model_dump_json(indent=4))
        return meta_path

    def job_summary(self) -> dict:
        """
        Returns a summary dictionary suitable for embedding inside a job status file.
        """
        return {
            "id": self.id,
            "name": self.name,
            "status": self.status.value if isinstance(self.status, DocStatus) else self.status,
        }
