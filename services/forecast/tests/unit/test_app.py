"""
Unit tests for the Forecast service skeleton.

Tests cover:
 - GET /health: PostgreSQL and Triton reachability
 - Middleware: X-Request-ID injection
 - Error envelope for unknown paths, wrong methods, validation errors and
   unexpected exceptions
 - TRITON_UNAVAILABLE
 - Settings defaults and the blank-means-unset rule
 - The worker command line
"""

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


@pytest.fixture
def client():
    """TestClient for the real app, with startup probes and health probes patched."""
    import forecast.app as forecast_app

    with patch.object(forecast_app, "check_db_reachable", return_value=True), patch.object(
        forecast_app, "is_triton_ready", return_value=True
    ), patch.object(forecast_app, "close_db_connections"):
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
    assert response.json() == {"status": "ok", "checks": {"postgres": "ok", "triton": "ok"}}


@pytest.mark.unit
def test_health_triton_unreachable_is_still_200():
    import forecast.app as forecast_app

    with patch.object(forecast_app, "check_db_reachable", return_value=True), patch.object(
        forecast_app, "is_triton_ready", return_value=False
    ), patch.object(forecast_app, "close_db_connections"):
        with TestClient(forecast_app.app) as test_client:
            response = test_client.get("/health")
    assert response.status_code == 200
    assert response.json() == {
        "status": "ok",
        "checks": {"postgres": "ok", "triton": "unreachable"},
    }


@pytest.mark.unit
def test_health_postgres_down_is_503():
    import forecast.app as forecast_app

    with patch.object(forecast_app, "check_db_reachable", return_value=False), patch.object(
        forecast_app, "is_triton_ready", return_value=True
    ), patch.object(forecast_app, "close_db_connections"):
        with TestClient(forecast_app.app) as test_client:
            response = test_client.get("/health")
    assert response.status_code == 503
    assert response.json() == {
        "status": "unavailable",
        "checks": {"postgres": "error", "triton": "ok"},
    }


@pytest.mark.unit
def test_service_starts_when_postgres_and_triton_are_down():
    """Startup must not fail: /health is the place that reports both."""
    import forecast.app as forecast_app

    with patch.object(forecast_app, "check_db_reachable", return_value=False), patch.object(
        forecast_app, "is_triton_ready", return_value=False
    ), patch.object(forecast_app, "close_db_connections"):
        with TestClient(forecast_app.app) as test_client:
            assert test_client.get("/health").status_code == 503


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
    from forecast.encryption import EncryptionKeyError, get_cipher
    from forecast.settings import settings

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        with pytest.raises(EncryptionKeyError, match="not found"):
            get_cipher()


@pytest.mark.unit
@pytest.mark.parametrize("key", ["short", "x" * 31, "x" * 33])
def test_cipher_rejects_a_key_that_is_not_32_bytes(key):
    from forecast.encryption import EncryptionKeyError, get_cipher
    from forecast.settings import settings

    with patch.object(settings.forecast, "db_encryption_key", SecretStr(key)):
        with pytest.raises(EncryptionKeyError, match="32 bytes"):
            get_cipher()


@pytest.mark.unit
def test_cipher_round_trips_with_a_32_byte_key():
    from forecast.encryption import get_cipher
    from forecast.settings import settings

    with patch.object(
        settings.forecast, "db_encryption_key", SecretStr("0123456789abcdef0123456789abcdef")
    ):
        cipher = get_cipher()
    nonce = b"0" * 12
    assert cipher.decrypt(nonce, cipher.encrypt(nonce, b"secret", None), None) == b"secret"


@pytest.mark.unit
def test_key_error_message_does_not_contain_the_key():
    from forecast.encryption import EncryptionKeyError, get_cipher
    from forecast.settings import settings

    with patch.object(settings.forecast, "db_encryption_key", SecretStr("topsecretvalue")):
        with pytest.raises(EncryptionKeyError) as raised:
            get_cipher()
    assert "topsecretvalue" not in str(raised.value)


@pytest.mark.unit
def test_startup_logs_an_error_without_a_key_and_still_starts(caplog):
    import forecast.app as forecast_app
    from forecast.settings import settings

    with patch.object(forecast_app, "check_db_reachable", return_value=True), patch.object(
        forecast_app, "is_triton_ready", return_value=True
    ), patch.object(forecast_app, "close_db_connections"), patch.object(
        settings.forecast, "db_encryption_key", SecretStr("")
    ), caplog.at_level("ERROR"):
        with TestClient(forecast_app.app) as test_client:
            assert test_client.get("/health").status_code == 200
    assert any("DB_ENCRYPTION_KEY" in record.getMessage() for record in caplog.records)


@pytest.mark.unit
def test_startup_logs_no_error_with_a_valid_key(caplog):
    import forecast.app as forecast_app
    from forecast.settings import settings

    with patch.object(forecast_app, "check_db_reachable", return_value=True), patch.object(
        forecast_app, "is_triton_ready", return_value=True
    ), patch.object(forecast_app, "close_db_connections"), patch.object(
        settings.forecast, "db_encryption_key", SecretStr("0123456789abcdef0123456789abcdef")
    ), caplog.at_level("ERROR"):
        with TestClient(forecast_app.app):
            pass
    assert not any("DB_ENCRYPTION_KEY" in record.getMessage() for record in caplog.records)


@pytest.mark.unit
def test_shutdown_logs_when_closing_the_database_fails(caplog):
    import forecast.app as forecast_app

    with patch.object(forecast_app, "check_db_reachable", return_value=True), patch.object(
        forecast_app, "is_triton_ready", return_value=True
    ), patch.object(
        forecast_app, "close_db_connections", side_effect=RuntimeError("boom")
    ), caplog.at_level("ERROR"):
        with TestClient(forecast_app.app):
            pass
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
    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", url):
        assert triton.triton_url_problem() is None


@pytest.mark.unit
@pytest.mark.parametrize("url", ["triton:8000", "localhost:8000", "10.0.0.5:8000", "ftp://triton:8000"])
def test_triton_url_without_http_scheme_is_a_problem(url):
    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", url):
        problem = triton.triton_url_problem()
    assert problem and "http://" in problem


@pytest.mark.unit
@pytest.mark.parametrize("url", ["", "   "])
def test_empty_triton_url_is_a_problem(url):
    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", url):
        assert "not set" in triton.triton_url_problem()


@pytest.mark.unit
def test_triton_url_problem_does_not_echo_the_value():
    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", "user:hunter2@triton:8000"):
        assert "hunter2" not in triton.triton_url_problem()


def _start_app_with_triton_url(url, caplog):
    import forecast.app as forecast_app
    from forecast.settings import settings

    with patch.object(forecast_app, "check_db_reachable", return_value=True), patch.object(
        forecast_app, "close_db_connections"
    ), patch.object(settings.forecast, "triton_url", url), patch.object(
        settings.forecast, "db_encryption_key", SecretStr("0123456789abcdef0123456789abcdef")
    ), caplog.at_level("WARNING"):
        with TestClient(forecast_app.app) as test_client:
            response = test_client.get("/health")
    return response


@pytest.mark.unit
def test_startup_reports_a_bad_triton_url_as_a_configuration_error(caplog):
    response = _start_app_with_triton_url("triton:8000", caplog)

    errors = [r for r in caplog.records if r.levelname == "ERROR" and "TRITON_URL" in r.getMessage()]
    assert len(errors) == 1
    assert "triton:8000" not in errors[0].getMessage()
    assert not any("not reachable" in r.getMessage() and "Triton" in r.getMessage() for r in caplog.records)
    # /health keeps its documented values and the service stays up.
    assert response.status_code == 200
    assert response.json()["checks"]["triton"] == "unreachable"


@pytest.mark.unit
def test_startup_warns_when_a_valid_triton_url_is_not_reachable(caplog):
    import requests

    with patch("forecast.triton.requests.get", side_effect=requests.ConnectionError("down")):
        _start_app_with_triton_url("http://triton:8000", caplog)

    assert not any("TRITON_URL" in r.getMessage() for r in caplog.records)
    assert any(r.levelname == "WARNING" and "Triton is not reachable" in r.getMessage() for r in caplog.records)


# ---------------------------------------------------------------------------
# Triton probe
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_triton_probe_false_without_url():
    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", ""):
        assert triton.is_triton_ready() is False


@pytest.mark.unit
def test_triton_probe_true_on_200():
    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", "http://triton:8000/"), patch(
        "forecast.triton.requests.get"
    ) as get:
        get.return_value.status_code = 200
        assert triton.is_triton_ready() is True
        get.assert_called_once_with("http://triton:8000/v2/health/ready", timeout=triton.HEALTH_TIMEOUT_S)


@pytest.mark.unit
def test_triton_probe_false_on_non_200():
    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", "http://triton:8000"), patch(
        "forecast.triton.requests.get"
    ) as get:
        get.return_value.status_code = 503
        assert triton.is_triton_ready() is False


@pytest.mark.unit
def test_triton_probe_false_on_connection_error():
    import requests

    import forecast.triton as triton

    with patch.object(triton.settings.forecast, "triton_url", "http://triton:8000"), patch(
        "forecast.triton.requests.get", side_effect=requests.ConnectionError("refused")
    ):
        assert triton.is_triton_ready() is False


@pytest.mark.unit
def test_triton_probe_failure_logs_the_class_but_not_the_url():
    import requests

    import forecast.triton as triton

    error = requests.ConnectionError("refused http://user:secret@triton:8000")
    with patch.object(triton.settings.forecast, "triton_url", "http://triton:8000"), patch(
        "forecast.triton.requests.get", side_effect=error
    ), patch.object(triton.logger, "debug") as debug:
        assert triton.is_triton_ready() is False
    message = debug.call_args.args[0]
    assert "ConnectionError" in message
    assert "secret" not in message


# ---------------------------------------------------------------------------
# Database reachability
# ---------------------------------------------------------------------------


@pytest.fixture(autouse=True)
def _no_probe_left_in_flight():
    """Start every test without a database probe left over from an earlier one."""
    import forecast.db.connection as connection

    connection._probe_in_flight = None
    yield
    connection._probe_in_flight = None


@pytest.mark.unit
def test_db_reachable_false_without_connection_settings():
    import forecast.db.connection as connection

    with patch.object(connection, "_check_db_connection", None):
        assert connection.check_db_reachable() is False


@pytest.mark.unit
def test_db_reachable_follows_the_checker():
    import forecast.db.connection as connection

    with patch.object(connection, "_check_db_connection", lambda: True):
        assert connection.check_db_reachable() is True
    with patch.object(connection, "_check_db_connection", lambda: False):
        assert connection.check_db_reachable() is False


@pytest.mark.unit
def test_db_probe_gives_up_when_the_database_does_not_answer():
    import threading
    import time

    import forecast.db.connection as connection

    release = threading.Event()
    calls = []

    def hanging_checker():
        calls.append(1)
        release.wait(5)
        return True

    try:
        with patch.object(connection, "_check_db_connection", hanging_checker):
            started = time.monotonic()
            assert connection.check_db_reachable(timeout=0.1) is False
            assert time.monotonic() - started < 2

            # A second call waits on the probe that is still running.
            assert connection.check_db_reachable(timeout=0.1) is False
            assert len(calls) == 1
    finally:
        release.set()


@pytest.mark.unit
def test_db_probe_recovers_after_a_hung_probe_finishes():
    import threading

    import forecast.db.connection as connection

    release = threading.Event()

    def slow_then_fast():
        release.wait(5)
        return True

    with patch.object(connection, "_check_db_connection", slow_then_fast):
        assert connection.check_db_reachable(timeout=0.05) is False
        release.set()
        assert connection.check_db_reachable(timeout=2) is True
        assert connection.check_db_reachable(timeout=2) is True


@pytest.mark.unit
def test_db_probe_false_when_the_checker_raises():
    import forecast.db.connection as connection

    def broken():
        raise RuntimeError("boom")

    with patch.object(connection, "_check_db_connection", broken):
        assert connection.check_db_reachable() is False


@pytest.mark.unit
def test_db_probe_failure_logs_the_class_but_not_the_message():
    import forecast.db.connection as connection

    def broken():
        raise RuntimeError("password=secret")

    with patch.object(connection, "_check_db_connection", broken), patch.object(
        connection.logger, "warning"
    ) as warning:
        assert connection.check_db_reachable() is False
    message = warning.call_args.args[0]
    assert "RuntimeError" in message
    assert "secret" not in message


@pytest.mark.unit
def test_db_probe_timeout_is_logged():
    import threading

    import forecast.db.connection as connection

    release = threading.Event()

    def hanging_checker():
        release.wait(5)
        return True

    try:
        with patch.object(connection, "_check_db_connection", hanging_checker), patch.object(
            connection.logger, "warning"
        ) as warning:
            assert connection.check_db_reachable(timeout=0.05) is False
        assert "did not answer" in warning.call_args.args[0]
    finally:
        release.set()


@pytest.mark.unit
def test_close_db_connections_is_safe_without_engine():
    import forecast.db.connection as connection

    with patch.object(connection, "_close_db_connections", None):
        connection.close_db_connections()


@pytest.mark.unit
def test_close_db_connections_calls_the_closer():
    import forecast.db.connection as connection

    with patch.object(connection, "_close_db_connections") as closer:
        connection.close_db_connections()
    closer.assert_called_once_with()


# ---------------------------------------------------------------------------
# Worker command line
# ---------------------------------------------------------------------------


@pytest.mark.unit
def test_worker_requires_a_job_id():
    from forecast.worker import parse_args

    with pytest.raises(SystemExit):
        parse_args([])


@pytest.mark.unit
def test_worker_parses_the_job_id():
    from forecast.worker import parse_args

    assert parse_args(["0f8fad5b-d9cb-469f-a165-70867728950e"]).job_id == "0f8fad5b-d9cb-469f-a165-70867728950e"


@pytest.mark.unit
def test_worker_exits_non_zero_until_implemented(capsys):
    from forecast.worker import main

    assert main(["job-1"]) == 1
    assert "not implemented" in capsys.readouterr().err
