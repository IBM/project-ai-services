"""
Inference request validation for the Forecast service.
"""

import math
from typing import Any

from common.error_utils import APIError, ErrorCode


def validate_inference_instances(
    instances: list[dict[str, Any]], input_columns: list[dict], max_rows: int = 5000
) -> None:
    """Validate inference instances against the connector's input columns schema.

    Raises APIError with INVALID_REQUEST (400) if validation fails.
    """
    if not isinstance(instances, list):
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            "Request body must be a JSON array of row objects.",
        )

    if len(instances) == 0:
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            "The request body must contain at least one row.",
        )

    if len(instances) > max_rows:
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            f"Request row count ({len(instances)}) exceeds maximum allowed inference limit of {max_rows} rows.",
        )

    expected_names = {col["name"] for col in input_columns}
    type_map = {col["name"]: col.get("type") for col in input_columns}

    for i, row in enumerate(instances):
        if not isinstance(row, dict):
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Row {i} is not a valid JSON object.",
            )

        row_keys = set(row.keys())
        missing = expected_names - row_keys
        unknown = row_keys - expected_names

        if missing:
            missing_str = ", ".join(sorted(missing))
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Row {i} is missing required column(s): {missing_str}",
            )

        if unknown:
            unknown_str = ", ".join(sorted(unknown))
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Row {i} contains unknown column(s): {unknown_str}",
            )

        for col_name, val in row.items():
            if val is None:
                APIError.raise_error(
                    ErrorCode.INVALID_REQUEST,
                    f"Row {i} column '{col_name}' cannot be null.",
                )

            expected_type = type_map.get(col_name)
            if expected_type == "string":
                if not isinstance(val, str):
                    APIError.raise_error(
                        ErrorCode.INVALID_REQUEST,
                        f"Row {i} column '{col_name}' expected string, got {type(val).__name__}.",
                    )
            elif expected_type == "integer":
                if (
                    isinstance(val, bool)
                    or not isinstance(val, int)
                    or not -(2**63) <= val < 2**63
                ):
                    val_repr = str(val)
                    if len(val_repr) > 50:
                        val_repr = val_repr[:47] + "..."
                    APIError.raise_error(
                        ErrorCode.INVALID_REQUEST,
                        f"Row {i} column '{col_name}' expected integer (64-bit integer), got {val_repr}.",
                    )
            elif expected_type == "number":
                if (
                    isinstance(val, bool)
                    or not isinstance(val, (int, float))
                    or abs(val) > 3.4028235e38
                    or not math.isfinite(val)
                ):
                    val_repr = str(val)
                    if len(val_repr) > 50:
                        val_repr = val_repr[:47] + "..."
                    APIError.raise_error(
                        ErrorCode.INVALID_REQUEST,
                        f"Row {i} column '{col_name}' must be a finite number within single-precision range, got {val_repr}.",
                    )
            elif expected_type is None:
                if isinstance(val, (dict, list, bool)):
                    APIError.raise_error(
                        ErrorCode.INVALID_REQUEST,
                        f"Row {i} column '{col_name}' cannot be complex JSON type {type(val).__name__}.",
                    )

