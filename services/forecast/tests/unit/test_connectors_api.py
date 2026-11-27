"""
Unit tests for the connector API and the connector secret handling.

The database is mocked at the connectors DB layer, so the tests run without
PostgreSQL. Tests cover:
 - POST /v1/connectors: create, a connector that exists (409), a bad key (201, stored as
   FAILED), body validation, encrypted storage and the scheduler call
 - GET /v1/connectors and GET /v1/connectors/{id}: pagination, name filter, safe fields
 - PUT /v1/connectors/{id}: partial merge, re-encryption, validation, rejected fields
 - GET /v1/connectors/{id}/metrics: versions and training summary
 - Encryption helpers: round trip, allowlist, nonce
"""

import uuid
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from unittest.mock import DEFAULT, MagicMock, patch

import pytest
from fastapi.testclient import TestClient
from pydantic import SecretStr
from sqlalchemy.exc import IntegrityError, OperationalError

# Imported at collection time so the "forecast_server" logger exists before pytest
# attaches its capture handlers (see test_app.py).
import forecast.app as forecast_app
import forecast.api.v1.connectors as connectors_api
import forecast.db.connectors as connectors_db
from forecast.utils.api_utils import format_timestamp
from forecast.db.connectors import ConnectorListRow, ConnectorRow, JobRow
from forecast.training.scheduler import register_connector_job
from forecast.settings import settings
from forecast.utils.encryption import (
    decrypt_secrets,
    encrypt_secrets,
    safe_connection_details,
)

CONNECTOR_ID = "7c9e6679-7425-40de-944b-e07fc1f90ae7"
KEY = "0123456789abcdef0123456789abcdef"
CREATED = datetime(2026, 10, 26, 8, 55, tzinfo=timezone.utc)


@pytest.fixture(autouse=True)
def _startup_dependencies_stubbed():
    """Valid key and stubbed TRITON_URL check."""
    with patch.object(
        settings.forecast, "db_encryption_key", SecretStr(KEY)
    ), patch("forecast.app.triton_url_check", return_value=None):
        yield


@pytest.fixture
def client():
    with patch.object(forecast_app, "close_db_connections"):
        with TestClient(forecast_app.app) as test_client:
            yield test_client


@pytest.fixture
def db():
    """Replace the session and the connectors DB functions with mocks."""
    session = MagicMock(name="session")

    @contextmanager
    def fake_session():
        yield session

    defaults = {
        "lock_connectors": None,
        "connector_exists": False,
        "connector_id_exists": False,
        "insert_connector": None,
        "insert_failed_connector": None,
        "list_connectors": ([], 0),
        "get_connector": None,
        "update_connector": None,
        "list_ready_versions": [],
        "list_jobs": [],
    }
    with patch.object(connectors_api, "get_db_session", fake_session), patch.object(
        connectors_api, "register_connector_job"
    ) as register, patch.multiple(connectors_db, **{name: DEFAULT for name in defaults}) as mocks:
        for name, value in defaults.items():
            mocks[name].return_value = value
        yield SimpleNamespace(session=session, register=register, **mocks)


CERTIFICATE = "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUTEST\n-----END CERTIFICATE-----"


def _create_body(**overrides):
    body = {
        "id": CONNECTOR_ID,
        "name": "prod-sales-db",
        "type": "db2_for_i",
        "connection_details": {
            "host": "ibmi.example.com",
            "port": 8471,
            "database": "PRODDB",
            "username": "forecast_ro",
            "password": "s3cret",
        },
        "service_settings": {
            "forecast": {
                "table": "ORDERS",
                "targetColumn": "DELIVERY_LATE",
                "inputColumns": ["REGION", "QUANTITY"],
                "schedule": "daily",
            },
        },
    }
    body.update(overrides)
    return body


def _settings(**overrides):
    """The `forecast` settings of a create body, with the given fields replaced."""
    return {**_create_body()["service_settings"]["forecast"], **overrides}


def _service_settings(**overrides):
    """service_settings of a create body, with the given `forecast` fields replaced."""
    return {"forecast": _settings(**overrides)}


def _stored_row(password="s3cret", host="ibmi.example.com", certificate=None, ignore_secure_connection=None, **overrides):
    """A connector as the database returns it, with the password and the certificate encrypted.

    A connector stored before the TLS fields existed has neither of them, so they are only
    set when given.
    """
    details = {
        "host": host,
        "port": 8471,
        "database": "PRODDB",
        "username": "forecast_ro",
        "password": password,
    }
    if certificate is not None:
        details["certificate"] = certificate
    if ignore_secure_connection is not None:
        details["ignore_secure_connection"] = ignore_secure_connection
    config = encrypt_secrets(details)
    fields = {
        "id": uuid.UUID(CONNECTOR_ID),
        "name": "prod-sales-db",
        "type": "db2_for_i",
        "config": config,
        "status": "ACCEPTED",
        "message": None,
        "created_at": CREATED,
        "updated_at": CREATED,
        "source_table": "ORDERS",
        "target_column": "DELIVERY_LATE",
        "schedule": "daily",
        "last_trained_at": None,
    }
    fields.update(overrides)
    return ConnectorRow(**fields)


def _job(seq, status, **overrides):
    """A training job as the database returns it."""
    fields = {
        "seq": seq,
        "status": status,
        "message": None,
        "result": None,
        "created_at": CREATED,
        "started_at": CREATED,
        "finished_at": CREATED,
    }
    fields.update(overrides)
    return JobRow(**fields)


def _assert_envelope(body: dict, code: str, status: int):
    assert set(body) == {"error"}
    assert body["error"]["code"] == code
    assert body["error"]["status"] == status


# ---------------------------------------------------------------------------
# POST /v1/connectors
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_create_stores_encrypted_connector_and_queues_training(client, db):
    response = client.post("/v1/connectors", json=_create_body(id=CONNECTOR_ID))

    assert response.status_code == 201
    assert response.json() == {"id": CONNECTOR_ID}
    db.lock_connectors.assert_called_once()

    kwargs = db.insert_connector.call_args.kwargs
    assert kwargs["connector_id"] == CONNECTOR_ID
    assert kwargs["connector_type"] == "db2_for_i"
    assert kwargs["config"]["host"] == "ibmi.example.com"
    assert kwargs["config"]["password"] != "s3cret"
    assert decrypt_secrets(kwargs["config"])["password"] == "s3cret"
    assert kwargs["config"]["ignore_secure_connection"] is False
    assert kwargs["source_table"] == "ORDERS"
    assert kwargs["target_column"] == "DELIVERY_LATE"
    assert kwargs["input_columns"] == ["REGION", "QUANTITY"]
    assert kwargs["schedule"] == "daily"

    db.register.assert_called_once_with(CONNECTOR_ID, "daily")


@pytest.mark.unit
def test_create_without_an_id_is_422(client, db):
    body = _create_body()
    del body["id"]
    response = client.post("/v1/connectors", json=body)

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


@pytest.mark.unit
def test_create_without_a_schedule_registers_none(client, db):
    settings_body = _service_settings()
    del settings_body["forecast"]["schedule"]

    response = client.post("/v1/connectors", json=_create_body(service_settings=settings_body))

    assert response.status_code == 201
    assert db.insert_connector.call_args.kwargs["schedule"] is None
    db.register.assert_called_once_with(CONNECTOR_ID, None)


@pytest.mark.unit
@pytest.mark.parametrize("body", [{}, {"name": "another-db"}])
def test_create_when_a_connector_exists_is_409_and_stores_nothing(client, db, body):
    db.connector_exists.return_value = True

    response = client.post("/v1/connectors", json=_create_body(**body))

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_CONFLICT", 409)
    assert response.json()["error"]["message"].endswith("A connector already exists. Only one connector is supported.")
    db.insert_connector.assert_not_called()
    db.insert_failed_connector.assert_not_called()
    db.register.assert_not_called()


@pytest.mark.unit
def test_create_with_an_existing_id_is_201_and_changes_nothing(client, db):
    db.connector_id_exists.return_value = True
    db.connector_exists.return_value = True

    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 201
    assert response.json() == {"id": CONNECTOR_ID}
    db.connector_id_exists.assert_called_once()
    assert db.connector_id_exists.call_args.args[1] == CONNECTOR_ID
    db.insert_connector.assert_not_called()
    db.insert_failed_connector.assert_not_called()
    db.register.assert_not_called()


@pytest.mark.unit
def test_create_stored_as_failed_with_a_database_error_is_500(db):
    failing_client = TestClient(forecast_app.app, raise_server_exceptions=False)
    db.insert_failed_connector.side_effect = IntegrityError(
        "INSERT", {}, MagicMock(pgcode="23514")
    )

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        response = failing_client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


@pytest.mark.unit
def test_rejected_create_is_logged_without_secrets(client, db, caplog):
    db.connector_exists.return_value = True

    with caplog.at_level("INFO"):
        client.post("/v1/connectors", json=_create_body())

    assert "a connector already exists" in caplog.text
    assert CONNECTOR_ID in caplog.text
    assert "s3cret" not in caplog.text


@pytest.mark.unit
@pytest.mark.parametrize(
    "overrides",
    [
        {"type": "oracle"},
        {"id": "not-a-uuid"},
        {"name": ""},
        {"name": "x" * 256},
        {"unknown": "field"},
        {"schedule": "daily"},
        {"training": _settings()},
        {"params": _settings()},
        {"connection_details": {"host": "h", "database": "d", "username": "u", "password": "p", "extra": 1}},
        {"connection_details": {"host": "h", "database": "d", "username": "u"}},
        {"service_settings": _service_settings(schedule="hourly-ish")},
        {"service_settings": _service_settings(inputColumns=[{"name": "A"}])},
        {"service_settings": _service_settings(inputColumns=[])},
        {"service_settings": _service_settings(table="T; DROP TABLE")},
        {"service_settings": _service_settings(targetColumn="C' OR 1=1")},
        {"service_settings": _service_settings(inputColumns=["BAD NAME"])},
        {"service_settings": _service_settings(table="T\n")},
        {"service_settings": {}},
        {"service_settings": {"other": _settings()}},
        {"service_settings": _settings()},
        {"service_settings": {"forecast": {"table": "T", "target_column": "C", "input_columns": ["A"]}}},
        {"service_settings": _service_settings(inputColumns=["COL1", "col1"])},
        {"service_settings": _service_settings(targetColumn="TARGET_COL", inputColumns=["target_col", "OTHER"])},
    ],
)
def test_create_rejects_invalid_bodies_with_422(client, db, overrides):
    response = client.post("/v1/connectors", json=_create_body(**overrides))

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)
    db.insert_connector.assert_not_called()
    db.insert_failed_connector.assert_not_called()


@pytest.mark.unit
def test_create_accepts_the_catalog_format_and_ignores_unknown_settings(client, db):
    service_settings = {
        "forecast": {
            "table": "sales_data",
            "targetColumn": "sales_amount",
            "inputColumns": ["region", "unit_price"],
            "schedule": "daily",
            "validationMetric": "accuracy",
            "nested": {"a": 1},
        },
        "digitize": {"anything": ["goes"]},
    }

    response = client.post("/v1/connectors", json=_create_body(service_settings=service_settings))

    assert response.status_code == 201
    kwargs = db.insert_connector.call_args.kwargs
    assert kwargs["source_table"] == "sales_data"
    assert kwargs["target_column"] == "sales_amount"
    assert kwargs["input_columns"] == ["region", "unit_price"]
    assert kwargs["schedule"] == "daily"
    assert "validationMetric" not in str(kwargs) and "digitize" not in str(kwargs)


@pytest.mark.unit
def test_unknown_service_settings_are_dropped_by_the_model():
    from forecast.models import ServiceSettings

    parsed = ServiceSettings.model_validate(
        {"forecast": {**_settings(), "extra": 1}, "other": {"x": 1}}
    )

    assert set(parsed.model_dump()) == {"forecast"}
    assert set(parsed.forecast.model_dump()) == {"table", "target_column", "input_columns", "schedule"}


@pytest.mark.unit
def test_create_without_service_settings_is_422(client, db):
    body = _create_body()
    del body["service_settings"]

    response = client.post("/v1/connectors", json=body)

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


@pytest.mark.unit
@pytest.mark.parametrize(
    "key, message",
    [("", "Connector encryption key not found"), ("x" * 31, "must be exactly 32 bytes")],
)
def test_create_without_a_usable_key_is_201_and_stored_as_failed_without_the_secrets(db, caplog, key, message):
    client = TestClient(forecast_app.app)

    with patch.object(settings.forecast, "db_encryption_key", SecretStr(key)), caplog.at_level("ERROR"):
        response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 201
    assert response.json() == {"id": CONNECTOR_ID}
    db.insert_connector.assert_not_called()
    db.register.assert_not_called()
    kwargs = db.insert_failed_connector.call_args.kwargs
    assert message in kwargs["message"]
    # Only the fields that are shown in a response are kept: nothing can be encrypted, and the
    # password and the certificate are never stored in plain text.
    assert kwargs["config"] == {
        "host": "ibmi.example.com", "port": 8471, "database": "PRODDB", "username": "forecast_ro",
        "ignore_secure_connection": False,
    }
    assert "s3cret" not in caplog.text and "s3cret" not in response.text and "s3cret" not in kwargs["message"]


@pytest.mark.unit
def test_create_without_a_key_drops_the_certificate_and_keeps_the_flag(db):
    client = TestClient(forecast_app.app)
    body = _create_body()
    body["connection_details"].update({"certificate": CERTIFICATE, "ignore_secure_connection": True})

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        response = client.post("/v1/connectors", json=body)

    assert response.status_code == 201
    config = db.insert_failed_connector.call_args.kwargs["config"]
    assert config["ignore_secure_connection"] is True
    assert "certificate" not in config


@pytest.mark.unit
def test_create_without_a_key_when_a_connector_exists_is_409_and_stores_nothing(client, db):
    db.connector_exists.return_value = True

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_CONFLICT", 409)
    db.insert_failed_connector.assert_not_called()


@pytest.mark.unit
def test_create_with_an_existing_id_and_no_key_changes_nothing(client, db):
    db.connector_id_exists.return_value = True
    db.connector_exists.return_value = True

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 201
    assert response.json() == {"id": CONNECTOR_ID}
    db.insert_connector.assert_not_called()
    db.insert_failed_connector.assert_not_called()


# ---------------------------------------------------------------------------
# GET /v1/connectors
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_list_returns_the_pagination_envelope(client, db):
    db.list_connectors.return_value = (
        [
            ConnectorListRow(
                id=uuid.UUID(CONNECTOR_ID),
                name="prod-sales-db",
                type="db2_for_i",
                status="ACCEPTED",
                message=None,
                created_at=CREATED,
                last_trained_at=None,
                source_table="ORDERS",
                target_column="DELIVERY_LATE",
            )
        ],
        1,
    )

    response = client.get("/v1/connectors", params={"name": "prod", "limit": 10, "offset": 0})

    assert response.status_code == 200
    body = response.json()
    assert body["pagination"] == {"total": 1, "limit": 10, "offset": 0}
    assert body["data"][0]["id"] == CONNECTOR_ID
    assert body["data"][0]["table"] == "ORDERS"
    assert body["data"][0]["status"] == "ACCEPTED"
    assert body["data"][0]["last_trained_at"] is None
    assert "password" not in response.text
    db.list_connectors.assert_called_once_with(
        db.session, limit=10, offset=0, name="prod"
    )


@pytest.mark.unit
@pytest.mark.parametrize("params", [{"limit": 0}, {"limit": 201}, {"offset": -1}])
def test_list_rejects_out_of_range_paging(client, db, params):
    response = client.get("/v1/connectors", params=params)

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


@pytest.mark.unit
def test_list_name_longer_than_255_returns_422(client, db):
    response = client.get("/v1/connectors", params={"name": "a" * 256})

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)
    db.list_connectors.assert_not_called()


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_get_of_a_connector_stored_before_the_tls_fields_has_no_ignore_secure_connection(client, db):
    # Absent means false; the response shows the details as stored.
    db.get_connector.return_value = _stored_row()

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 200
    assert "ignore_secure_connection" not in response.json()["connection_details"]


@pytest.mark.unit
def test_get_returns_safe_details_only(client, db):
    db.get_connector.return_value = _stored_row(certificate=CERTIFICATE, ignore_secure_connection=True)

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 200
    body = response.json()
    assert body["connection_details"] == {
        "host": "ibmi.example.com",
        "port": 8471,
        "database": "PRODDB",
        "username": "forecast_ro",
        "ignore_secure_connection": True,
    }
    assert "BEGIN CERTIFICATE" not in response.text
    assert "certificate" not in response.text
    assert body["table"] == "ORDERS"
    assert body["target_column"] == "DELIVERY_LATE"
    assert body["schedule"] == "daily"
    assert body["status"] == "ACCEPTED"
    assert "s3cret" not in response.text
    assert "password" not in response.text


@pytest.mark.unit
def test_get_unknown_connector_is_404(client, db):
    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_get_with_a_non_uuid_id_is_422(client, db):
    response = client.get("/v1/connectors/not-a-uuid")

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


# ---------------------------------------------------------------------------
# PUT /v1/connectors/{connector_id}
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_put_empty_body_is_a_no_op(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={})

    assert response.status_code == 200
    assert response.content == b""
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_without_a_body_is_a_no_op(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 200
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_null_port_clears_the_port(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"port": None, "password": "s3cret"}},
    )

    assert response.status_code == 200
    stored = decrypt_secrets(db.update_connector.call_args.kwargs["config"])
    assert stored == {
        "host": "ibmi.example.com", "port": None, "database": "PRODDB",
        "username": "forecast_ro", "password": "s3cret",
    }


@pytest.mark.unit
def test_put_unknown_connector_is_404(client, db):
    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "new"})

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_put_renames_without_touching_the_secrets(client, db):
    row = _stored_row()
    db.get_connector.return_value = row

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert response.status_code == 200
    kwargs = db.update_connector.call_args.kwargs
    assert kwargs["name"] == "renamed"
    assert kwargs["config"] == row.config


@pytest.mark.unit
def test_put_with_a_name_that_is_taken_is_409(client, db):
    db.get_connector.return_value = _stored_row()
    db.update_connector.side_effect = IntegrityError("UPDATE", {}, Exception("duplicate key"))

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "taken"})

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)
    assert "taken" in response.json()["error"]["message"]


@pytest.mark.unit
def test_put_partial_details_keeps_other_keys_and_reencrypts_the_password(client, db):
    row = _stored_row(password="old-pass")
    db.get_connector.return_value = row

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"password": "new-pass", "port": 9000}},
    )

    assert response.status_code == 200
    config = db.update_connector.call_args.kwargs["config"]
    assert config["host"] == "ibmi.example.com"
    assert config["username"] == "forecast_ro"
    assert config["port"] == 9000
    assert config["password"] not in ("old-pass", "new-pass")
    assert decrypt_secrets(config)["password"] == "new-pass"
    assert db.update_connector.call_args.kwargs["name"] is None


@pytest.mark.unit
def test_put_that_breaks_the_merged_details_is_400_and_writes_nothing(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": ""}},
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    db.update_connector.assert_not_called()


@pytest.mark.unit
@pytest.mark.parametrize(
    "body",
    [{"type": "db2_for_i"}, {"service_settings": {"forecast": {"table": "T", "targetColumn": "C", "inputColumns": ["A"]}}}],
)
def test_put_rejects_type_and_service_settings(client, db, body):
    db.get_connector.return_value = _stored_row()

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json=body)

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)
    db.update_connector.assert_not_called()


# ---------------------------------------------------------------------------
# GET /v1/connectors/{connector_id}/metrics
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_metrics_unknown_connector_is_404(client, db):
    response = client.get(f"/v1/connectors/{CONNECTOR_ID}/metrics")

    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_metrics_summarizes_the_retained_jobs(client, db):
    db.get_connector.return_value = _stored_row()
    db.list_ready_versions.return_value = [3, 2]
    db.list_jobs.return_value = [
        _job(
            3, "FAILED", message="timeout",
            finished_at=datetime(2026, 10, 28, tzinfo=timezone.utc),
            result={"rows_read": 10, "attempts": 2, "training_seconds": 99},
        ),
        _job(
            2, "COMPLETED",
            finished_at=datetime(2026, 10, 27, tzinfo=timezone.utc),
            result={"rows_read": 120000, "algorithm": "GBM", "training_seconds": 412, "attempts": 1},
        ),
        _job(
            1, "COMPLETED",
            finished_at=datetime(2026, 10, 26, 9, tzinfo=timezone.utc),
            result={"rows_read": 100000, "algorithm": "GBM", "training_seconds": 400, "attempts": 1},
        ),
    ]

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}/metrics")

    assert response.status_code == 200
    body = response.json()
    assert body["connector_id"] == CONNECTOR_ID
    assert body["versions"] == {"ready": [3, 2]}
    assert body["training"] == {
        "jobs_completed": 2,
        "jobs_failed": 1,
        "last_job_status": "FAILED",
        "last_duration_seconds": 99,
        "last_completed_at": "2026-10-27T00:00:00Z",
    }
    assert [job["seq"] for job in body["jobs"]] == [3, 2, 1]
    assert body["jobs"][1]["result"]["training_seconds"] == 412


@pytest.mark.unit
def test_metrics_without_jobs_has_empty_training_summary(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}/metrics")

    assert response.status_code == 200
    assert response.json()["training"] == {
        "jobs_completed": 0,
        "jobs_failed": 0,
        "last_job_status": None,
        "last_duration_seconds": None,
        "last_completed_at": None,
    }
    assert response.json()["jobs"] == []


@pytest.mark.unit
def test_metrics_with_a_finished_job_without_a_result_has_no_duration(client, db):
    db.get_connector.return_value = _stored_row()
    db.list_jobs.return_value = [_job(1, "COMPLETED")]

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}/metrics")

    assert response.status_code == 200
    training = response.json()["training"]
    assert training["jobs_completed"] == 1
    assert training["last_duration_seconds"] is None


@pytest.mark.unit
def test_metrics_failed_newest_job_without_training_seconds_has_no_duration(client, db):
    db.get_connector.return_value = _stored_row()
    db.list_jobs.return_value = [
        _job(2, "FAILED", message="timeout", result={"attempts": 3}),
        _job(
            1, "COMPLETED",
            finished_at=datetime(2026, 10, 27, tzinfo=timezone.utc),
            result={"training_seconds": 400},
        ),
    ]

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}/metrics")

    assert response.status_code == 200
    training = response.json()["training"]
    assert training["last_job_status"] == "FAILED"
    assert training["last_duration_seconds"] is None
    assert training["last_completed_at"] == "2026-10-27T00:00:00Z"


# ---------------------------------------------------------------------------
# Encryption helpers
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_only_the_secrets_are_encrypted():
    details = {
        "host": "h", "port": 1, "database": "d", "username": "u", "password": "p",
        "ignore_secure_connection": True, "certificate": CERTIFICATE,
    }

    stored = encrypt_secrets(details)

    assert {k: stored[k] for k in ("host", "port", "database", "username", "ignore_secure_connection")} == {
        "host": "h", "port": 1, "database": "d", "username": "u", "ignore_secure_connection": True,
    }
    assert stored["password"] != "p"
    assert stored["certificate"] != CERTIFICATE
    assert decrypt_secrets(stored) == details


@pytest.mark.unit
def test_encryption_uses_a_fresh_nonce_each_time():
    first = encrypt_secrets({"password": "p"})["password"]
    second = encrypt_secrets({"password": "p"})["password"]

    assert first != second


@pytest.mark.unit
def test_safe_connection_details_drops_everything_else():
    stored = {
        "host": "h", "port": 1, "database": "d", "username": "u", "ignore_secure_connection": True,
        "password": "cipher", "certificate": "cipher", "legacy_token": "x",
    }

    assert safe_connection_details(stored) == {
        "host": "h", "port": 1, "database": "d", "username": "u", "ignore_secure_connection": True,
    }


@pytest.mark.unit
def test_scheduler_stub_is_a_no_op():
    assert register_connector_job(CONNECTOR_ID, None) is None


@pytest.mark.unit
def test_create_does_not_schedule_when_the_insert_fails(client, db):
    db.insert_connector.side_effect = RuntimeError("insert failed")
    failing_client = TestClient(forecast_app.app, raise_server_exceptions=False)

    response = failing_client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 500
    db.register.assert_not_called()


@pytest.mark.unit
def test_create_still_succeeds_when_scheduling_fails(client, db, caplog):
    """The connector is stored, so the call must not fail."""
    db.register.side_effect = RuntimeError("scheduler down")

    with caplog.at_level("ERROR"):
        response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 201
    assert response.json() == {"id": CONNECTOR_ID}
    assert "could not be scheduled" in caplog.text
    assert "s3cret" not in caplog.text


@pytest.mark.unit
def test_create_with_another_integrity_error_is_a_database_error(client, db):
    """A CHECK or NOT NULL violation is not a name conflict."""
    db.insert_connector.side_effect = IntegrityError(
        "INSERT", {}, MagicMock(pgcode="23514")
    )

    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)
    db.register.assert_not_called()


@pytest.mark.unit
def test_create_checks_only_after_taking_the_table_lock(client, db):
    """Two requests cannot both see an empty table: every check is made under the lock."""
    calls = []
    db.lock_connectors.side_effect = lambda *_: calls.append("lock")
    db.connector_id_exists.side_effect = lambda *_: calls.append("id_exists") or False
    db.connector_exists.side_effect = lambda *_: calls.append("exists") or False

    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 201
    assert calls == ["lock", "id_exists", "exists"]


@pytest.mark.unit
def test_put_with_another_integrity_error_is_a_database_error(client, db):
    db.get_connector.return_value = _stored_row()
    db.update_connector.side_effect = IntegrityError(
        "UPDATE", {}, MagicMock(pgcode="23502")
    )

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


@pytest.mark.unit
def test_put_replacing_an_unreadable_password_succeeds(client, db):
    db.get_connector.return_value = _stored_row(password="placeholder")
    db.get_connector.return_value.config["password"] = "not-valid-ciphertext!"

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"password": "fresh-pass"}},
    )

    assert response.status_code == 200
    config = db.update_connector.call_args.kwargs["config"]
    assert decrypt_secrets(config)["password"] == "fresh-pass"


@pytest.mark.unit
def test_put_keeping_unreadable_password_returns_400(client, db):
    db.get_connector.return_value = _stored_row(password="placeholder")
    db.get_connector.return_value.config["password"] = "not-valid-ciphertext!"

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": "new-host.example.com"}},
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "Stored secret could not be decrypted" in response.json()["error"]["message"]


@pytest.mark.unit
@pytest.mark.parametrize("key", ["", "x" * 31])
@pytest.mark.parametrize(
    "details",
    [{"password": "fresh-pass"}, {"host": "new-host.example.com", "password": "fresh-pass"}, {"port": 8472}],
)
def test_put_connection_details_without_a_usable_key_is_500_and_writes_nothing(client, db, caplog, key, details):
    db.get_connector.return_value = _stored_row()

    with patch.object(settings.forecast, "db_encryption_key", SecretStr(key)), caplog.at_level("ERROR"):
        response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": details})

    assert response.status_code == 500
    _assert_envelope(response.json(), "INTERNAL_SERVER_ERROR", 500)
    assert "The connector encryption key of the service is missing or invalid." in response.json()["error"]["message"]
    db.update_connector.assert_not_called()
    assert "DB_ENCRYPTION_KEY is missing or invalid" in caplog.text
    assert "fresh-pass" not in response.text and "fresh-pass" not in caplog.text


@pytest.mark.unit
def test_put_that_fails_to_encrypt_the_merged_details_is_500_and_writes_nothing(client, db):
    from forecast.utils.encryption import EncryptionKeyError

    db.get_connector.return_value = _stored_row()

    with patch("forecast.utils.api_utils.encrypt_secrets", side_effect=EncryptionKeyError("no key")):
        response = client.put(
            f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"password": "fresh-pass"}}
        )

    assert response.status_code == 500
    _assert_envelope(response.json(), "INTERNAL_SERVER_ERROR", 500)
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_rename_without_a_usable_key_still_succeeds(client, db):
    db.get_connector.return_value = _stored_row()

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert response.status_code == 200
    assert db.update_connector.call_args.kwargs["name"] == "renamed"


@pytest.mark.unit
def test_put_reads_the_connector_for_update(client, db):
    db.get_connector.return_value = _stored_row()

    client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert db.get_connector.call_args_list[-1].kwargs == {"for_update": True}


@pytest.mark.unit
def test_put_merges_and_writes_in_one_locked_transaction(client, db):
    """The config that is validated is the one that is read under the row lock.

    A second, unlocked read would let a concurrent PUT change the host between the
    host/username check and the write, and replay its password to the wrong host.
    """
    db.get_connector.return_value = _stored_row(host="old-host.example.com")
    sessions = []

    def record(session, *args, **kwargs):
        sessions.append(session)
        return db.get_connector.return_value

    db.get_connector.side_effect = record
    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": "new-host.example.com", "password": "new-pass"}},
    )

    assert response.status_code == 200
    db.get_connector.assert_called_once()
    assert db.get_connector.call_args.kwargs == {"for_update": True}
    assert db.update_connector.call_args.args[0] is sessions[0]


@pytest.mark.unit
def test_put_without_changes_reads_without_the_row_lock(client, db):
    db.get_connector.return_value = _stored_row()

    client.put(f"/v1/connectors/{CONNECTOR_ID}", json={})

    db.get_connector.assert_called_once()
    assert db.get_connector.call_args.kwargs == {"for_update": False}


@pytest.mark.unit
def test_database_error_on_create_does_not_expose_the_parameters(db, caplog):
    db.insert_connector.side_effect = OperationalError(
        "INSERT INTO connectors", {"config": "SECRETCIPHER"}, Exception("connection lost")
    )
    failing_client = TestClient(forecast_app.app, raise_server_exceptions=False)

    with caplog.at_level("ERROR"):
        response = failing_client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)
    assert "SECRETCIPHER" not in response.text
    assert "SECRETCIPHER" not in caplog.text
    assert "OperationalError" in caplog.text


@pytest.mark.unit
def test_database_error_on_put_does_not_expose_the_parameters(client, db, caplog):
    db.get_connector.return_value = _stored_row()
    db.update_connector.side_effect = OperationalError(
        "UPDATE connectors", {"config": "SECRETCIPHER"}, Exception("connection lost")
    )

    with caplog.at_level("ERROR"):
        response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)
    assert "SECRETCIPHER" not in response.text
    assert "SECRETCIPHER" not in caplog.text


@pytest.mark.unit
def test_database_lock_timeout_returns_409(client, db):
    orig_exc = MagicMock()
    orig_exc.pgcode = "55P03"
    db.get_connector.return_value = _stored_row()
    db.update_connector.side_effect = OperationalError(
        "UPDATE connectors", {}, orig_exc
    )

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)
    assert "currently locked by a concurrent operation" in response.json()["error"]["message"]


@pytest.mark.unit
def test_database_error_on_list_returns_database_error(client, db):
    db.list_connectors.side_effect = OperationalError(
        "SELECT connectors", {}, Exception("connection lost")
    )

    response = client.get("/v1/connectors")

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


@pytest.mark.unit
def test_database_error_on_get_returns_database_error(client, db):
    db.get_connector.side_effect = OperationalError(
        "SELECT connector", {}, Exception("connection lost")
    )

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


@pytest.mark.unit
def test_database_error_on_metrics_returns_database_error(client, db):
    db.get_connector.side_effect = OperationalError(
        "SELECT connector", {}, Exception("connection lost")
    )

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}/metrics")

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)

@pytest.mark.unit
def test_put_empty_connection_details_does_not_touch_the_secrets(client, db):
    row = _stored_row()
    db.get_connector.return_value = row

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {}})

    assert response.status_code == 200
    assert db.update_connector.call_args.kwargs["config"] == row.config


@pytest.mark.unit
def test_create_logs_the_outcome_without_the_secret(client, db, caplog):
    with caplog.at_level("INFO"):
        response = client.post("/v1/connectors", json=_create_body(id=CONNECTOR_ID))

    assert response.status_code == 201
    assert f"Connector {CONNECTOR_ID} ('prod-sales-db') created" in caplog.text
    assert "s3cret" not in caplog.text


@pytest.mark.unit
def test_invalid_put_is_logged_without_the_secret(client, db, caplog):
    db.get_connector.return_value = _stored_row()

    with caplog.at_level("WARNING"):
        response = client.put(
            f"/v1/connectors/{CONNECTOR_ID}",
            json={"connection_details": {"host": "", "password": "s3cret-new"}},
        )

    assert response.status_code == 400
    assert f"Update of connector {CONNECTOR_ID} rejected" in caplog.text
    assert "s3cret" not in caplog.text


@pytest.mark.unit
@pytest.mark.parametrize(
    "details",
    [{"host": 123}, {"port": "x"}, {"database": ["d"]}],
)
def test_put_with_a_wrong_type_is_400_like_any_invalid_value(client, db, details):
    db.get_connector.return_value = _stored_row()

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": details})

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_stores_the_validated_port(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"port": "9000", "password": "s3cret"}},
    )

    assert response.status_code == 200
    assert db.update_connector.call_args.kwargs["config"]["port"] == 9000


@pytest.mark.unit
def test_timestamps_are_returned_in_utc(client, db):
    local = timezone(timedelta(hours=-4))
    db.get_connector.return_value = _stored_row(
        created_at=datetime(2026, 10, 26, 4, 55, tzinfo=local),
        updated_at=datetime(2026, 10, 26, 4, 55, tzinfo=local),
    )

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 200
    assert response.json()["created_at"] == "2026-10-26T08:55:00Z"
    assert response.json()["updated_at"] == "2026-10-26T08:55:00Z"


@pytest.mark.unit
def test_create_returns_409_when_the_insert_hits_a_lock_timeout(db):
    orig_exc = MagicMock()
    orig_exc.pgcode = "55P03"
    db.insert_connector.side_effect = OperationalError(
        "LOCK TABLE connectors", {}, orig_exc
    )
    client = TestClient(forecast_app.app, raise_server_exceptions=False)

    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)


@pytest.mark.unit
def test_create_returns_409_when_the_table_lock_times_out(db):
    """The lock timeout fires at LOCK TABLE, before anything is inserted."""
    orig_exc = MagicMock()
    orig_exc.pgcode = "55P03"
    db.lock_connectors.side_effect = OperationalError("LOCK TABLE connectors", {}, orig_exc)
    client = TestClient(forecast_app.app, raise_server_exceptions=False)

    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)
    db.insert_connector.assert_not_called()
    db.register.assert_not_called()


@pytest.mark.unit
def test_timestamp_treats_a_naive_value_as_utc():
    assert format_timestamp(datetime(2026, 10, 26, 8, 55)) == "2026-10-26T08:55:00Z"
    assert format_timestamp(None) is None


@pytest.mark.unit
def test_lock_timeout_with_psycopg3_sqlstate_returns_409(client, db):
    orig_exc = MagicMock()
    orig_exc.pgcode = None
    orig_exc.sqlstate = "55P03"
    db.get_connector.return_value = _stored_row()
    db.update_connector.side_effect = OperationalError(
        "UPDATE connectors", {}, orig_exc
    )

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)


@pytest.mark.unit
def test_database_pool_timeout_returns_429_server_busy(client, db):
    from sqlalchemy.exc import TimeoutError as PoolTimeoutError

    db.get_connector.side_effect = PoolTimeoutError("QueuePool limit exceeded")

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 429
    _assert_envelope(response.json(), "SERVER_BUSY", 429)


@pytest.mark.unit
@pytest.mark.parametrize(
    "invalid_host",
    ["host with space", "host;injection", "host\nnewline", "host`whoami`"],
)
def test_create_rejects_invalid_host_characters(client, db, invalid_host):
    body = _create_body()
    body["connection_details"]["host"] = invalid_host

    response = client.post("/v1/connectors", json=body)

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


@pytest.mark.unit
@pytest.mark.parametrize(
    "field,value",
    [
        ("host", "a" * 256),
        ("database", "a" * 129),
        ("username", "a" * 129),
        ("password", "a" * 1025),
    ],
)
def test_create_rejects_oversized_connection_details(client, db, field, value):
    body = _create_body()
    body["connection_details"][field] = value

    response = client.post("/v1/connectors", json=body)

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


def _certificate_of_length(length):
    """A value with both PEM markers and exactly `length` characters."""
    head, tail = "-----BEGIN CERTIFICATE-----\n", "\n-----END CERTIFICATE-----"
    return head + "A" * (length - len(head) - len(tail)) + tail


@pytest.mark.unit
@pytest.mark.parametrize(
    "field,value",
    [
        ("certificate", "not a certificate"),
        ("certificate", ""),
        ("certificate", "-----BEGIN CERTIFICATE-----\nno end marker"),
        ("certificate", "no begin marker\n-----END CERTIFICATE-----"),
        ("certificate", _certificate_of_length(65537)),
        ("certificate", CERTIFICATE + "a" * 65536),
        ("certificate", 42),
        ("ignore_secure_connection", "true"),
        ("ignore_secure_connection", 1),
        ("ignore_secure_connection", None),
    ],
)
def test_create_rejects_invalid_tls_details(client, db, field, value):
    body = _create_body()
    body["connection_details"][field] = value

    response = client.post("/v1/connectors", json=body)

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)
    if isinstance(value, str) and value:
        # The message names the field and the problem, never the value that was sent.
        assert value not in response.text
    db.insert_connector.assert_not_called()


@pytest.mark.unit
def test_create_accepts_a_certificate_of_the_maximum_length(client, db):
    certificate = _certificate_of_length(65536)
    body = _create_body()
    body["connection_details"]["certificate"] = certificate

    response = client.post("/v1/connectors", json=body)

    assert response.status_code == 201
    config = db.insert_connector.call_args.kwargs["config"]
    assert decrypt_secrets(config)["certificate"] == certificate


@pytest.mark.unit
def test_create_without_tls_details_defaults_to_verifying_the_server(client, db):
    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 201
    config = db.insert_connector.call_args.kwargs["config"]
    assert config["ignore_secure_connection"] is False
    assert "certificate" not in config


@pytest.mark.unit
def test_decrypt_value_failures_raise_decryption_error():
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    from forecast.utils.encryption import DecryptionError, _decrypt_value

    cipher = AESGCM(KEY.encode())

    # Invalid base64
    with pytest.raises(DecryptionError):
        _decrypt_value(cipher, "not-valid-base64!@#$")

    # Truncated ciphertext (< 12 bytes nonce + 16 bytes tag)
    with pytest.raises(DecryptionError):
        _decrypt_value(cipher, "AQIDBA==")

    # Tampered ciphertext (valid base64 but corrupted auth tag)
    import base64
    tampered = base64.b64encode(b"0" * 32).decode()
    with pytest.raises(DecryptionError):
        _decrypt_value(cipher, tampered)

    # Wrong key
    other_cipher = AESGCM(b"9876543210fedcba9876543210fedcba")
    valid_token = encrypt_secrets({"password": "secret"})["password"]
    with pytest.raises(DecryptionError):
        _decrypt_value(other_cipher, valid_token)


@pytest.mark.unit
@pytest.mark.parametrize(
    "change",
    [
        {"host": "new-host.example.com"},
        {"port": 9000},
        {"database": "OTHERDB"},
        {"username": "other_user"},
    ],
)
def test_put_changing_the_target_without_password_fails_400(client, db, change):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": change}
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert (
        "Password must be supplied when host, port, database, username, "
        "ignore_secure_connection or certificate changes"
    ) in response.json()["error"]["message"]
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_changing_host_with_password_succeeds(client, db):
    db.get_connector.return_value = _stored_row(host="old-host.example.com")

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": "new-host.example.com", "password": "new-password"}},
    )

    assert response.status_code == 200
    db.update_connector.assert_called_once()


@pytest.mark.unit
def test_put_turning_on_ignore_secure_connection_without_password_fails_400(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"ignore_secure_connection": True}}
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "ignore_secure_connection" in response.json()["error"]["message"]
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_turning_on_ignore_secure_connection_with_password_stores_it_in_plain_text(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"ignore_secure_connection": True, "password": "new-pass"}},
    )

    assert response.status_code == 200
    config = db.update_connector.call_args.kwargs["config"]
    assert config["ignore_secure_connection"] is True
    assert decrypt_secrets(config)["password"] == "new-pass"


@pytest.mark.unit
@pytest.mark.parametrize("stored", [None, False, True], ids=["unset", "false", "true"])
def test_put_repeating_the_stored_ignore_secure_connection_without_password_succeeds(client, db, stored):
    # A connector stored before the field existed has it unset, which means false.
    db.get_connector.return_value = _stored_row(ignore_secure_connection=stored)
    repeated = bool(stored)

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"ignore_secure_connection": repeated}}
    )

    assert response.status_code == 200
    assert db.update_connector.call_args.kwargs["config"]["ignore_secure_connection"] is repeated


@pytest.mark.unit
def test_put_turning_off_ignore_secure_connection_without_password_fails_400(client, db):
    db.get_connector.return_value = _stored_row(ignore_secure_connection=True)

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"ignore_secure_connection": False}}
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "ignore_secure_connection" in response.json()["error"]["message"]
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_certificate_without_password_fails_400(client, db):
    db.get_connector.return_value = _stored_row(certificate=CERTIFICATE)

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"certificate": CERTIFICATE}}
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "certificate" in response.json()["error"]["message"]
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_certificate_with_password_encrypts_it(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"certificate": CERTIFICATE, "password": "new-pass"}},
    )

    assert response.status_code == 200
    config = db.update_connector.call_args.kwargs["config"]
    assert config["certificate"] != CERTIFICATE
    assert decrypt_secrets(config)["certificate"] == CERTIFICATE
    assert CERTIFICATE not in response.text


@pytest.mark.unit
def test_put_keeps_a_stored_certificate_when_other_fields_change(client, db):
    row = _stored_row(certificate=CERTIFICATE)
    db.get_connector.return_value = row

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"password": "new-pass"}}
    )

    assert response.status_code == 200
    config = db.update_connector.call_args.kwargs["config"]
    assert config["certificate"] == row.config["certificate"]
    assert decrypt_secrets(config)["certificate"] == CERTIFICATE


@pytest.mark.unit
def test_put_clearing_the_certificate_with_password_stores_null(client, db):
    db.get_connector.return_value = _stored_row(certificate=CERTIFICATE)

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"certificate": None, "password": "new-pass"}},
    )

    assert response.status_code == 200
    config = db.update_connector.call_args.kwargs["config"]
    # The key stays, with the value null: a null certificate means no certificate.
    assert "certificate" in config and config["certificate"] is None


@pytest.mark.unit
def test_put_clearing_the_certificate_without_password_fails_400(client, db):
    db.get_connector.return_value = _stored_row(certificate=CERTIFICATE)

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"certificate": None}}
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "certificate" in response.json()["error"]["message"]
    db.update_connector.assert_not_called()


@pytest.mark.unit
@pytest.mark.parametrize(
    "change,leaked",
    [
        ({"certificate": "not a certificate"}, "not a certificate"),
        ({"certificate": 42}, "42"),
        ({"ignore_secure_connection": "yes"}, "yes"),
        ({"ignore_secure_connection": None}, None),
    ],
)
def test_put_with_an_invalid_tls_value_is_400_and_writes_nothing(client, db, change, leaked):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {**change, "password": "new-pass"}},
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    # The message names the field and the problem, never the value that was sent.
    assert "new-pass" not in response.text
    if leaked is not None:
        assert f"'{leaked}'" not in response.text and f'"{leaked}"' not in response.text
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_unchanged_host_without_password_succeeds(client, db):
    db.get_connector.return_value = _stored_row(host="ibmi.example.com")

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": "ibmi.example.com"}},
    )

    assert response.status_code == 200
    db.update_connector.assert_called_once()
