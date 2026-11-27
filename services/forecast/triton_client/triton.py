"""Triton helpers: startup checks and inference HTTP client."""

import math
from typing import Any, NoReturn
from urllib.parse import quote

import httpx

from common.error_utils import APIError, ErrorCode
from common.misc_utils import get_logger
from forecast.errors import raise_triton_unavailable
from forecast.settings import settings
from forecast.utils.http_client import get_client

logger = get_logger("triton_client")

_NOT_SERVED_MARKERS = (
    "unknown model",
    "not at ready state",
    "not ready",
    "is not ready",
    "model not found",
)


def triton_url_check() -> None:
    """Validate that TRITON_URL is set and uses an HTTP or HTTPS scheme.

    Raises RuntimeError when invalid. The value is not echoed because a URL can carry credentials.
    """
    url = settings.forecast.triton_url.strip()
    if not url:
        raise RuntimeError("TRITON_URL is not set")
    if not url.lower().startswith(("http://", "https://")):
        raise RuntimeError("TRITON_URL must start with http:// or https://")


def _bad_response(reason: str) -> NoReturn:
    """Log the reason and raise a generic 500 error; internal details never reach the client."""
    logger.error(f"Unusable Triton response: {reason}")
    APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, "Triton returned an unusable response.")


def _rows(output: dict, num_rows: int) -> list:
    """Split a flat row-major Triton tensor into one entry per input row."""
    if not isinstance(output, dict):
        _bad_response("output tensor is not an object")
    data = output.get("data")
    if not isinstance(data, list) or num_rows == 0 or len(data) % num_rows != 0:
        _bad_response(
            f"output {output.get('name')!r} has invalid data for {num_rows} rows"
        )
    shape = output.get("shape")
    if isinstance(shape, list) and len(shape) > 0 and shape[0] != num_rows:
        _bad_response(
            f"output {output.get('name')!r} shape[0]={shape[0]} does not match batch size {num_rows}"
        )
    width = len(data) // num_rows
    if width == 1:
        return list(data)
    return [data[i * width : (i + 1) * width] for i in range(num_rows)]


async def run_inference(
    model_name: str, instances: list[dict[str, Any]], input_columns: list[str]
) -> list[dict[str, Any]]:
    """Forward an inference request to Triton and parse the predictions and confidences.

    Calls POST /v2/models/<model_name>/infer on Triton.
    """
    triton_url = settings.forecast.triton_url.strip().rstrip("/")
    infer_url = f"{triton_url}/v2/models/{quote(model_name, safe='')}/infer"
    timeout = settings.forecast.triton_infer_timeout_s

    # Build Triton V2 HTTP request inputs. Every column is sent as text (BYTES). An input column
    # has no type, and the forecaster's inputs are all TYPE_STRING: its graph parses
    # a numeric column and looks up a category column itself (see the proposal, section 10.4).
    num_rows = len(instances)
    inputs_payload = [
        {
            "name": name,
            "shape": [num_rows, 1],
            "datatype": "BYTES",
            "data": [str(row[name]) for row in instances],
        }
        for name in input_columns
    ]

    payload = {"inputs": inputs_payload}

    try:
        response = await get_client().post(infer_url, json=payload, timeout=timeout)
    except (httpx.ConnectError, httpx.TimeoutException) as exc:
        logger.error(f"Triton unreachable for model {model_name}: {type(exc).__name__}")
        raise_triton_unavailable()
    except httpx.RequestError as exc:
        logger.error(f"Triton request failed for model {model_name}: {type(exc).__name__}")
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            "Triton inference failed.",
        )

    if response.status_code in (404, 503) or (
        response.status_code == 400
        and any(m in response.text.lower() for m in _NOT_SERVED_MARKERS)
    ):
        logger.error(
            f"Triton does not serve model {model_name} (HTTP {response.status_code}): {response.text[:200]}"
        )
        raise_triton_unavailable()

    if response.status_code != 200:
        logger.error(
            f"Triton returned HTTP {response.status_code} for {model_name}: {response.text[:200]}"
        )
        APIError.raise_error(
            ErrorCode.INTERNAL_SERVER_ERROR,
            "Triton inference failed.",
        )

    try:
        result_json = response.json()
    except Exception:
        _bad_response("response body is not valid JSON")

    if not isinstance(result_json, dict) or not isinstance(result_json.get("outputs"), list):
        _bad_response("response body is not a valid Triton V2 JSON payload")

    # Parse predictions and confidence scores
    output_map = {
        o["name"]: o
        for o in result_json["outputs"]
        if isinstance(o, dict) and "name" in o and isinstance(o.get("data"), list)
    }
    probs = output_map.get("probabilities") or output_map.get("probabilities_output")
    preds_out = output_map.get("predictions") or output_map.get("prediction")
    confs_out = output_map.get("confidences") or output_map.get("confidence")

    if probs is not None:
        prob_rows = _rows(probs, num_rows)
        if not all(isinstance(r, list) for r in prob_rows):
            _bad_response("probabilities output must carry one value per class")
        if not all(
            isinstance(v, (int, float))
            and not isinstance(v, bool)
            and math.isfinite(v)
            for r in prob_rows
            for v in r
        ):
            _bad_response("probabilities output must contain finite numbers")
        confs = [max(r) for r in prob_rows]
        if preds_out is None:
            _bad_response("probabilities output returned without class label predictions")
        preds = _rows(preds_out, num_rows)
    elif preds_out is not None and confs_out is not None:
        preds = _rows(preds_out, num_rows)
        confs = _rows(confs_out, num_rows)
    else:
        _bad_response(f"outputs {sorted(output_map.keys())} do not carry confidence scores")

    predictions = []
    for p, c in zip(preds, confs):
        if isinstance(c, bool) or not isinstance(c, (int, float)) or not math.isfinite(c) or not (0.0 <= c <= 1.000001):
            _bad_response("confidence score must be a finite number between 0 and 1")
        if p is None or isinstance(p, (list, dict)):
            _bad_response("prediction must be a scalar value")
        predictions.append({
            "prediction": p,
            "confidence": min(1.0, max(0.0, round(float(c), 4))),
        })

    return predictions
