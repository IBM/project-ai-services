"""Similarity enforces catalog-issued tokens on every route (deny by default)."""

import sys
from pathlib import Path
from unittest.mock import patch

import pytest
from fastapi.routing import APIRoute
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

import similarity.app as similarity_app
from common.auth import AuthConfigError, JWKSClient, TokenVerifier, set_verifier
from common.auth.config import AuthConfig
from common.tests.auth.helpers import CountingFetcher, Signer, jwks_body

pytestmark = pytest.mark.real_auth

PUBLIC = {("GET", "/health"), ("GET", "/openapi.json"), ("GET", "/ready")}
SEARCH = ("POST", "/v1/similarity-search")
BODY = {"query": "q", "mode": "dense"}


class SimSigner(Signer):
    """A signer whose default tokens are for the similarity audience."""

    def claims(self, **over):
        base = {"aud": ["similarity"], "scope": "similarity:admin"}
        base.update(over)
        return super().claims(**base)


@pytest.fixture
def signer():
    return SimSigner()


@pytest.fixture
def client(signer):
    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="similarity")
    set_verifier(TokenVerifier(config, JWKSClient(config, CountingFetcher(jwks_body(signer)))))
    return TestClient(similarity_app.app, raise_server_exceptions=False)


def _routes():
    out = []
    for r in similarity_app.app.routes:
        if isinstance(r, APIRoute):
            for m in r.methods - {"HEAD", "OPTIONS"}:
                out.append((m, r.path))
    return out


def _bearer(token):
    return {"Authorization": f"Bearer {token}"}


def test_route_matrix_is_not_empty():
    routes = _routes()
    assert SEARCH in routes
    assert ("GET", "/") in routes
    assert ("GET", "/ready") in routes


@pytest.mark.parametrize("method,path", _routes())
def test_every_route_requires_a_token(client, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    assert client.request(method, path).status_code == 401


@pytest.mark.parametrize("method,path", _routes())
def test_token_for_another_service_is_refused_everywhere(client, signer, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    tok = signer.token(aud=["digitize"], scope="digitize:admin")
    assert client.request(method, path, headers=_bearer(tok)).status_code == 401


@pytest.mark.parametrize("method,path", _routes())
def test_chat_token_for_chat_audience_is_refused_everywhere(client, signer, method, path):
    """A token issued for another audience (chat) must not work, whatever its roles."""
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    tok = signer.token(aud=["chat"], scope="chat:admin")
    assert client.request(method, path, headers=_bearer(tok)).status_code == 401


def test_swagger_root_needs_admin(client, signer):
    assert client.get("/").status_code == 401
    for scope in ("similarity:read", "similarity:write"):
        r = client.get("/", headers=_bearer(signer.token(scope=scope)))
        assert r.status_code == 403, scope
    ok = client.get("/", headers=_bearer(signer.token(scope="similarity:admin")))
    assert ok.status_code == 200


def test_swagger_docs_are_public_and_redoc_is_gone(client, signer):
    assert client.get("/docs").status_code == 200  # public so users can browse the API
    assert client.post("/docs").status_code == 401  # only GET/HEAD are public
    assert client.get("/redoc").status_code == 401  # unknown paths are denied, not 404
    assert client.get("/redoc", headers=_bearer(signer.token())).status_code == 404


def test_search_needs_read_and_a_token_with_no_role_is_forbidden(client, signer):
    assert client.post(SEARCH[1], json=BODY).status_code == 401
    other_service_role = signer.token(scope="digitize:admin")
    assert client.post(SEARCH[1], json=BODY, headers=_bearer(other_service_role)).status_code == 403


@pytest.mark.parametrize("scope", ["similarity:read", "similarity:write", "similarity:admin"])
def test_search_is_allowed_for_read_and_above(client, signer, scope):
    """DA1: admin implies write implies read. The chat token carries exactly similarity:read."""
    docs = [{"page_content": "x", "filename": "f", "type": "text", "source": "f", "chunk_id": "1"}]
    perf = {"retrieve_time": 0.01}
    with patch.object(similarity_app, "emb_model_dict", {"emb_model": "m", "emb_endpoint": "http://e", "max_model_len": 512}), \
         patch.object(similarity_app, "_validate_query_length", return_value=(True, None)), \
         patch.object(similarity_app, "perform_similarity_search", return_value=(docs, [0.9], "cosine", perf)):
        r = client.post(SEARCH[1], json=BODY, headers=_bearer(signer.token(sub="chat", typ="service", idp="internal", scope=scope)))
    assert r.status_code == 200, r.text
    assert r.json()["results"][0]["page_content"] == "x"


def test_search_body_is_not_validated_before_authentication(client):
    """An invalid body gets 401, not 422/400: the middleware runs before the route."""
    assert client.post(SEARCH[1], json={"mode": "bogus"}).status_code == 401
    assert client.post(SEARCH[1], content=b"not json").status_code == 401


def test_health_is_public(client):
    assert client.get("/health").status_code == 200


def test_ready_is_public_and_performs_jwks_check(client):
    r = client.get("/ready")
    assert r.status_code == 200
    assert r.json() == {"status": "ready"}


def test_ready_is_503_without_a_leak_when_jwks_is_unavailable(signer):
    from common.auth import JWKSUnavailable

    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="similarity")
    failing = CountingFetcher(exc=JWKSUnavailable("secret-host.internal refused"))
    set_verifier(TokenVerifier(config, JWKSClient(config, failing)))
    r = TestClient(similarity_app.app, raise_server_exceptions=False).get("/ready")
    assert r.status_code == 503
    assert "secret-host" not in r.text


def test_unknown_path_is_denied_before_routing(client):
    assert client.get("/v2/anything").status_code == 401


def test_expired_token_is_refused(client, signer):
    r = client.post(SEARCH[1], json=BODY, headers=_bearer(signer.token(exp=1)))
    assert r.status_code == 401


# ---- error sweep ----

def _search(client, signer, **patches):
    emb = {"emb_model": "m", "emb_endpoint": "http://e", "max_model_len": 512}
    validate = patches.get("validate", (True, None))
    with patch.object(similarity_app, "emb_model_dict", emb), \
         patch.object(similarity_app, "_validate_query_length", return_value=validate), \
         patch.object(similarity_app, "perform_similarity_search", side_effect=patches.get("search_exc")):
        return client.post(SEARCH[1], json=BODY, headers=_bearer(signer.token(scope="similarity:read")))


def test_internal_error_does_not_leak_exception_text(client, signer):
    r = _search(client, signer, search_exc=RuntimeError("password=hunter2 at 10.0.0.5:9200"))
    assert r.status_code == 500
    assert "hunter2" not in r.text
    assert "10.0.0.5" not in r.text
    assert "RuntimeError" not in r.text
    assert r.json()["error"]["code"] == "INTERNAL_SERVER_ERROR"


def test_query_too_long_is_still_a_400_not_a_500(client, signer):
    """The broad except must not turn a deliberate 400 into a 500."""
    r = _search(client, signer, validate=(False, "query exceeds the limit"), search_exc=RuntimeError("unused"))
    assert r.status_code == 400
    assert "query exceeds the limit" in r.text


# ---- startup ----

def test_startup_fails_closed_without_auth_config(monkeypatch):
    set_verifier(None)
    for k in ("AUTH_JWKS_URL", "AUTH_AUDIENCE"):
        monkeypatch.delenv(k, raising=False)
    with pytest.raises(AuthConfigError):
        with TestClient(similarity_app.app):
            pass


def test_no_error_text_is_formatted_into_client_responses():
    """Static guard for the error sweep: no repr/str/exception formatting in raise_error calls."""
    import re

    src = (Path(similarity_app.__file__)).read_text()
    for call in re.findall(r"APIError\.raise_error\([^)]*\)", src):
        assert not re.search(r"repr\(|str\(|\{e\}|\{exc\}|\{err\}", call), call
