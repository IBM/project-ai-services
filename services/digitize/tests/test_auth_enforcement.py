"""Digitize enforces catalog-issued admin tokens on every route (deny by default)."""

import pytest
from fastapi.routing import APIRoute
from fastapi.testclient import TestClient

import digitize.app as digitize_app
from common.auth import AuthConfigError, JWKSClient, TokenVerifier, set_verifier
from common.auth.config import AuthConfig
from common.tests.auth.helpers import CountingFetcher, Signer, jwks_body

pytestmark = pytest.mark.real_auth

PUBLIC = {("GET", "/health"), ("GET", "/openapi.json"), ("GET", "/ready")}


@pytest.fixture
def signer():
    return Signer()


@pytest.fixture
def client(signer):
    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="digitize")
    set_verifier(TokenVerifier(config, JWKSClient(config, CountingFetcher(jwks_body(signer)))))
    return TestClient(digitize_app.app, raise_server_exceptions=False)


def _walk(routes, prefix=""):
    """Yield (methods, path) for every API route, in both flattened and lazily-included layouts."""
    for r in routes:
        if isinstance(r, APIRoute):
            yield r.methods, prefix + r.path
        elif hasattr(r, "original_router"):  # newer FastAPI: included routers are kept as a tree
            yield from _walk(r.original_router.routes, prefix + r.include_context.prefix)


def _routes():
    out = []
    for methods, path in _walk(digitize_app.app.routes):
        for m in methods - {"HEAD", "OPTIONS"}:
            out.append((m, path))
    return out


def _fill(path):
    return path.replace("{", "x").replace("}", "")


def test_route_matrix_is_not_empty():
    assert len(_routes()) > 15


@pytest.mark.parametrize("method,path", _routes())
def test_every_route_requires_a_token(client, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    assert client.request(method, _fill(path)).status_code == 401


@pytest.mark.parametrize("method,path", _routes())
def test_every_route_requires_the_admin_role(client, signer, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    tok = signer.token(scope="digitize:read digitize:write")
    r = client.request(method, _fill(path), headers={"Authorization": f"Bearer {tok}"})
    assert r.status_code == 403


@pytest.mark.parametrize("method,path", _routes())
def test_token_for_another_service_is_refused_everywhere(client, signer, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    tok = signer.token(aud=["summarize"], scope="summarize:admin")
    r = client.request(method, _fill(path), headers={"Authorization": f"Bearer {tok}"})
    assert r.status_code == 401


def test_health_is_public(client):
    assert client.get("/health").status_code == 200


def test_ready_is_public_and_performs_jwks_check(client):
    r = client.get("/ready")
    assert r.status_code == 200
    assert r.json() == {"status": "ready"}


def test_swagger_root_needs_admin_and_builtin_docs_are_gone(client, signer):
    assert client.get("/").status_code == 401
    assert client.get("/docs").status_code == 401  # unknown paths are denied, not 404
    assert client.get("/redoc").status_code == 401
    ok = client.get("/", headers={"Authorization": f"Bearer {signer.token()}"})
    assert ok.status_code == 200


def test_unknown_path_is_denied_before_routing(client):
    assert client.get("/v2/anything").status_code == 401


def test_unauthenticated_multipart_upload_is_rejected(client):
    r = client.post("/v1/jobs", files={"files": ("a.pdf", b"x" * 1024)})
    assert r.status_code == 401


def test_expired_token_is_refused(client, signer):
    r = client.get("/v1/jobs", headers={"Authorization": f"Bearer {signer.token(exp=1)}"})
    assert r.status_code == 401


def test_startup_fails_closed_without_auth_config(monkeypatch):
    set_verifier(None)
    for k in ("AUTH_JWKS_URL", "AUTH_AUDIENCE"):
        monkeypatch.delenv(k, raising=False)
    from common.auth import get_verifier
    with pytest.raises(AuthConfigError):
        get_verifier()
