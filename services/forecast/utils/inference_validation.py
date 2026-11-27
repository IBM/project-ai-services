"""
Inference request validation for the Forecast service.
"""

from typing import Any

from common.error_utils import APIError, ErrorCode


def validate_inference_instances(
    instances: list[dict[str, Any]], input_columns: list[str], max_rows: int = 5000
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

    expected_names = set(input_columns)

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

            if isinstance(val, (dict, list, bool)):
                APIError.raise_error(
                    ErrorCode.INVALID_REQUEST,
                    f"Row {i} column '{col_name}' cannot be complex JSON type {type(val).__name__}.",
                )
