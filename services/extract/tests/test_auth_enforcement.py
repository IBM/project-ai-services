"""Extract enforces catalog-issued tokens on every route (deny by default)."""

import sys
from pathlib import Path

import pytest
from fastapi.routing import APIRoute
from fastapi.testclient import TestClient

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

import extract.app as service_app
from common.auth import AuthConfigError, JWKSClient, JWKSUnavailable, TokenVerifier, set_verifier
from common.auth.config import AuthConfig
from common.tests.auth.helpers import CountingFetcher, Signer, jwks_body

pytestmark = pytest.mark.real_auth

SERVICE = "extract"
PUBLIC = {("GET", "/health"), ("GET", "/openapi.json"), ("GET", "/ready")}
RANK = {"read": 1, "write": 2, "admin": 3}

# This is the single source of truth for the tests below. A route added without a decision
# here fails test_matrix_covers_every_route_and_nothing_else (deny by default).
ROLE = {
    ("GET", "/"): "admin",
    ("POST", "/v1/schemas"): "write",
    ("GET", "/v1/schemas"): "read",
    ("GET", "/v1/schemas/{schema_id}"): "read",
    ("DELETE", "/v1/schemas/{schema_id}"): "write",
    ("DELETE", "/v1/schemas"): "admin",
    ("POST", "/v1/extract"): "write",
    ("POST", "/v1/extract/jobs"): "write",
    ("GET", "/v1/extract/jobs"): "read",
    ("GET", "/v1/extract/jobs/{job_id}"): "read",
    ("GET", "/v1/extract/jobs/{job_id}/results/{doc_id}"): "read",
    ("GET", "/v1/extract/jobs/{job_id}/results/{doc_id}/download"): "read",
    ("DELETE", "/v1/extract/jobs/{job_id}"): "write",
    ("DELETE", "/v1/extract/jobs"): "admin",
}


class ServiceSigner(Signer):
    """A signer whose default tokens are for this service's audience."""

    def claims(self, **over):
        base = {"aud": [SERVICE], "scope": f"{SERVICE}:admin"}
        base.update(over)
        return super().claims(**base)


@pytest.fixture
def signer():
    return ServiceSigner()


@pytest.fixture
def client(signer):
    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience=SERVICE)
    set_verifier(TokenVerifier(config, JWKSClient(config, CountingFetcher(jwks_body(signer)))))
    return TestClient(service_app.app, raise_server_exceptions=False)


def _walk(routes, prefix=""):
    """Yield (methods, path) for every API route, in both flattened and lazily-included layouts."""
    for r in routes:
        if isinstance(r, APIRoute):
            yield r.methods, prefix + r.path
        elif hasattr(r, "original_router"):  # newer FastAPI: included routers are kept as a tree
            yield from _walk(r.original_router.routes, prefix + r.include_context.prefix)


def _routes():
    out = []
    for methods, path in _walk(service_app.app.routes):
        for m in methods - {"HEAD", "OPTIONS"}:
            out.append((m, path))
    return out


def _concrete(path):
    for name in ("job_id", "doc_id", "schema_id", "connector_id"):
        path = path.replace("{" + name + "}", "x-1")
    return path


def _bearer(token):
    return {"Authorization": f"Bearer {token}"}


def _call(client, method, path, token=None):
    headers = _bearer(token) if token else {}
    return client.request(method, _concrete(path), headers=headers)


# ---- the matrix covers the real route list ----

def test_matrix_covers_every_route_and_nothing_else():
    """/openapi.json is a plain Starlette route, not an APIRoute; test_openapi_json_is_public covers it."""
    assert set(_routes()) == set(ROLE) | (PUBLIC - {("GET", "/openapi.json")})


# ---- authentication ----

@pytest.mark.parametrize("method,path", _routes())
def test_every_route_requires_a_token(client, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    assert _call(client, method, path).status_code == 401


@pytest.mark.parametrize("method,path", _routes())
@pytest.mark.parametrize("other", ["digitize", "similarity", "chat", "summarize", "extract", "translate", "forecast"])
def test_token_for_another_service_is_refused_everywhere(client, signer, method, path, other):
    """A token issued for another audience must not work, whatever its roles."""
    if (method, path) in PUBLIC or other == SERVICE:
        pytest.skip("not applicable")
    tok = signer.token(aud=[other], scope=f"{other}:admin")
    assert _call(client, method, path, tok).status_code == 401


def test_expired_token_is_refused(client, signer):
    method, path = next(iter(sorted(ROLE)))
    assert _call(client, method, path, signer.token(exp=1)).status_code == 401


def test_unknown_path_is_denied_before_routing(client):
    assert client.get("/v2/anything").status_code == 401


def test_swagger_docs_are_public_and_redoc_is_gone(client, signer):
    assert client.get("/docs").status_code == 200  # public so users can browse the API
    assert client.post("/docs").status_code == 401  # only GET/HEAD are public
    assert client.get("/redoc").status_code == 401  # unknown paths are denied, not 404
    assert client.get("/redoc", headers=_bearer(signer.token())).status_code == 404


# ---- authorization ----

@pytest.mark.parametrize("method,path", sorted(ROLE))
@pytest.mark.parametrize("held", ["read", "write", "admin"])
def test_role_hierarchy_per_route(client, signer, method, path, held):
    """admin implies write implies read. Too low a role is 403, enough is not."""
    r = _call(client, method, path, signer.token(scope=f"{SERVICE}:{held}"))
    if RANK[held] >= RANK[ROLE[(method, path)]]:
        assert r.status_code not in (401, 403), (method, path, held, r.status_code)
    else:
        assert r.status_code == 403, (method, path, held, r.status_code)


@pytest.mark.parametrize("method,path", sorted(ROLE))
def test_token_with_no_role_for_this_service_is_forbidden_everywhere(client, signer, method, path):
    other = "digitize" if SERVICE != "digitize" else "chat"
    assert _call(client, method, path, signer.token(scope=f"{other}:admin")).status_code == 403


@pytest.mark.parametrize("method,path", sorted(ROLE))
def test_role_of_another_service_in_the_scope_does_not_help(client, signer, method, path):
    """A token for this audience whose only roles belong to another service is forbidden."""
    assert _call(client, method, path, signer.token(scope="similarity:admin")).status_code == 403


def test_swagger_root_is_not_public(client, signer):
    assert client.get("/").status_code == 401


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


def test_ready_is_503_without_a_leak_when_jwks_is_unavailable():
    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience=SERVICE)
    failing = CountingFetcher(exc=JWKSUnavailable("secret-host.internal refused"))
    set_verifier(TokenVerifier(config, JWKSClient(config, failing)))
    r = TestClient(service_app.app, raise_server_exceptions=False).get("/ready")
    assert r.status_code == 503
    assert "secret-host" not in r.text


# ---- startup ----

@pytest.mark.parametrize("missing", [("AUTH_JWKS_URL",), ("AUTH_AUDIENCE",), ("AUTH_JWKS_URL", "AUTH_AUDIENCE")])
def test_startup_fails_closed_without_auth_config(monkeypatch, missing):
    set_verifier(None)
    monkeypatch.setenv("AUTH_JWKS_URL", "https://catalog.invalid/jwks")
    monkeypatch.setenv("AUTH_AUDIENCE", SERVICE)
    for k in missing:
        monkeypatch.delenv(k, raising=False)
    with pytest.raises(AuthConfigError):
        with TestClient(service_app.app):
            pass
