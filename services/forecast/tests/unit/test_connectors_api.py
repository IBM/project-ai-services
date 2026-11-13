"""
Unit tests for the connector API and the connector secret handling.

The database is mocked at the connectors DB layer, so the tests run without
PostgreSQL. Tests cover:
 - POST /v1/connectors: create, single-connector 409, queue 429, body validation,
   encrypted storage and the scheduler call
 - GET /v1/connectors and GET /v1/connectors/{id}: pagination, name filter, safe fields
 - PUT /v1/connectors/{id}: partial merge, re-encryption, validation, rejected fields
 - GET /v1/connectors/{id}/metrics: versions and training summary
 - Encryption helpers: round trip, allowlist, nonce
"""

from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from unittest.mock import DEFAULT, MagicMock, patch

import pytest
from fastapi.testclient import TestClient
from pydantic import SecretStr
from sqlalchemy.exc import IntegrityError

# Imported at collection time so the "forecast_server" logger exists before pytest
# attaches its capture handlers (see test_app.py).
import forecast.app as forecast_app
import forecast.api.v1.connectors as connectors_api
import forecast.db.connectors as connectors_db
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
    """Valid key, stubbed TRITON_URL check, and stubbed Db2 probe."""
    with patch.object(
        settings.forecast, "db_encryption_key", SecretStr(KEY)
    ), patch("forecast.app.triton_url_check", return_value=None), patch(
        "forecast.api.v1.connectors.probe_db2_connector"
    ) as mock_probe:
        yield mock_probe


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
        "count_queued_jobs": 0,
        "insert_connector": None,
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


def _create_body(**overrides):
    body = {
        "id": CONNECTOR_ID,
        "name": "prod-sales-db",
        "type": "db2_for_i",
        "schedule": "daily",
        "connection_details": {
            "host": "ibmi.example.com",
            "port": 8471,
            "database": "PRODDB",
            "username": "forecast_ro",
            "password": "s3cret",
        },
        "training": {
            "table": "ORDERS",
            "target_column": "DELIVERY_LATE",
            "input_columns": [{"name": "REGION"}, {"name": "QUANTITY", "type": "integer"}],
        },
    }
    body.update(overrides)
    return body


def _stored_row(password="s3cret", **overrides):
    """A connector as the database returns it, with the password encrypted."""
    config = encrypt_secrets(
        {
            "host": "ibmi.example.com",
            "port": 8471,
            "database": "PRODDB",
            "username": "forecast_ro",
            "password": password,
        }
    )
    row = {
        "id": CONNECTOR_ID,
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
    row.update(overrides)
    return row


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
    assert kwargs["source_table"] == "ORDERS"
    assert kwargs["target_column"] == "DELIVERY_LATE"
    assert kwargs["input_columns"] == [{"name": "REGION"}, {"name": "QUANTITY", "type": "integer"}]
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
def test_create_when_a_connector_exists_is_409(client, db):
    db.connector_exists.return_value = True

    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)
    db.insert_connector.assert_not_called()
    db.register.assert_not_called()


@pytest.mark.unit
def test_create_when_the_queue_is_full_is_429(client, db):
    db.count_queued_jobs.return_value = settings.forecast.max_queued_jobs

    response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 429
    _assert_envelope(response.json(), "SERVER_BUSY", 429)
    db.insert_connector.assert_not_called()


@pytest.mark.unit
@pytest.mark.parametrize(
    "overrides",
    [
        {"type": "oracle"},
        {"schedule": "hourly-ish"},
        {"id": "not-a-uuid"},
        {"name": ""},
        {"name": "x" * 256},
        {"unknown": "field"},
        {"connection_details": {"host": "h", "database": "d", "username": "u", "password": "p", "extra": 1}},
        {"connection_details": {"host": "h", "database": "d", "username": "u"}},
        {"training": {"table": "T", "target_column": "C", "input_columns": [{"name": "A"}], "validation_metric": "accuracy"}},
        {"training": {"table": "T", "target_column": "C", "input_columns": [{"name": "A", "type": "blob"}]}},
        {"training": {"table": "T", "target_column": "C", "input_columns": []}},
        {"training": {"table": "T; DROP TABLE", "target_column": "C", "input_columns": [{"name": "A"}]}},
        {"training": {"table": "T", "target_column": "C' OR 1=1", "input_columns": [{"name": "A"}]}},
        {"training": {"table": "T", "target_column": "C", "input_columns": [{"name": "BAD NAME"}]}},
        {"training": {"table": "T\n", "target_column": "C", "input_columns": [{"name": "A"}]}},
        {"training": {"table": "T", "target_column": "C", "input_columns": [{"name": "COL1"}, {"name": "col1"}]}},
        {"training": {"table": "T", "target_column": "TARGET_COL", "input_columns": [{"name": "target_col"}, {"name": "OTHER"}]}},
    ],
)
def test_create_rejects_invalid_bodies_with_422(client, db, overrides):
    response = client.post("/v1/connectors", json=_create_body(**overrides))

    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)
    db.insert_connector.assert_not_called()


@pytest.mark.unit
def test_create_without_a_key_is_500_and_does_not_leak_the_password(db):
    failing_client = TestClient(forecast_app.app, raise_server_exceptions=False)

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        response = failing_client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 500
    _assert_envelope(response.json(), "INTERNAL_SERVER_ERROR", 500)
    assert "s3cret" not in response.text
    db.insert_connector.assert_not_called()


# ---------------------------------------------------------------------------
# GET /v1/connectors
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_list_returns_the_pagination_envelope(client, db):
    db.list_connectors.return_value = (
        [
            {
                "id": CONNECTOR_ID,
                "name": "prod-sales-db",
                "type": "db2_for_i",
                "status": "ACCEPTED",
                "message": None,
                "created_at": CREATED,
                "source_table": "ORDERS",
                "target_column": "DELIVERY_LATE",
                "last_trained_at": None,
            }
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
def test_get_returns_safe_details_only(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 200
    body = response.json()
    assert body["connection_details"] == {
        "host": "ibmi.example.com",
        "port": 8471,
        "database": "PRODDB",
        "username": "forecast_ro",
    }
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
def test_put_null_port_clears_the_port_and_keeps_the_password(client, db):
    db.get_connector.return_value = _stored_row()

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"port": None}}
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
    assert kwargs["config"] == row["config"]


@pytest.mark.unit
def test_put_with_a_name_that_is_taken_is_409(client, db):
    db.get_connector.return_value = _stored_row()
    db.update_connector.side_effect = IntegrityError("UPDATE", {}, Exception("duplicate key"))

    response = client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "taken"})

    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_CONFLICT", 409)
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
    [{"type": "db2_for_i"}, {"training": {"table": "T", "target_column": "C", "input_columns": []}}],
)
def test_put_rejects_type_and_training(client, db, body):
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
        {
            "seq": 3, "status": "FAILED", "message": "timeout", "created_at": CREATED,
            "started_at": CREATED, "finished_at": datetime(2026, 10, 28, tzinfo=timezone.utc),
            "result": {"rows_read": 10, "attempts": 2, "training_seconds": 99},
        },
        {
            "seq": 2, "status": "COMPLETED", "message": None, "created_at": CREATED,
            "started_at": CREATED, "finished_at": datetime(2026, 10, 27, tzinfo=timezone.utc),
            "result": {"rows_read": 120000, "algorithm": "GBM", "training_seconds": 412, "attempts": 1},
        },
        {
            "seq": 1, "status": "COMPLETED", "message": None, "created_at": CREATED,
            "started_at": CREATED, "finished_at": datetime(2026, 10, 26, 9, tzinfo=timezone.utc),
            "result": {"rows_read": 100000, "algorithm": "GBM", "training_seconds": 400, "attempts": 1},
        },
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
    db.list_jobs.return_value = [
        {
            "seq": 1, "status": "COMPLETED", "message": None, "created_at": CREATED,
            "started_at": CREATED, "finished_at": CREATED, "result": None,
        },
    ]

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}/metrics")

    assert response.status_code == 200
    training = response.json()["training"]
    assert training["jobs_completed"] == 1
    assert training["last_duration_seconds"] is None


@pytest.mark.unit
def test_metrics_failed_newest_job_without_training_seconds_has_no_duration(client, db):
    db.get_connector.return_value = _stored_row()
    db.list_jobs.return_value = [
        {
            "seq": 2, "status": "FAILED", "message": "timeout", "created_at": CREATED,
            "started_at": CREATED, "finished_at": CREATED, "result": {"attempts": 3},
        },
        {
            "seq": 1, "status": "COMPLETED", "message": None, "created_at": CREATED,
            "started_at": CREATED, "finished_at": datetime(2026, 10, 27, tzinfo=timezone.utc),
            "result": {"training_seconds": 400},
        },
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
def test_only_the_password_is_encrypted():
    details = {"host": "h", "port": 1, "database": "d", "username": "u", "password": "p"}

    stored = encrypt_secrets(details)

    assert {k: stored[k] for k in ("host", "port", "database", "username")} == {
        "host": "h", "port": 1, "database": "d", "username": "u",
    }
    assert stored["password"] != "p"
    assert decrypt_secrets(stored) == details


@pytest.mark.unit
def test_encryption_uses_a_fresh_nonce_each_time():
    first = encrypt_secrets({"password": "p"})["password"]
    second = encrypt_secrets({"password": "p"})["password"]

    assert first != second


@pytest.mark.unit
def test_safe_connection_details_drops_everything_else():
    stored = {"host": "h", "port": 1, "database": "d", "username": "u", "password": "cipher", "legacy_token": "x"}

    assert safe_connection_details(stored) == {"host": "h", "port": 1, "database": "d", "username": "u"}


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
def test_put_replacing_an_unreadable_password_succeeds(client, db):
    db.get_connector.return_value = _stored_row(password="placeholder")
    db.get_connector.return_value["config"]["password"] = "not-valid-ciphertext!"

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
    db.get_connector.return_value["config"]["password"] = "not-valid-ciphertext!"

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": "new-host.example.com"}},
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "Stored secret could not be decrypted" in response.json()["error"]["message"]


@pytest.mark.unit
def test_put_reads_the_connector_for_update(client, db):
    db.get_connector.return_value = _stored_row()

    client.put(f"/v1/connectors/{CONNECTOR_ID}", json={"name": "renamed"})

    assert db.get_connector.call_args_list[-1].kwargs == {"for_update": True}


@pytest.mark.unit
def test_database_error_on_create_does_not_expose_the_parameters(db, caplog):
    from sqlalchemy.exc import OperationalError

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
    from sqlalchemy.exc import OperationalError

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
    from unittest.mock import MagicMock
    from sqlalchemy.exc import OperationalError

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
    from sqlalchemy.exc import OperationalError

    db.list_connectors.side_effect = OperationalError(
        "SELECT connectors", {}, Exception("connection lost")
    )

    response = client.get("/v1/connectors")

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


@pytest.mark.unit
def test_database_error_on_get_returns_database_error(client, db):
    from sqlalchemy.exc import OperationalError

    db.get_connector.side_effect = OperationalError(
        "SELECT connector", {}, Exception("connection lost")
    )

    response = client.get(f"/v1/connectors/{CONNECTOR_ID}")

    assert response.status_code == 500
    _assert_envelope(response.json(), "DATABASE_ERROR", 500)


@pytest.mark.unit
def test_database_error_on_metrics_returns_database_error(client, db):
    from sqlalchemy.exc import OperationalError

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
    assert db.update_connector.call_args.kwargs["config"] == row["config"]


@pytest.mark.unit
def test_create_logs_the_outcome_without_the_secret(client, db, caplog):
    with caplog.at_level("INFO"):
        response = client.post("/v1/connectors", json=_create_body(id=CONNECTOR_ID))

    assert response.status_code == 201
    assert f"Connector {CONNECTOR_ID} ('prod-sales-db') created" in caplog.text
    assert "s3cret" not in caplog.text


@pytest.mark.unit
def test_rejected_create_is_logged(client, db, caplog):
    db.connector_exists.return_value = True

    with caplog.at_level("WARNING"):
        response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 409
    assert "a connector already exists" in caplog.text
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
        f"/v1/connectors/{CONNECTOR_ID}", json={"connection_details": {"port": "9000"}}
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
def test_create_returns_409_on_database_lock_timeout(db):
    from sqlalchemy.exc import OperationalError

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
def test_lock_timeout_with_psycopg3_sqlstate_returns_409(client, db):
    from sqlalchemy.exc import OperationalError

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
def test_create_fails_and_does_not_insert_when_db2_probe_fails(client, db):
    from fastapi import HTTPException

    with patch(
        "forecast.api.v1.connectors.probe_db2_connector",
        side_effect=HTTPException(
            status_code=400,
            detail={"error": {"code": "INVALID_REQUEST", "message": "Cannot connect to Db2", "status": 400}},
        ),
    ):
        response = client.post("/v1/connectors", json=_create_body())

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    db.insert_connector.assert_not_called()
    db.register.assert_not_called()


@pytest.mark.unit
def test_put_fails_and_does_not_update_when_db2_probe_fails(client, db):
    from fastapi import HTTPException

    db.get_connector.return_value = _stored_row()
    with patch(
        "forecast.api.v1.connectors.probe_db2_connector",
        side_effect=HTTPException(
            status_code=400,
            detail={"error": {"code": "INVALID_REQUEST", "message": "Authentication failed", "status": 400}},
        ),
    ):
        response = client.put(
            f"/v1/connectors/{CONNECTOR_ID}",
            json={"connection_details": {"password": "new-pass"}},
        )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    db.update_connector.assert_not_called()


@pytest.mark.unit
def test_put_changing_host_without_password_fails_400(client, db):
    db.get_connector.return_value = _stored_row(host="old-host.example.com")

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": "new-host.example.com"}},
    )

    assert response.status_code == 400
    _assert_envelope(response.json(), "INVALID_REQUEST", 400)
    assert "Password must be supplied when host or username changes" in response.json()["error"]["message"]
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
def test_create_calls_db2_probe_with_table_and_columns(client, db):
    with patch("forecast.api.v1.connectors.probe_db2_connector") as mock_probe:
        payload = _create_body()
        response = client.post("/v1/connectors", json=payload)
        assert response.status_code == 201

        mock_probe.assert_called_once_with(
            host=payload["connection_details"]["host"],
            port=payload["connection_details"]["port"],
            database=payload["connection_details"]["database"],
            username=payload["connection_details"]["username"],
            password=payload["connection_details"]["password"],
            table=payload["training"]["table"],
            columns=["REGION", "QUANTITY", "DELIVERY_LATE"],
        )


@pytest.mark.unit
def test_put_calls_db2_probe_with_table_and_no_columns(client, db):
    db.get_connector.return_value = _stored_row(source_table="MYLIB.SALES")
    with patch("forecast.api.v1.connectors.probe_db2_connector") as mock_probe:
        response = client.put(
            f"/v1/connectors/{CONNECTOR_ID}",
            json={"connection_details": {"password": "updated-password"}},
        )
        assert response.status_code == 200

        mock_probe.assert_called_once_with(
            host="ibmi.example.com",
            port=8471,
            database="PRODDB",
            username="forecast_ro",
            password="updated-password",
            table="MYLIB.SALES",
            columns=None,
        )


@pytest.mark.unit
def test_put_unchanged_host_without_password_succeeds(client, db):
    db.get_connector.return_value = _stored_row(host="ibmi.example.com")

    response = client.put(
        f"/v1/connectors/{CONNECTOR_ID}",
        json={"connection_details": {"host": "ibmi.example.com"}},
    )

    assert response.status_code == 200
    db.update_connector.assert_called_once()
