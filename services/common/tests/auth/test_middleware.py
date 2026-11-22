"""AuthMiddleware: deny by default, answer before the body is read, never fail open."""

import asyncio

import pytest
from fastapi import Depends, FastAPI, File, UploadFile
from fastapi.testclient import TestClient

from common.auth import AuthMiddleware, JWKSClient, TokenVerifier, require_role
from common.auth.jwks import JWKSUnavailable
from common.tests.auth.helpers import CountingFetcher, Signer, jwks_body, make_config


def _app(fetcher, public=None):
    config = make_config()
    verifier = TokenVerifier(config, JWKSClient(config, fetcher))
    app = FastAPI(docs_url=None, redoc_url=None)
    kw = {"verifier_provider": lambda: verifier}
    if public is not None:
        kw["public_paths"] = public
    app.add_middleware(AuthMiddleware, **kw)

    @app.get("/health")
    def health():
        return {"status": "ok"}

    @app.get("/v1/admin-only", dependencies=[Depends(require_role("admin"))])
    def admin_only():
        return {"ok": True}

    @app.get("/v1/write-only", dependencies=[Depends(require_role("write"))])
    def write_only():
        return {"ok": True}

    @app.post("/v1/upload")
    async def upload(files: list[UploadFile] = File(...)):
        return {"n": len(files)}

    @app.get("/v1/unprotected-by-dependency")
    def forgot_dependency():
        return {"ok": True}

    return app


@pytest.fixture
def signer():
    return Signer()


@pytest.fixture
def client(signer):
    return TestClient(_app(CountingFetcher(jwks_body(signer))))


def _h(tok):
    return {"Authorization": f"Bearer {tok}"}


def test_health_is_public(client):
    assert client.get("/health").status_code == 200


def test_post_to_public_path_is_not_public(client):
    assert client.post("/health").status_code == 401


def test_no_token_is_401_with_challenge(client):
    r = client.get("/v1/admin-only")
    assert r.status_code == 401
    assert r.headers["www-authenticate"] == "Bearer"
    assert r.json()["error"]["message"] == "Authentication required"


def test_unknown_path_is_401_not_404(client):
    assert client.get("/nope").status_code == 401


def test_route_without_dependency_still_needs_a_token(client, signer):
    assert client.get("/v1/unprotected-by-dependency").status_code == 401
    assert client.get("/v1/unprotected-by-dependency", headers=_h(signer.token())).status_code == 200


def test_valid_admin_token(client, signer):
    assert client.get("/v1/admin-only", headers=_h(signer.token())).status_code == 200


def test_valid_token_without_role_is_403(client, signer):
    tok = signer.token(scope="digitize:read")
    assert client.get("/v1/write-only", headers=_h(tok)).status_code == 403
    assert client.get("/v1/admin-only", headers=_h(tok)).status_code == 403


def test_admin_token_satisfies_lower_roles(client, signer):
    # DA1: admin implies write and read.
    assert client.get("/v1/write-only", headers=_h(signer.token())).status_code == 200


def test_token_for_another_service_is_401(client, signer):
    r = client.get("/v1/admin-only", headers=_h(signer.token(aud=["other"], scope="other:admin")))
    assert r.status_code == 401


@pytest.mark.parametrize("hdr", ["Basic abc", "Bearer", "Bearer a b", "bearer", "Token abc", ""])
def test_malformed_authorization_header(client, hdr):
    assert client.get("/v1/admin-only", headers={"Authorization": hdr}).status_code == 401


def test_error_body_does_not_reveal_the_reason(client, signer):
    expired = client.get("/v1/admin-only", headers=_h(signer.token(exp=1))).json()
    garbage = client.get("/v1/admin-only", headers=_h("garbage")).json()
    assert expired == garbage


def test_jwks_down_is_503_never_200(signer):
    c = TestClient(_app(CountingFetcher(exc=JWKSUnavailable("down"))))
    r = c.get("/v1/admin-only", headers=_h(signer.token()))
    assert r.status_code == 503
    assert c.get("/health").status_code == 200  # liveness is independent of JWKS


def test_openapi_is_public_by_default_and_only_for_get(client):
    assert client.get("/openapi.json").status_code != 401
    assert client.post("/openapi.json").status_code == 401


def test_openapi_is_protected_when_not_listed(signer):
    c = TestClient(_app(CountingFetcher(jwks_body(signer)), public={"/health"}))
    assert c.get("/openapi.json").status_code == 401
    assert c.get("/openapi.json", headers=_h(signer.token())).status_code == 200


def test_response_has_no_store(client):
    assert client.get("/v1/admin-only").headers["cache-control"] == "no-store"


def test_unauthenticated_upload_is_rejected_without_reading_the_body(signer):
    """The reason this is a middleware: a dependency would parse the whole upload first."""
    app = _app(CountingFetcher(jwks_body(signer)))
    received = {"bytes": 0}
    body_chunks = [b"x" * 65536] * 8 + [b""]

    async def run():
        msgs = []

        async def receive():
            if body_chunks:
                chunk = body_chunks.pop(0)
                received["bytes"] += len(chunk)
                return {"type": "http.request", "body": chunk, "more_body": bool(body_chunks)}
            return {"type": "http.disconnect"}

        async def send(m):
            msgs.append(m)

        scope = {
            "type": "http", "method": "POST", "path": "/v1/upload", "raw_path": b"/v1/upload",
            "query_string": b"", "root_path": "", "scheme": "http", "http_version": "1.1",
            "headers": [(b"content-type", b"multipart/form-data; boundary=xx"), (b"content-length", b"524288")],
            "client": ("127.0.0.1", 1), "server": ("test", 80),
        }
        await app(scope, receive, send)
        return msgs

    msgs = asyncio.run(run())
    assert msgs[0]["status"] == 401
    assert received["bytes"] == 0


def test_websocket_is_refused(client):
    from starlette.websockets import WebSocketDisconnect
    with pytest.raises(WebSocketDisconnect):
        with client.websocket_connect("/health"):
            pass
