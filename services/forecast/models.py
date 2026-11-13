
from typing import Any, Literal, Optional
from uuid import UUID

from pydantic import ConfigDict, Field, model_validator
from pydantic.main import BaseModel

# SQL identifier patterns: allows letters, numbers, underscores, and common SQL/Db2 special chars ($, #, @).
# Table can optionally be schema-qualified (e.g. "SCHEMA.TABLE" or just "TABLE").
_SQL_IDENTIFIER_PATTERN = r"^[A-Za-z_][A-Za-z0-9_$#@]{0,127}$"
_SQL_TABLE_PATTERN = r"^(?:[A-Za-z_][A-Za-z0-9_$#@]{0,127}\.)?[A-Za-z_][A-Za-z0-9_$#@]{0,127}$"

Schedule = Literal["hourly", "daily", "weekly", "monthly", "yearly"]
ColumnType = Literal["string", "integer", "number"]

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


class TrainingInputColumn(BaseModel):
    """An input column used for model training."""

    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1, max_length=128, pattern=_SQL_IDENTIFIER_PATTERN)
    type: Optional[ColumnType] = None


class TrainingRequest(BaseModel):
    """Training configuration for a connector."""

    model_config = ConfigDict(extra="forbid")

    table: str = Field(min_length=1, max_length=257, pattern=_SQL_TABLE_PATTERN)
    target_column: str = Field(min_length=1, max_length=128, pattern=_SQL_IDENTIFIER_PATTERN)
    input_columns: list[TrainingInputColumn] = Field(min_length=1, max_length=1000)

    @model_validator(mode="after")
    def _validate_columns(self):
        names = [col.name.upper() for col in self.input_columns]
        if len(set(names)) != len(names):
            raise ValueError("input_columns must not contain duplicate column names")
        if self.target_column.upper() in names:
            raise ValueError("target_column must not be present in input_columns")
        return self


class ConnectorCreateRequest(BaseModel):
    """Payload to create a new database connector."""

    model_config = ConfigDict(extra="forbid")

    id: UUID
    name: str = Field(min_length=1, max_length=255)
    type: Literal["db2_for_i"]
    schedule: Optional[Schedule] = None
    connection_details: Db2ForIDetails
    training: TrainingRequest


class ConnectorUpdateRequest(BaseModel):
    """Payload to update an existing database connector."""

    model_config = ConfigDict(extra="forbid")

    name: Optional[str] = Field(default=None, min_length=1, max_length=255)
    connection_details: Optional[Db2ForIDetailsPatch] = None

