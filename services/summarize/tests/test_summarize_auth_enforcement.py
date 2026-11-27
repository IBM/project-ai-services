"""Summarize enforces catalog-issued tokens on every route (deny by default)."""

import asyncio
import re
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from fastapi.routing import APIRoute
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

import summarize.app as summarize_app
from common.auth import AuthConfigError, JWKSClient, TokenVerifier, set_verifier
from common.auth.config import AuthConfig
from common.tests.auth.helpers import CountingFetcher, Signer, jwks_body

pytestmark = pytest.mark.real_auth

PUBLIC = {("GET", "/health"), ("GET", "/openapi.json"), ("GET", "/ready")}

# Plan section 7. This is the single source of truth for the tests below.
ROLE = {
    ("GET", "/"): "admin",
    ("POST", "/v1/summarize"): "write",
    ("POST", "/v1/summarize/jobs"): "write",
    ("DELETE", "/v1/summarize/jobs/{job_id}"): "write",
    ("GET", "/v1/summarize/jobs"): "read",
    ("GET", "/v1/summarize/jobs/{job_id}"): "read",
    ("GET", "/v1/summarize/jobs/{job_id}/result"): "read",
    ("DELETE", "/v1/summarize/jobs"): "admin",
}
RANK = {"read": 1, "write": 2, "admin": 3}
BULK_DELETE = ("DELETE", "/v1/summarize/jobs")
WRITE_ROUTES = [r for r, role in ROLE.items() if role == "write"]
READ_ROUTES = [r for r, role in ROLE.items() if role == "read"]


class SummarizeSigner(Signer):
    """A signer whose default tokens are for the summarize audience."""

    def claims(self, **over):
        base = {"aud": ["summarize"], "scope": "summarize:admin"}
        base.update(over)
        return super().claims(**base)


@pytest.fixture
def signer():
    return SummarizeSigner()


@pytest.fixture(autouse=True)
def no_database():
    """Handlers that are allowed to run must not reach a real database."""
    repo = MagicMock()
    repo.get_all_jobs.return_value = ([], 0)
    repo.get_job_by_id.return_value = None
    repo.get_active_jobs.return_value = []
    repo.delete_all_jobs.return_value = True
    with patch("summarize.db.manager.db_repo", repo):
        yield repo


@pytest.fixture
def client(signer):
    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="summarize")
    set_verifier(TokenVerifier(config, JWKSClient(config, CountingFetcher(jwks_body(signer)))))
    return TestClient(summarize_app.app, raise_server_exceptions=False)


def _routes():
    out = []
    for r in summarize_app.app.routes:
        if isinstance(r, APIRoute):
            for m in r.methods - {"HEAD", "OPTIONS"}:
                out.append((m, r.path))
    return out


def _concrete(path):
    return path.replace("{job_id}", "job-1")


def _bearer(token):
    return {"Authorization": f"Bearer {token}"}


def _call(client, method, path, token=None):
    headers = _bearer(token) if token else {}
    return client.request(method, _concrete(path), headers=headers)


# ---- the matrix covers the real route list ----

def test_matrix_covers_every_route_and_nothing_else():
    """A route added without a decision here fails the build (deny by default).

    /openapi.json is a plain Starlette route, not an APIRoute, so it is not listed by _routes();
    test_openapi_json_is_public covers it.
    """
    assert set(_routes()) == set(ROLE) | (PUBLIC - {("GET", "/openapi.json")})


def test_every_protected_route_declares_a_role_dependency():
    for r in summarize_app.app.routes:
        if not isinstance(r, APIRoute):
            continue
        for m in r.methods - {"HEAD", "OPTIONS"}:
            if (m, r.path) in PUBLIC:
                assert not r.dependencies, (m, r.path)
            else:
                assert r.dependencies, f"{m} {r.path} has no require_role dependency"


# ---- authentication ----

@pytest.mark.parametrize("method,path", _routes())
def test_every_route_requires_a_token(client, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    assert _call(client, method, path).status_code == 401


@pytest.mark.parametrize("method,path", _routes())
@pytest.mark.parametrize("other", ["digitize", "similarity", "chat"])
def test_token_for_another_service_is_refused_everywhere(client, signer, method, path, other):
    """A token issued for another audience must not work, whatever its roles."""
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    tok = signer.token(aud=[other], scope=f"{other}:admin")
    assert _call(client, method, path, tok).status_code == 401


def test_expired_token_is_refused(client, signer):
    assert _call(client, "GET", "/v1/summarize/jobs", signer.token(exp=1)).status_code == 401


def test_unknown_path_is_denied_before_routing(client):
    assert client.get("/v2/anything").status_code == 401


def test_swagger_docs_are_public_and_redoc_is_gone(client, signer):
    assert client.get("/docs").status_code == 200  # public so users can browse the API
    assert client.post("/docs").status_code == 401  # only GET/HEAD are public
    assert client.get("/redoc").status_code == 401  # unknown paths are denied, not 404
    assert client.get("/redoc", headers=_bearer(signer.token())).status_code == 404


def test_bodies_are_not_validated_before_authentication(client):
    """An invalid body or upload gets 401, not 4xx from the handler: the middleware runs first."""
    assert client.post("/v1/summarize", content=b"not json", headers={"Content-Type": "application/json"}).status_code == 401
    assert client.post("/v1/summarize/jobs", files={"file": ("a.txt", b"x")}).status_code == 401


# ---- authorization ----

def _allowed(role_needed, scope_role):
    return RANK[scope_role] >= RANK[role_needed]


@pytest.mark.parametrize("method,path", sorted(ROLE))
@pytest.mark.parametrize("held", ["read", "write", "admin"])
def test_role_hierarchy_per_route(client, signer, method, path, held):
    """DA1: admin implies write implies read. Too low a role is 403, enough is not."""
    r = _call(client, method, path, signer.token(scope=f"summarize:{held}"))
    if _allowed(ROLE[(method, path)], held):
        assert r.status_code not in (401, 403), (method, path, held, r.status_code)
    else:
        assert r.status_code == 403, (method, path, held, r.status_code)


@pytest.mark.parametrize("method,path", sorted(ROLE))
def test_token_with_no_summarize_role_is_forbidden_everywhere(client, signer, method, path):
    tok = signer.token(scope="digitize:admin")
    assert _call(client, method, path, tok).status_code == 403


@pytest.mark.parametrize("method,path", WRITE_ROUTES)
def test_read_token_cannot_call_a_write_route(client, signer, method, path):
    assert _call(client, method, path, signer.token(scope="summarize:read")).status_code == 403


def test_write_token_cannot_call_bulk_delete(client, signer, no_database):
    r = client.delete("/v1/summarize/jobs?confirm=true", headers=_bearer(signer.token(scope="summarize:write")))
    assert r.status_code == 403
    no_database.delete_all_jobs.assert_not_called()


def test_admin_token_can_call_bulk_delete(client, signer, no_database):
    r = client.delete("/v1/summarize/jobs?confirm=true", headers=_bearer(signer.token(scope="summarize:admin")))
    assert r.status_code == 204
    no_database.delete_all_jobs.assert_called_once()


@pytest.mark.parametrize("method,path", READ_ROUTES)
def test_read_token_can_call_read_routes(client, signer, method, path):
    r = _call(client, method, path, signer.token(scope="summarize:read"))
    assert r.status_code not in (401, 403)


def test_swagger_root_needs_admin(client, signer):
    assert client.get("/").status_code == 401
    for scope in ("summarize:read", "summarize:write"):
        assert client.get("/", headers=_bearer(signer.token(scope=scope))).status_code == 403, scope
    assert client.get("/", headers=_bearer(signer.token(scope="summarize:admin"))).status_code == 200


# ---- public routes ----

def test_health_is_public(client):
    assert client.get("/health").status_code == 200


def test_openapi_json_is_public(client):
    assert client.get("/openapi.json").status_code == 200


def test_public_routes_are_get_only(client):
    for path in ("/health", "/ready", "/openapi.json"):
        assert client.post(path).status_code == 401, path


def test_ready_is_public_and_performs_jwks_check(client):
    r = client.get("/ready")
    assert r.status_code == 200
    assert r.json() == {"status": "ready"}


def test_ready_is_503_without_a_leak_when_jwks_is_unavailable(signer):
    from common.auth import JWKSUnavailable

    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="summarize")
    failing = CountingFetcher(exc=JWKSUnavailable("secret-host.internal refused"))
    set_verifier(TokenVerifier(config, JWKSClient(config, failing)))
    r = TestClient(summarize_app.app, raise_server_exceptions=False).get("/ready")
    assert r.status_code == 503
    assert "secret-host" not in r.text


# ---- startup ----

@pytest.mark.parametrize("missing", [("AUTH_JWKS_URL",), ("AUTH_AUDIENCE",), ("AUTH_JWKS_URL", "AUTH_AUDIENCE")])
def test_startup_fails_closed_without_auth_config(monkeypatch, missing):
    set_verifier(None)
    monkeypatch.setenv("AUTH_JWKS_URL", "https://catalog.invalid/jwks")
    monkeypatch.setenv("AUTH_AUDIENCE", "summarize")
    for k in missing:
        monkeypatch.delenv(k, raising=False)
    with pytest.raises(AuthConfigError):
        with TestClient(summarize_app.app):
            pass


def test_misconfigured_pod_fails_before_touching_the_database_or_workers(monkeypatch):
    """The auth check is the first thing the lifespan does: no LLM session, DB or zombie scan."""
    set_verifier(None)
    for k in ("AUTH_JWKS_URL", "AUTH_AUDIENCE"):
        monkeypatch.delenv(k, raising=False)
    with patch.object(summarize_app, "check_db_connection") as db_check, \
         patch.object(summarize_app, "create_llm_session") as llm_session, \
         patch.object(summarize_app, "initialize_models") as models, \
         patch.object(summarize_app, "ensure_directories") as dirs, \
         patch("summarize.job_utils.recover_zombie_jobs") as zombies:
        with pytest.raises(AuthConfigError):
            with TestClient(summarize_app.app):
                pass
    db_check.assert_not_called()
    llm_session.assert_not_called()
    models.assert_not_called()
    dirs.assert_not_called()
    zombies.assert_not_called()


# ---- error sweep ----

def test_no_error_text_is_formatted_into_client_responses():
    """Static guard: no repr/str/exception formatting in raise_error calls."""
    src = Path(summarize_app.__file__).read_text()
    for call in re.findall(r"APIError\.raise_error\([^)]*\)", src):
        assert not re.search(r"repr\(|str\(|\{e\}|\{exc\}|\{err\}", call), call


def _job_run(tmp_path, tokenize_exc=None, text="word " * 50):
    """Run process_summarization_job against a staged .txt file and return the stored error."""
    job_id = "11111111-2222-3333-4444-555555555555"
    (tmp_path / job_id).mkdir()
    (tmp_path / job_id / "doc.txt").write_text(text)

    settings = MagicMock()
    settings.summarize.staging_dir = tmp_path
    settings.summarize.results_dir = tmp_path
    repo = MagicMock()
    with patch("summarize.app.settings", settings), \
         patch("summarize.app.llm_model_dict", {"llm_endpoint": "http://e", "llm_model": "m"}, create=True), \
         patch("summarize.db.manager.db_repo", repo), \
         patch("summarize.app.tokenize_with_llm", side_effect=tokenize_exc), \
         patch("summarize.app.cleanup_staging_directory"):
        asyncio.run(summarize_app.process_summarization_job(job_id, "standard"))
    errors = [c.kwargs["error"] for c in repo.update_job.call_args_list if c.kwargs.get("error")]
    assert len(errors) == 1
    return errors[0]


def test_failed_job_does_not_store_exception_text(tmp_path):
    """The stored error is returned by the job list, job details and the 422 on /result."""
    err = _job_run(tmp_path, tokenize_exc=RuntimeError("password=hunter2 at 10.0.0.5:8000 /var/cache/x"))
    assert err == summarize_app.GENERIC_JOB_ERROR
    for leaked in ("hunter2", "10.0.0.5", "RuntimeError", "/var/cache"):
        assert leaked not in err


def test_failed_job_keeps_the_deliberate_message_for_empty_input(tmp_path):
    """The E2E suite and users rely on this message."""
    assert _job_run(tmp_path, text="   \n") == "Extracted text is empty"


def test_failed_job_does_not_expose_the_staging_path(tmp_path):
    job_id = "11111111-2222-3333-4444-555555555555"  # staging dir is never created
    settings = MagicMock()
    settings.summarize.staging_dir = tmp_path
    settings.summarize.results_dir = tmp_path
    repo = MagicMock()
    with patch("summarize.app.settings", settings), \
         patch("summarize.db.manager.db_repo", repo), \
         patch("summarize.app.cleanup_staging_directory"):
        asyncio.run(summarize_app.process_summarization_job(job_id, "standard"))
    err = [c.kwargs["error"] for c in repo.update_job.call_args_list if c.kwargs.get("error")][0]
    assert str(tmp_path) not in err
    assert err == "Staged input file not found"


def test_job_llm_error_is_not_copied_into_the_stored_error(tmp_path):
    job_id = "11111111-2222-3333-4444-555555555555"
    (tmp_path / job_id).mkdir()
    (tmp_path / job_id / "doc.txt").write_text("word " * 50)
    settings = MagicMock()
    settings.summarize.staging_dir = tmp_path
    settings.summarize.results_dir = tmp_path
    settings.summarize.summarization_prompt_token_count = 10
    settings.summarize.summarization_temperature = 0.0
    repo = MagicMock()
    with patch("summarize.app.settings", settings), \
         patch("summarize.app.llm_model_dict", {"llm_endpoint": "http://e", "llm_model": "m"}, create=True), \
         patch("summarize.db.manager.db_repo", repo), \
         patch("summarize.app.tokenize_with_llm", return_value=list(range(10))), \
         patch("summarize.app.get_llm_max_model_len", return_value=1000), \
         patch("summarize.app.compute_target_and_max_tokens", return_value=(5, 4, 6, 10)), \
         patch("summarize.app.query_vllm_summarize", return_value=({"error": "http://10.1.2.3:8000 token=abc"}, 1, 0)), \
         patch("summarize.app.cleanup_staging_directory"):
        asyncio.run(summarize_app.process_summarization_job(job_id, "standard"))
    err = [c.kwargs["error"] for c in repo.update_job.call_args_list if c.kwargs.get("error")][0]
    assert err == "LLM error"
    assert "10.1.2.3" not in err and "abc" not in err


def test_result_of_a_failed_job_returns_the_stored_text_only(client, signer, no_database):
    job = MagicMock()
    job.status = "failed"
    job.error = summarize_app.GENERIC_JOB_ERROR
    no_database.get_job_by_id.return_value = job
    r = client.get("/v1/summarize/jobs/job-1/result", headers=_bearer(signer.token(scope="summarize:read")))
    assert r.status_code == 422
    assert r.json()["error"]["code"] == "JOB_FAILED"
    assert summarize_app.GENERIC_JOB_ERROR in r.json()["error"]["message"]


def test_stream_error_does_not_leak_upstream_detail():
    """query_vllm_summarize_stream is streamed to the caller as is."""
    import requests
    from common import misc_utils
    from common.llm_utils import query_vllm_summarize_stream

    resp = MagicMock()
    resp.text = "internal vllm trace at 10.9.9.9 api_key=sk-secret"
    session = MagicMock()
    session.post.side_effect = requests.exceptions.HTTPError("boom http://10.9.9.9:8000", response=resp)
    with patch.object(misc_utils, "SESSION", session):
        out = "".join(query_vllm_summarize_stream("http://e", [], "m", 10, 0.0))
    assert "[DONE]" in out
    for leaked in ("10.9.9.9", "sk-secret", "boom", "trace"):
        assert leaked not in out
    assert "Failed to generate summary" in out
