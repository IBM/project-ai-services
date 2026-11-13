"""
Unit tests for the Forecast service skeleton.

Tests cover:
 - GET /health: fixed ok response
 - Middleware: X-Request-ID injection
 - Error envelope for unknown paths, wrong methods, validation errors and
   unexpected exceptions
 - TRITON_UNAVAILABLE
 - Settings defaults and the blank-means-unset rule
 - Startup checks: DB_ENCRYPTION_KEY validation and the TRITON_URL check
 - The worker command line
"""

import logging
from unittest.mock import patch

import pytest
from fastapi import FastAPI, HTTPException
from fastapi.testclient import TestClient
from pydantic import BaseModel, SecretStr

# Imported here, at collection time, so the "forecast_server" logger already exists
# when pytest attaches its capture handlers. Imported inside a test instead, the
# logger would be created after the handler was attached, and caplog would miss its
# records in whichever test imports the app first.
import forecast.app  # noqa: F401
from forecast.errors import (
    SERVICE_UNAVAILABLE,
    TRITON_UNAVAILABLE,
    raise_triton_unavailable,
    register_exception_handlers,
)


@pytest.fixture(autouse=True)
def _startup_dependencies_stubbed():
    """Give every app startup a valid key and a stubbed TRITON_URL check.

    Startup validates DB_ENCRYPTION_KEY and checks TRITON_URL, so tests that start
    the app need both settled. Tests that need another value patch it again.
    """
    from forecast.settings import settings

    with patch.object(
        settings.forecast, "db_encryption_key", SecretStr("0123456789abcdef0123456789abcdef")
    ), patch("forecast.app.triton_url_check", return_value=None):
        yield


@pytest.fixture
def client():
    """TestClient for the real app, with the database close patched out."""
    import forecast.app as forecast_app

    with patch.object(forecast_app, "close_db_connections"):
        with TestClient(forecast_app.app) as test_client:
            yield test_client


def _assert_envelope(body: dict, code: str, status: int):
    assert set(body) == {"error"}
    assert body["error"]["code"] == code
    assert body["error"]["status"] == status
    assert isinstance(body["error"]["message"], str) and body["error"]["message"]


# ---------------------------------------------------------------------------
# /health
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_health_ok(client):
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}



# ---------------------------------------------------------------------------
# Middleware
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_request_id_is_generated(client):
    response = client.get("/health")
    assert response.headers["X-Request-ID"]


@pytest.mark.unit
def test_request_id_is_echoed(client):
    response = client.get("/health", headers={"X-Request-ID": "abc-123"})
    assert response.headers["X-Request-ID"] == "abc-123"


@pytest.mark.unit
def test_empty_request_id_is_replaced(client):
    response = client.get("/health", headers={"X-Request-ID": ""})
    assert response.headers["X-Request-ID"]


# ---------------------------------------------------------------------------
# Error envelope
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_unknown_path_uses_envelope(client):
    response = client.get("/v1/nothing")
    assert response.status_code == 404
    _assert_envelope(response.json(), "RESOURCE_NOT_FOUND", 404)


@pytest.mark.unit
def test_wrong_method_uses_envelope(client):
    response = client.post("/health")
    assert response.status_code == 405
    _assert_envelope(response.json(), "METHOD_NOT_ALLOWED", 405)
    assert response.headers["Allow"] == "GET"


@pytest.fixture
def error_app():
    """A small app that uses the same handlers and raises each kind of error."""

    class Body(BaseModel):
        name: str
        count: int

    app = FastAPI()
    register_exception_handlers(app)

    @app.post("/validate")
    def validate(body: Body):
        return body

    @app.get("/triton-bare")
    def triton_bare():
        raise_triton_unavailable()

    @app.get("/triton")
    def triton():
        raise_triton_unavailable("connection refused")

    @app.get("/http")
    def http_error():
        raise HTTPException(status_code=409, detail="already exists")

    @app.get("/route-405")
    def route_405():
        raise HTTPException(status_code=405, detail="not here", headers={"Allow": "POST"})

    @app.get("/plain-503")
    def plain_503():
        raise HTTPException(status_code=503, detail="not ready")

    @app.get("/route-429")
    def route_429():
        raise HTTPException(status_code=429, detail="queue is full")

    @app.get("/route-415")
    def route_415():
        raise HTTPException(status_code=415, detail="wrong type")

    @app.get("/teapot")
    def teapot():
        raise HTTPException(status_code=418, detail="teapot")

    @app.get("/structured")
    def structured():
        raise HTTPException(
            status_code=409,
            detail={"error": {"code": "CUSTOM", "message": "kept as is", "status": 409}},
        )

    @app.get("/boom")
    def boom():
        raise RuntimeError("secret detail that must not leak")

    return TestClient(app, raise_server_exceptions=False)


@pytest.mark.unit
def test_validation_error_is_422_with_invalid_request(error_app):
    response = error_app.post("/validate", json={"name": "x", "count": "not-a-number"})
    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)
    assert "count" in response.json()["error"]["message"]


@pytest.mark.unit
def test_missing_field_is_422(error_app):
    response = error_app.post("/validate", json={})
    assert response.status_code == 422
    _assert_envelope(response.json(), "INVALID_REQUEST", 422)


@pytest.mark.unit
def test_triton_unavailable_is_503(error_app):
    response = error_app.get("/triton")
    assert response.status_code == 503
    _assert_envelope(response.json(), TRITON_UNAVAILABLE, 503)
    assert "connection refused" in response.json()["error"]["message"]


@pytest.mark.unit
def test_triton_unavailable_without_detail(error_app):
    response = error_app.get("/triton-bare")
    assert response.status_code == 503
    _assert_envelope(response.json(), TRITON_UNAVAILABLE, 503)
    assert response.json()["error"]["message"] == "Triton cannot be reached"


@pytest.mark.unit
def test_http_exception_uses_envelope(error_app):
    response = error_app.get("/http")
    assert response.status_code == 409
    _assert_envelope(response.json(), "RESOURCE_LOCKED", 409)


@pytest.mark.unit
def test_405_raised_by_a_route_is_method_not_allowed(error_app):
    response = error_app.get("/route-405")
    assert response.status_code == 405
    _assert_envelope(response.json(), "METHOD_NOT_ALLOWED", 405)
    assert response.headers["Allow"] == "POST"


@pytest.mark.unit
def test_429_is_server_busy(error_app):
    response = error_app.get("/route-429")
    assert response.status_code == 429
    _assert_envelope(response.json(), "SERVER_BUSY", 429)


@pytest.mark.unit
def test_415_is_unsupported_media_type(error_app):
    response = error_app.get("/route-415")
    assert response.status_code == 415
    _assert_envelope(response.json(), "UNSUPPORTED_MEDIA_TYPE", 415)


@pytest.mark.unit
def test_plain_503_is_service_unavailable(error_app):
    response = error_app.get("/plain-503")
    assert response.status_code == 503
    _assert_envelope(response.json(), SERVICE_UNAVAILABLE, 503)


@pytest.mark.unit
def test_unlisted_status_falls_back_to_internal_server_error(error_app):
    response = error_app.get("/teapot")
    assert response.status_code == 418
    _assert_envelope(response.json(), "INTERNAL_SERVER_ERROR", 418)


@pytest.mark.unit
def test_structured_detail_is_passed_through(error_app):
    response = error_app.get("/structured")
    assert response.status_code == 409
    assert response.json() == {
        "error": {"code": "CUSTOM", "message": "kept as is", "status": 409}
    }


@pytest.mark.unit
def test_unexpected_exception_is_500_without_details(error_app):
    response = error_app.get("/boom")
    assert response.status_code == 500
    _assert_envelope(response.json(), "INTERNAL_SERVER_ERROR", 500)
    assert "secret detail" not in response.text


# ---------------------------------------------------------------------------
# Settings
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_settings_defaults(monkeypatch):
    for name in (
        "MODEL_REPO_PATH", "SCRATCH_DIR", "DATA_DIR", "TRITON_URL", "TRITON_STARTUP_WAIT_S",
        "TRITON_LOAD_TIMEOUT_S", "TRITON_INFER_TIMEOUT_S", "FORECASTER_RETENTION_COUNT",
        "DEFAULT_TIME_LIMIT_SECONDS", "TRAINING_MAX_ATTEMPTS",
        "TRAINING_RETRY_DELAY_SECONDS", "MAX_QUEUED_JOBS", "MAX_TRAINING_ROWS",
        "MAX_INFERENCE_ROWS", "LOADER_CHUNK_ROWS", "WORKER_MEMORY_LIMIT_MB",
    ):
        monkeypatch.delenv(name, raising=False)

    for name in (
        "DB_ENCRYPTION_KEY", "WORKER_POSTGRES_USER", "WORKER_POSTGRES_PASSWORD",
        "DB_POOL_SIZE", "DB_MAX_OVERFLOW",
    ):
        monkeypatch.delenv(name, raising=False)

    from forecast.settings import DatabaseConfig, ForecastConfig

    database = DatabaseConfig()
    assert (database.pool_size, database.max_overflow) == (5, 5)

    config = ForecastConfig()
    assert config.triton_url == ""
    assert config.db_encryption_key.get_secret_value() == ""
    assert config.worker_postgres_user == ""
    assert config.worker_postgres_password.get_secret_value() == ""
    assert str(config.model_repo_path) == "/models"
    assert str(config.scratch_dir) == "/var/cache/forecast/scratch"
    assert str(config.data_dir) == "/data"
    assert config.triton_startup_wait_s == 120
    assert config.triton_load_timeout_s == 120
    assert config.triton_infer_timeout_s == 30
    assert config.forecaster_retention_count == 5
    assert config.default_time_limit_seconds == 1800
    assert config.training_max_attempts == 2
    assert config.training_retry_delay_seconds == 60
    assert config.max_queued_jobs == 10
    assert config.max_training_rows == 10_000_000
    assert config.max_inference_rows == 5000
    assert config.loader_chunk_rows == 100_000
    assert config.worker_memory_limit_mb is None


@pytest.mark.unit
def test_settings_read_the_environment(monkeypatch):
    monkeypatch.setenv("TRAINING_MAX_ATTEMPTS", "1")
    monkeypatch.setenv("MAX_QUEUED_JOBS", "3")
    monkeypatch.setenv("WORKER_MEMORY_LIMIT_MB", "4096")

    from forecast.settings import ForecastConfig

    config = ForecastConfig()
    assert config.training_max_attempts == 1
    assert config.max_queued_jobs == 3
    assert config.worker_memory_limit_mb == 4096


@pytest.mark.unit
def test_blank_memory_limit_means_unset(monkeypatch):
    monkeypatch.setenv("WORKER_MEMORY_LIMIT_MB", "  ")

    from forecast.settings import ForecastConfig

    config = ForecastConfig()
    assert config.worker_memory_limit_mb is None


@pytest.mark.unit
def test_secrets_are_not_printed(monkeypatch):
    monkeypatch.setenv("DB_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
    monkeypatch.setenv("WORKER_POSTGRES_PASSWORD", "hunter2")

    from forecast.settings import ForecastConfig

    config = ForecastConfig()
    assert "0123456789abcdef" not in repr(config)
    assert "hunter2" not in repr(config)
    assert config.db_encryption_key.get_secret_value() == "0123456789abcdef0123456789abcdef"


# ---------------------------------------------------------------------------
# Encryption key
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_cipher_requires_a_key():
    from forecast.utils.encryption import EncryptionKeyError, validate_cipher
    from forecast.settings import settings

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        with pytest.raises(EncryptionKeyError, match="not found"):
            validate_cipher()


@pytest.mark.unit
@pytest.mark.parametrize("key", ["short", "x" * 31, "x" * 33])
def test_cipher_rejects_a_key_that_is_not_32_bytes(key):
    from forecast.utils.encryption import EncryptionKeyError, validate_cipher
    from forecast.settings import settings

    with patch.object(settings.forecast, "db_encryption_key", SecretStr(key)):
        with pytest.raises(EncryptionKeyError, match="32 bytes"):
            validate_cipher()


@pytest.mark.unit
def test_cipher_round_trips_with_a_32_byte_key():
    from forecast.utils.encryption import validate_cipher
    from forecast.settings import settings

    with patch.object(
        settings.forecast, "db_encryption_key", SecretStr("0123456789abcdef0123456789abcdef")
    ):
        cipher = validate_cipher()
    nonce = b"0" * 12
    assert cipher.decrypt(nonce, cipher.encrypt(nonce, b"secret", None), None) == b"secret"


@pytest.mark.unit
def test_key_error_message_does_not_contain_the_key():
    from forecast.utils.encryption import EncryptionKeyError, validate_cipher
    from forecast.settings import settings

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("topsecretvalue")):
        with pytest.raises(EncryptionKeyError) as raised:
            validate_cipher()
    assert "topsecretvalue" not in str(raised.value)


@pytest.mark.unit
@pytest.mark.parametrize("key", ["", "x" * 31])
def test_startup_fails_fast_without_a_valid_key(key):
    from forecast.utils.encryption import EncryptionKeyError
    import forecast.app as forecast_app
    from forecast.settings import settings

    with patch.object(forecast_app, "close_db_connections"), patch.object(
        settings.forecast, "db_encryption_key", SecretStr(key)
    ):
        with pytest.raises(EncryptionKeyError):
            with TestClient(forecast_app.app):
                pass


@pytest.mark.unit
def test_startup_logs_no_error_with_a_valid_key(caplog):
    import forecast.app as forecast_app
    from forecast.settings import settings

    with patch.object(forecast_app, "close_db_connections"), patch.object(
        settings.forecast, "db_encryption_key", SecretStr("0123456789abcdef0123456789abcdef")
    ), caplog.at_level("ERROR"):
        with TestClient(forecast_app.app):
            pass
    assert not [record for record in caplog.records if record.levelno >= logging.ERROR]


@pytest.mark.unit
def test_close_db_connections_is_a_no_op_without_a_connection_manager():
    import forecast.db.connection as connection

    with patch.object(connection, "_close_db_connections", None):
        connection.close_db_connections()


@pytest.mark.unit
def test_shutdown_logs_when_closing_the_database_fails(caplog):
    import forecast.app as forecast_app

    with patch.object(
        forecast_app, "close_db_connections", side_effect=RuntimeError("boom")
    ), caplog.at_level("ERROR", logger="forecast_server"):
        logging.getLogger("forecast_server").propagate = True
        try:
            with TestClient(forecast_app.app):
                pass
        finally:
            logging.getLogger("forecast_server").propagate = False
    assert any("Error closing database connections" in r.getMessage() for r in caplog.records)


# ---------------------------------------------------------------------------
# Triton URL check
# ---------------------------------------------------------------------------


@pytest.mark.unit
@pytest.mark.parametrize(
    "url",
    ["http://triton:8000", "https://triton:8000/", "HTTP://triton:8000", "  http://triton:8000  "],
)
def test_triton_url_with_a_scheme_is_accepted(url):
    import forecast.triton_client.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", url):
        assert triton.triton_url_check() is None


@pytest.mark.unit
@pytest.mark.parametrize("url", ["triton:8000", "localhost:8000", "10.0.0.5:8000", "ftp://triton:8000"])
def test_triton_url_without_http_scheme_is_a_problem(url):
    import forecast.triton_client.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", url):
        with pytest.raises(RuntimeError, match="must start with http"):
            triton.triton_url_check()


@pytest.mark.unit
@pytest.mark.parametrize("url", ["", "   "])
def test_empty_triton_url_is_a_problem(url):
    import forecast.triton_client.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", url):
        with pytest.raises(RuntimeError, match="not set"):
            triton.triton_url_check()


@pytest.mark.unit
def test_triton_url_problem_does_not_echo_the_value():
    import forecast.triton_client.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", "user:hunter2@triton:8000"):
        with pytest.raises(RuntimeError) as raised:
            triton.triton_url_check()
    assert "hunter2" not in str(raised.value)


@pytest.mark.unit
def test_startup_fails_on_a_bad_triton_url_without_echoing_it():
    import forecast.app as forecast_app
    import forecast.triton_client.triton as triton
    from forecast.settings import settings

    # The real check runs here, so the autouse stub is replaced for this test only.
    with patch.object(forecast_app, "close_db_connections"), patch.object(
        forecast_app, "triton_url_check", triton.triton_url_check
    ), patch.object(settings.forecast, "triton_url", "triton:8000"):
        with pytest.raises(RuntimeError) as raised:
            with TestClient(forecast_app.app):
                pass
    assert "must start with http" in str(raised.value)
    assert "triton:8000" not in str(raised.value)


# ---------------------------------------------------------------------------
# Worker command line
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_worker_requires_a_job_id():
    from forecast.triton_client.worker import parse_args

    with pytest.raises(SystemExit):
        parse_args([])


@pytest.mark.unit
def test_worker_parses_the_job_id():
    from forecast.triton_client.worker import parse_args

    assert parse_args(["42"]).job_id == 42


@pytest.mark.unit
def test_worker_exits_non_zero_until_implemented(capsys):
    from forecast.triton_client.worker import main

    assert main(["1"]) == 1
    assert "not implemented" in capsys.readouterr().err
