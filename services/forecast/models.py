"""Request and response models of the Forecast service API."""

from typing import Annotated, Any, Literal, Optional, Union
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, RootModel, StringConstraints, model_validator

# SQL identifier patterns: allows letters, numbers, underscores, and common SQL/Db2 special chars ($, #, @).
# Table can optionally be schema-qualified (e.g. "SCHEMA.TABLE" or just "TABLE").
_SQL_IDENTIFIER_PATTERN = r"^[A-Za-z_][A-Za-z0-9_$#@]{0,127}$"
_SQL_TABLE_PATTERN = r"^(?:[A-Za-z_][A-Za-z0-9_$#@]{0,127}\.)?[A-Za-z_][A-Za-z0-9_$#@]{0,127}$"

Schedule = Literal["hourly", "daily", "weekly", "monthly", "yearly"]

# ---------------------------------------------------------------------------
# Request models
# ---------------------------------------------------------------------------


class Db2ForIDetails(BaseModel):
    """connection_details of a db2_for_i connector. Unknown fields are rejected."""

    model_config = ConfigDict(extra="forbid")

    host: str = Field(min_length=1, max_length=255, pattern=r"^[A-Za-z0-9._:\-]+$")
    port: Optional[int] = Field(default=None, ge=1, le=65535)
    database: str = Field(min_length=1, max_length=128)
    username: str = Field(min_length=1, max_length=128)
    password: str = Field(min_length=1, max_length=1024)


class Db2ForIDetailsPatch(BaseModel):
    """Partial connection_details for PUT. Only the supplied keys are changed.

    Values are not typed here: the merged details are validated by Db2ForIDetails,
    so every invalid value is answered with the same 400.
    """

    model_config = ConfigDict(extra="forbid")

    host: Any = None
    port: Any = None
    database: Any = None
    username: Any = None
    password: Any = None


# The name of an input column. Its type is not given: the forecaster reads every input as text.
InputColumn = Annotated[str, StringConstraints(min_length=1, max_length=128, pattern=_SQL_IDENTIFIER_PATTERN)]


class ServiceSettings(BaseModel):
    """Settings chosen when the service is deployed: the training data and the training schedule.

    Unknown fields are ignored: the catalog forwards every setting of the deployed service,
    and the service cannot tell which of them are meant for the connector.
    """

    model_config = ConfigDict(extra="ignore")

    table: str = Field(min_length=1, max_length=257, pattern=_SQL_TABLE_PATTERN)
    target_column: str = Field(min_length=1, max_length=128, pattern=_SQL_IDENTIFIER_PATTERN)
    input_columns: list[InputColumn] = Field(min_length=1, max_length=1000)
    schedule: Optional[Schedule] = None

    @model_validator(mode="after")
    def _validate_columns(self) -> "ServiceSettings":
        """Reject duplicate input column names and a target that is also an input column."""
        names = [name.upper() for name in self.input_columns]
        if len(set(names)) != len(names):
            raise ValueError("input_columns must not contain duplicate column names")
        if self.target_column.upper() in names:
            raise ValueError("target_column must not be present in input_columns")
        return self


class ForecastRequest(RootModel[list[dict[str, Any]]]):
    """Request body of POST /v1/forecast: a JSON array with one object per row to forecast."""


class ConnectorCreateRequest(BaseModel):
    """Payload to create a new database connector."""

    model_config = ConfigDict(extra="forbid")

    id: UUID
    name: str = Field(min_length=1, max_length=255)
    type: Literal["db2_for_i"]
    connection_details: Db2ForIDetails
    service_settings: ServiceSettings


class ConnectorUpdateRequest(BaseModel):
    """Payload to update an existing database connector."""

    model_config = ConfigDict(extra="forbid")

    name: Optional[str] = Field(default=None, min_length=1, max_length=255)
    connection_details: Optional[Db2ForIDetailsPatch] = None


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------


class ConnectorCreateResponse(BaseModel):
    """Response of POST /v1/connectors."""

    id: str


class Pagination(BaseModel):
    """Paging details of a list response."""

    total: int
    limit: int
    offset: int


class ConnectorListItem(BaseModel):
    """One connector in the list response. Timestamps are UTC ISO 8601 strings."""

    id: str
    name: str
    type: str
    created_at: Optional[str] = None
    last_trained_at: Optional[str] = None
    status: str
    table: Optional[str] = None
    target_column: Optional[str] = None
    message: str = ""


class ConnectorListResponse(BaseModel):
    """Response of GET /v1/connectors."""

    pagination: Pagination
    data: list[ConnectorListItem]


class ConnectorResponse(BaseModel):
    """Response of GET /v1/connectors/{connector_id}."""

    id: str
    name: str
    type: str
    table: Optional[str] = None
    target_column: Optional[str] = None
    # Not typed as Schedule: the database compares the value in lower case, so a
    # stored "Daily" must still be returned and not turn into a 500.
    schedule: Optional[str] = None
    # Only the safe fields of the connector type. The fields depend on the type,
    # so they are not modelled here, and secrets are never part of it.
    connection_details: dict[str, Any]
    status: str
    last_trained_at: Optional[str] = None
    message: str = ""
    created_at: Optional[str] = None
    updated_at: Optional[str] = None


class ConnectorVersions(BaseModel):
    """Forecaster versions of a connector."""

    ready: list[int]


class TrainingSummary(BaseModel):
    """Summary of the retained training jobs of a connector."""

    jobs_completed: int
    jobs_failed: int
    last_job_status: Optional[str] = None
    last_duration_seconds: Optional[int | float] = None
    last_completed_at: Optional[str] = None


class JobResult(BaseModel):
    """Known keys of jobs.result. Any other key is left out of responses."""

    rows_read: Optional[int] = None
    algorithm: Optional[str] = None
    training_seconds: Optional[int | float] = None
    attempts: Optional[int] = None


class JobItem(BaseModel):
    """One training job in the metrics response."""

    seq: int
    status: str
    created_at: Optional[str] = None
    started_at: Optional[str] = None
    finished_at: Optional[str] = None
    message: str = ""
    # Keys can be missing while a job runs or after it fails.
    result: Optional[JobResult] = None


class ConnectorMetricsResponse(BaseModel):
    """Response of GET /v1/connectors/{connector_id}/metrics (developer use only)."""

    connector_id: str
    versions: ConnectorVersions
    training: TrainingSummary
    jobs: list[JobItem]


class ForecasterItem(BaseModel):
    """One forecaster in a list response."""

    version: int
    metrics: Optional[dict[str, Any]] = None
    created_at: Optional[str] = None


class Prediction(BaseModel):
    """Prediction and confidence for one input row."""

    prediction: Union[str, int, float, bool]
    confidence: float = Field(ge=0.0, le=1.0)


class ForecastResponse(BaseModel):
    """Forecast answer: the version that responded and one result per row."""

    version: int
    predictions: list[Prediction]
