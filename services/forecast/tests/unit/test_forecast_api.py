"""
Unit tests for the Forecast prediction and listing endpoints (/v1/forecast).
"""

import asyncio
from contextlib import ExitStack, contextmanager
from datetime import datetime, timezone
from types import SimpleNamespace
from unittest.mock import DEFAULT, MagicMock, patch
from uuid import UUID

import pytest
import httpx
from fastapi.testclient import TestClient
from pydantic import SecretStr

import forecast.api.v1.forecast as forecast_api
import forecast.app as forecast_app
import forecast.db.connectors as connectors_db
import forecast.utils.forecast_utils as forecast_utils
from forecast.db.connectors import ConnectorRow, ForecasterRow
from forecast.settings import settings

CONNECTOR_ID = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
KEY = "0123456789abcdef0123456789abcdef"
CREATED = datetime(2026, 10, 26, 9, 12, tzinfo=timezone.utc)


@pytest.fixture(autouse=True)
def _startup_dependencies_stubbed():
    """Stub DB encryption key and triton_url_check."""
    with patch.object(
        settings.forecast, "db_encryption_key", SecretStr(KEY)
    ), patch("forecast.app.triton_url_check", return_value=None):
        yield


@pytest.fixture
def client():
    with patch.object(forecast_app, "close_db_connections"):
        with TestClient(forecast_app.app) as test_client:
            yield test_client


@contextmanager
def _patched_db_session(*args, **kwargs):
    """Patch ``get_db_session`` where the endpoints and the helpers of forecast_utils use it."""
    with ExitStack() as stack:
        for module in (forecast_api, forecast_utils):
            stack.enter_context(patch.object(module, "get_db_session", *args, **kwargs))
        yield


@pytest.fixture
def db():
    """Replace database session and connector db queries with mocks."""
    session = MagicMock(name="session")

    @contextmanager
    def fake_session():
        yield session

    defaults = {
        "get_default_connector": None,
        "get_connector": None,
        "list_forecasters": [],
        "get_forecaster": None,
        "get_newest_forecaster": None,
    }

    with _patched_db_session(fake_session), patch.multiple(
        connectors_db, **{name: DEFAULT for name in defaults}
    ) as mocks:
        for name, value in defaults.items():
            mocks[name].return_value = value
        yield SimpleNamespace(session=session, **mocks)


def _assert_envelope(body: dict, code: str, status: int):
    assert set(body) == {"error"}
    assert body["error"]["code"] == code
    assert body["error"]["status"] == status
    assert isinstance(body["error"]["message"], str) and body["error"]["message"]


def _mock_connector(**overrides):
    fields = {
        "id": UUID(CONNECTOR_ID),
        "name": "sales-connector",
        "type": "db2_for_i",
        "status": "ACCEPTED",
        "message": None,
        "config": {},
        "source_table": "SALES",
        "target_column": "DELIVERY_LATE",
        "schedule": "daily",
        "input_columns": ["REGION", "QUANTITY", "UNIT_PRICE"],
        "created_at": CREATED,
        "updated_at": CREATED,
        "last_trained_at": CREATED,
    }
    fields.update(overrides)
    return ConnectorRow(**fields)


def _mock_forecaster(version=12, **overrides):
    fields = {
        "version": version,
        "metrics": {"metric": "accuracy", "validation_score": 0.91, "training_seconds": 412},
        "created_at": CREATED,
    }
    fields.update(overrides)
    return ForecasterRow(**fields)


# ---------------------------------------------------------------------------
# GET /v1/forecast
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_list_default_forecasters_returns_200(client, db):
    db.get_default_connector.return_value = _mock_connector()
    db.list_forecasters.return_value = [
        _mock_forecaster(version=12),
        _mock_forecaster(version=11),
    ]

    response = client.get("/v1/forecast")
    assert response.status_code == 200
    data = response.json()
    assert len(data) == 2
    assert data[0]["version"] == 12
    assert data[0]["metrics"]["validation_score"] == 0.91
    assert data[0]["created_at"] == "2026-10-26T09:12:00Z"
    assert data[1]["version"] == 11


@pytest.mark.unit
def test_list_default_forecasters_404_when_no_connector(client, db):
    db.get_default_connector.return_value = None

    response = client.get("/v1/forecast")
    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_list_default_forecasters_empty_when_no_forecasters(client, db):
    db.get_default_connector.return_value = _mock_connector()
    db.list_forecasters.return_value = []

    response = client.get("/v1/forecast")
    assert response.status_code == 200
    assert response.json() == []


# ---------------------------------------------------------------------------
# GET /v1/forecast/{connector_id}
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_list_connector_forecasters_returns_200(client, db):
    db.get_connector.return_value = _mock_connector()
    db.list_forecasters.return_value = [_mock_forecaster(version=15)]

    response = client.get(f"/v1/forecast/{CONNECTOR_ID}")
    assert response.status_code == 200
    data = response.json()
    assert len(data) == 1
    assert data[0]["version"] == 15


@pytest.mark.unit
def test_list_connector_forecasters_404_when_not_found(client, db):
    db.get_connector.return_value = None

    response = client.get(f"/v1/forecast/{CONNECTOR_ID}")
    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_list_connector_forecasters_422_on_invalid_uuid(client, db):
    response = client.get("/v1/forecast/invalid-uuid-123")
    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


# ---------------------------------------------------------------------------
# POST /v1/forecast & POST /v1/forecast/{connector_id}
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_forecast_default_success_array_payload(client, db):
    db.get_default_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    triton_predictions = [
        {"prediction": "N", "confidence": 0.87},
        {"prediction": "Y", "confidence": 0.64},
    ]

    with patch("forecast.triton_client.triton.run_inference", return_value=triton_predictions) as mock_infer:
        payload = [
            {"REGION": "EMEA", "QUANTITY": 12, "UNIT_PRICE": 19.99},
            {"REGION": "APAC", "QUANTITY": 3, "UNIT_PRICE": 249.0},
        ]
        response = client.post("/v1/forecast", json=payload)

    assert response.status_code == 200
    body = response.json()
    assert body["version"] == 12
    assert body["predictions"] == triton_predictions
    mock_infer.assert_called_once()


@pytest.mark.unit
def test_forecast_with_specific_version(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_forecaster.return_value = _mock_forecaster(version=7)

    triton_predictions = [{"prediction": "Y", "confidence": 0.99}]

    with patch("forecast.triton_client.triton.run_inference", return_value=triton_predictions) as mock_infer:
        payload = [{"REGION": "US", "QUANTITY": 1, "UNIT_PRICE": 10.0}]
        response = client.post(f"/v1/forecast/{CONNECTOR_ID}?version=7", json=payload)

    assert response.status_code == 200
    assert response.json()["version"] == 7
    mock_infer.assert_called_once_with(
        model_name=f"fc_{CONNECTOR_ID}_7",
        instances=payload,
        input_columns=_mock_connector().input_columns,
    )


@pytest.mark.unit
def test_forecast_empty_instances_fails_400(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=[])
    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)


@pytest.mark.unit
def test_forecast_invalid_body_format_fails_422(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json="not-a-list")
    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)

    # Object instead of array also fails 422
    response_obj = client.post(f"/v1/forecast/{CONNECTOR_ID}", json={"foo": "bar"})
    assert response_obj.status_code == 422
    _assert_envelope(response_obj.json(), "INVALID_REQUEST", 422)


@pytest.mark.unit
def test_forecast_missing_column_fails_400(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    # Missing UNIT_PRICE
    payload = [{"REGION": "EMEA", "QUANTITY": 10}]
    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=payload)
    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "missing required column" in response.json()["error"]["message"]


@pytest.mark.unit
def test_forecast_unknown_column_fails_400(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    # Extra UNKNOWN_COL
    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0, "UNKNOWN_COL": "val"}]
    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=payload)
    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "unknown column" in response.json()["error"]["message"]


@pytest.mark.unit
def test_forecast_null_column_value_fails_400(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    payload = [{"REGION": "EMEA", "QUANTITY": None, "UNIT_PRICE": 20.0}]
    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=payload)
    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "cannot be null" in response.json()["error"]["message"]


@pytest.mark.unit
def test_forecast_no_ready_forecaster_fails_404(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = None

    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=payload)
    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_forecast_specific_version_not_found_fails_404(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_forecaster.return_value = None

    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
    response = client.post(f"/v1/forecast/{CONNECTOR_ID}?version=999", json=payload)
    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_forecast_triton_unavailable_returns_503(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    with patch("httpx.AsyncClient.post", side_effect=httpx.ConnectError("Connection refused")):
        payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
        response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=payload)

    assert response.status_code == 503
    _assert_envelope(response.json(), "TRITON_UNAVAILABLE", 503)


@pytest.mark.unit
def test_forecast_exceeding_max_rows_fails_400(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    with patch.object(settings.forecast, "max_inference_rows", 2):
        payload = [
            {"REGION": "EMEA", "QUANTITY": 1, "UNIT_PRICE": 10.0},
            {"REGION": "APAC", "QUANTITY": 2, "UNIT_PRICE": 20.0},
            {"REGION": "LATAM", "QUANTITY": 3, "UNIT_PRICE": 30.0},
        ]
        response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=payload)

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "exceeds maximum allowed inference limit" in response.json()["error"]["message"]


@pytest.mark.unit
def test_forecast_with_invalid_non_positive_version_fails_400(client, db):
    db.get_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    payload = [{"REGION": "EMEA", "QUANTITY": 1, "UNIT_PRICE": 10.0}]
    response = client.post(f"/v1/forecast/{CONNECTOR_ID}?version=0", json=payload)
    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "positive integer" in response.json()["error"]["message"]


@pytest.mark.unit
def test_forecast_complex_json_fails_400(client, db):
    db.get_connector.return_value = _mock_connector(input_columns=["CONFIG"])
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    payload = [{"CONFIG": {"nested": "dict"}}]
    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=payload)
    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "cannot be complex JSON type" in response.json()["error"]["message"]


@pytest.mark.unit
def test_forecast_unknown_connector_fails_404(client, db):
    db.get_connector.return_value = None

    response = client.post(f"/v1/forecast/{CONNECTOR_ID}", json=[{"REGION": "EMEA"}])

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)
    db.get_newest_forecaster.assert_not_called()


def _validation_error(instances, input_columns):
    from fastapi import HTTPException
    from forecast.utils.inference_validation import validate_inference_instances

    with pytest.raises(HTTPException) as exc_info:
        validate_inference_instances(instances, input_columns)
    assert exc_info.value.status_code == 400
    return exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_validation_rejects_a_body_that_is_not_an_array():
    message = _validation_error({"REGION": "EMEA"}, ["REGION"])

    assert "must be a JSON array" in message


@pytest.mark.unit
def test_validation_rejects_a_row_that_is_not_an_object():
    message = _validation_error(["EMEA"], ["REGION"])

    assert message.endswith("Row 0 is not a valid JSON object.")


@pytest.mark.unit
def test_validation_accepts_scalars():
    from forecast.utils.inference_validation import validate_inference_instances

    validate_inference_instances(
        [{"A": "text", "B": 1, "C": 2.5}],
        ["A", "B", "C"],
    )


@pytest.mark.unit
def test_triton_rows_rejects_an_output_that_is_not_an_object():
    from fastapi import HTTPException
    from forecast.triton_client.triton import _rows

    with pytest.raises(HTTPException) as exc_info:
        _rows(["not", "an", "object"], 1)

    assert exc_info.value.status_code == 500


@pytest.mark.unit
def test_triton_rows_rejects_a_shape_that_does_not_match_the_batch():
    from fastapi import HTTPException
    from forecast.triton_client.triton import _rows

    with pytest.raises(HTTPException) as exc_info:
        _rows({"name": "predictions", "data": [1, 2], "shape": [1, 2]}, 2)

    assert exc_info.value.status_code == 500


@pytest.mark.unit
def test_triton_run_inference_flat_v2_tensor_parsing():
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {
                "name": "probabilities",
                "shape": [2, 2],
                "data": [0.1, 0.9, 0.85, 0.15],
            },
            {
                "name": "predictions",
                "shape": [2, 1],
                "data": [1, 0],
            },
        ]
    }

    input_cols = [
        "S",
        "I",
        "N",
        "U",
    ]
    instances = [
        {"S": "text", "I": 42, "N": 3.14, "U": "fallback"},
        {"S": "text2", "I": 43, "N": 2.71, "U": "fallback2"},
    ]

    with patch("httpx.AsyncClient.post", return_value=mock_resp) as mock_post:
        preds = asyncio.run(run_inference("fc_model_1", instances, input_cols))

    assert len(preds) == 2
    assert preds[0] == {"prediction": 1, "confidence": 0.9}
    assert preds[1] == {"prediction": 0, "confidence": 0.85}
    mock_post.assert_called_once()


@pytest.mark.unit
def test_triton_run_inference_probabilities_with_class_labels():
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {
                "name": "probabilities",
                "shape": [2, 2],
                "data": [0.1, 0.9, 0.85, 0.15],
            },
            {
                "name": "predictions",
                "shape": [2, 1],
                "data": ["Y", "N"],
            },
        ]
    }

    input_cols = ["A"]
    instances = [{"A": "x"}, {"A": "y"}]

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        preds = asyncio.run(run_inference("fc_model_1", instances, input_cols))

    assert len(preds) == 2
    assert preds[0] == {"prediction": "Y", "confidence": 0.9}
    assert preds[1] == {"prediction": "N", "confidence": 0.85}


@pytest.mark.unit
def test_triton_run_inference_reads_the_outputs_of_the_training_worker():
    """The worker's classification model returns ``prediction`` (one class per row) and ``probabilities``."""
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "prediction", "datatype": "BYTES", "shape": [2], "data": ["Y", "N"]},
            {"name": "probabilities", "datatype": "FP32", "shape": [2, 2], "data": [0.2, 0.8, 0.7, 0.3]},
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        preds = asyncio.run(run_inference("fc_model_1", [{"A": "x"}, {"A": "y"}], ["A"]))

    assert preds == [{"prediction": "Y", "confidence": 0.8}, {"prediction": "N", "confidence": 0.7}]


@pytest.mark.unit
def test_triton_scalar_probabilities_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    # 1D scalar per row (width = 1) is invalid for probability distributions
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "probabilities", "shape": [2, 1], "data": [0.9, 0.85]}
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}, {"A": 2}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_probabilities_without_predictions_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "probabilities", "shape": [2, 2], "data": [0.1, 0.9, 0.8, 0.2]}
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}, {"A": 2}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]

@pytest.mark.unit
def test_triton_run_inference_separate_predictions_and_confidences():
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "predictions", "shape": [2, 1], "data": ["Y", "N"]},
            {"name": "confidences", "shape": [2, 1], "data": [0.95, 0.88]},
        ]
    }

    input_cols = ["A"]
    instances = [{"A": "x"}, {"A": "y"}]

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        preds = asyncio.run(run_inference("fc_model_1", instances, input_cols))

    assert len(preds) == 2
    assert preds[0] == {"prediction": "Y", "confidence": 0.95}
    assert preds[1] == {"prediction": "N", "confidence": 0.88}


@pytest.mark.unit
def test_triton_malformed_json_response_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = ["not", "a", "dict"]

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_non_finite_confidence_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "predictions", "shape": [1, 1], "data": ["Y"]},
            {"name": "confidences", "shape": [1, 1], "data": [float("nan")]},
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_non_numeric_probabilities_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "probabilities", "shape": [1, 2], "data": ["invalid", 0.5]},
            {"name": "predictions", "shape": [1, 1], "data": ["Y"]},
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_non_scalar_prediction_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "predictions", "shape": [1, 2], "data": ["Y", "EXTRA"]},
            {"name": "confidences", "shape": [1, 1], "data": [0.9]},
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]

@pytest.mark.unit
def test_triton_run_inference_handles_not_served_404_as_503():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 404
    mock_resp.text = "Model not found"

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 503
    assert exc_info.value.detail["error"]["code"] == "TRITON_UNAVAILABLE"


@pytest.mark.unit
def test_triton_run_inference_handles_unknown_model_400_as_503():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 400
    mock_resp.text = "Request for unknown model: 'fc_model_1' is not ready"

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 503
    assert exc_info.value.detail["error"]["code"] == "TRITON_UNAVAILABLE"


@pytest.mark.unit
def test_triton_run_inference_handles_http_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 500
    mock_resp.text = "Internal Triton failure"

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton inference failed" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_run_inference_handles_malformed_json():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.side_effect = ValueError("Invalid JSON")

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_run_inference_output_dimension_mismatch_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    # 2 rows expected but 3 probabilities data items
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "probabilities", "data": [0.1, 0.2, 0.3]}
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference(
                "fc_model_1",
                [{"A": 1}, {"A": 2}],
                ["A"],
            ))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]


# ---------------------------------------------------------------------------
# Database error handling tests on forecast routes
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_get_default_forecasters_pool_timeout_fails_429(client):
    from sqlalchemy.exc import TimeoutError as PoolTimeoutError

    with _patched_db_session(side_effect=PoolTimeoutError("Pool timed out")):
        response = client.get("/v1/forecast")

    assert response.status_code == 429
    _assert_envelope(response.json(), "SERVER_BUSY", 429)


@pytest.mark.unit
def test_get_connector_forecasters_sqlalchemy_error_fails_500(client):
    from sqlalchemy.exc import SQLAlchemyError

    with _patched_db_session(side_effect=SQLAlchemyError("DB exploded")):
        response = client.get(f"/v1/forecast/{CONNECTOR_ID}")

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


@pytest.mark.unit
def test_get_connector_forecasters_lock_55p03_fails_409(client):
    from sqlalchemy.exc import SQLAlchemyError

    orig = SimpleNamespace(pgcode="55P03", sqlstate=None)
    exc = SQLAlchemyError("Lock not available")
    exc.orig = orig

    with _patched_db_session(side_effect=exc):
        response = client.get(f"/v1/forecast/{CONNECTOR_ID}")

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)


@pytest.mark.unit
def test_post_forecast_default_db_error_fails_500(client):
    from sqlalchemy.exc import SQLAlchemyError

    with _patched_db_session(side_effect=SQLAlchemyError("DB error")):
        response = client.post(
            "/v1/forecast", json=[{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
        )

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


# ---------------------------------------------------------------------------
# POST /v1/forecast (default connector) edge paths
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_post_forecast_default_no_connector_fails_404(client, db):
    db.get_default_connector.return_value = None

    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
    response = client.post("/v1/forecast", json=payload)

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)
    assert "No connector found" in response.json()["error"]["message"]


@pytest.mark.unit
def test_post_forecast_default_no_ready_forecaster_fails_404(client, db):
    db.get_default_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = None

    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
    response = client.post("/v1/forecast", json=payload)

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)
    assert "No ready forecaster found" in response.json()["error"]["message"]


@pytest.mark.unit
def test_post_forecast_default_failed_connector_without_training_details_fails_404(client, db):
    # The oldest connector is one that was stored as FAILED: no training_configs row, no forecaster.
    db.get_default_connector.return_value = _mock_connector(
        status="FAILED",
        message="A connector already exists. Only one connector is supported.",
        source_table=None,
        target_column=None,
        schedule=None,
        input_columns=None,
        last_trained_at=None,
    )
    db.get_newest_forecaster.return_value = None

    response = client.post("/v1/forecast", json=[{"REGION": "EMEA"}])

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)
    assert "No ready forecaster found" in response.json()["error"]["message"]


@pytest.mark.unit
def test_post_forecast_default_specific_version_success(client, db):
    db.get_default_connector.return_value = _mock_connector()
    db.get_forecaster.return_value = _mock_forecaster(version=5)

    mock_preds = [{"prediction": "Y", "confidence": 0.92}]
    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]

    with patch("forecast.triton_client.triton.run_inference", return_value=mock_preds) as mock_infer:
        response = client.post("/v1/forecast?version=5", json=payload)

    assert response.status_code == 200
    data = response.json()
    assert data["version"] == 5
    assert data["predictions"] == mock_preds
    mock_infer.assert_called_once_with(
        model_name=f"fc_{CONNECTOR_ID}_5",
        instances=payload,
        input_columns=_mock_connector().input_columns,
    )


@pytest.mark.unit
def test_post_forecast_default_specific_version_not_found_fails_404(client, db):
    db.get_default_connector.return_value = _mock_connector()
    db.get_forecaster.return_value = None

    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
    response = client.post("/v1/forecast?version=99", json=payload)

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)
    assert "version 99 not found" in response.json()["error"]["message"]


@pytest.mark.unit
def test_post_forecast_default_invalid_version_fails_400(client, db):
    db.get_default_connector.return_value = _mock_connector()

    payload = [{"REGION": "EMEA", "QUANTITY": 10, "UNIT_PRICE": 20.0}]
    response = client.post("/v1/forecast?version=-1", json=payload)

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)


@pytest.mark.unit
def test_post_forecast_default_empty_instances_fails_400(client, db):
    db.get_default_connector.return_value = _mock_connector()
    db.get_newest_forecaster.return_value = _mock_forecaster(version=12)

    response = client.post("/v1/forecast", json=[])

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)


# ---------------------------------------------------------------------------
# Triton client payload formatting & error handling tests
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_triton_run_inference_payload_structure_and_datatypes():
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "predictions", "shape": [2, 1], "data": ["A", "B"]},
            {"name": "confidences", "shape": [2, 1], "data": [0.9, 0.8]},
        ]
    }

    input_cols = [
        "INT_COL",
        "NUM_COL",
        "STR_COL",
        "TYPELESS_COL",
    ]
    instances = [
        {"INT_COL": 10, "NUM_COL": 12.5, "STR_COL": "val1", "TYPELESS_COL": "x"},
        {"INT_COL": 20, "NUM_COL": 25.0, "STR_COL": "val2", "TYPELESS_COL": 99},
    ]

    with patch("httpx.AsyncClient.post", return_value=mock_resp) as mock_post:
        preds = asyncio.run(run_inference("fc_test_model", instances, input_cols))

    assert len(preds) == 2
    mock_post.assert_called_once()
    _, kwargs = mock_post.call_args
    sent_inputs = kwargs["json"]["inputs"]

    # Every column goes as text, because the forecaster's inputs are TYPE_STRING.
    assert sent_inputs == [
        {"name": "INT_COL", "shape": [2, 1], "datatype": "BYTES", "data": ["10", "20"]},
        {"name": "NUM_COL", "shape": [2, 1], "datatype": "BYTES", "data": ["12.5", "25.0"]},
        {"name": "STR_COL", "shape": [2, 1], "datatype": "BYTES", "data": ["val1", "val2"]},
        {"name": "TYPELESS_COL", "shape": [2, 1], "datatype": "BYTES", "data": ["x", "99"]},
    ]


@pytest.mark.unit
def test_triton_run_inference_timeout_raises_503():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    with patch("httpx.AsyncClient.post", side_effect=httpx.ReadTimeout("Triton read timeout")):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 503
    assert exc_info.value.detail["error"]["code"] == "TRITON_UNAVAILABLE"


@pytest.mark.unit
def test_triton_run_inference_connection_error_raises_503():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    with patch("httpx.AsyncClient.post", side_effect=httpx.ConnectError("Connection refused")):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 503
    assert exc_info.value.detail["error"]["code"] == "TRITON_UNAVAILABLE"


@pytest.mark.unit
def test_triton_run_inference_request_exception_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    with patch("httpx.AsyncClient.post", side_effect=httpx.RemoteProtocolError("Corrupt payload")):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton inference failed" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_run_inference_http_400_not_unserved_marker_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 400
    mock_resp.text = "Invalid tensor shape in inference request"

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton inference failed" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_triton_run_inference_missing_confidences_raises_500():
    from fastapi import HTTPException
    from forecast.triton_client.triton import run_inference

    mock_resp = MagicMock()
    mock_resp.status_code = 200
    mock_resp.json.return_value = {
        "outputs": [
            {"name": "predictions", "shape": [1, 1], "data": ["Y"]}
        ]
    }

    with patch("httpx.AsyncClient.post", return_value=mock_resp):
        with pytest.raises(HTTPException) as exc_info:
            asyncio.run(run_inference("fc_model_1", [{"A": 1}], ["A"]))

    assert exc_info.value.status_code == 500
    assert "Triton returned an unusable response" in exc_info.value.detail["error"]["message"]


@pytest.mark.unit
def test_http_client_is_shared_and_recreated_after_close():
    from forecast.utils import http_client

    asyncio.run(http_client.close_client())
    first = http_client.get_client()
    assert http_client.get_client() is first

    asyncio.run(http_client.close_client())
    assert first.is_closed
    assert http_client.get_client() is not first

    asyncio.run(http_client.close_client())
    asyncio.run(http_client.close_client())  # Closing twice is harmless.
