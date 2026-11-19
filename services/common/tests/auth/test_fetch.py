"""fetch_jwks against a real local HTTP server: redirects, size cap, status, TLS flag."""

import http.server
import threading

import pytest

from common.auth import jwks as jwks_mod
from common.auth.jwks import JWKSUnavailable, fetch_jwks
from common.tests.auth.helpers import Signer, jwks_body, make_config


@pytest.fixture
def server():
    state = {"body": b"", "status": 200, "location": None, "hits": 0}

    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            state["hits"] += 1
            if self.path == "/target":
                state["target_hit"] = True
            self.send_response(state["status"])
            if state["location"]:
                self.send_header("Location", state["location"])
            self.send_header("Content-Length", str(len(state["body"])))
            self.end_headers()
            self.wfile.write(state["body"])

        def log_message(self, *a):
            pass

    srv = http.server.HTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    state["url"] = f"http://127.0.0.1:{srv.server_port}/jwks"
    yield state
    srv.shutdown()


def test_ok(server):
    server["body"] = jwks_body(Signer())
    assert fetch_jwks(make_config(jwks_url=server["url"])) == server["body"]


def test_redirect_is_not_followed(server):
    server.update(status=302, location=server["url"].replace("/jwks", "/target"))
    with pytest.raises(JWKSUnavailable):
        fetch_jwks(make_config(jwks_url=server["url"]))
    assert "target_hit" not in server


def test_non_200_rejected(server):
    server["status"] = 500
    with pytest.raises(JWKSUnavailable):
        fetch_jwks(make_config(jwks_url=server["url"]))


def test_oversized_body_rejected(server):
    server["body"] = b"x" * (jwks_mod.MAX_JWKS_BYTES + 1)
    with pytest.raises(JWKSUnavailable):
        fetch_jwks(make_config(jwks_url=server["url"]))


def test_unreachable_is_unavailable():
    with pytest.raises(JWKSUnavailable):
        fetch_jwks(make_config(jwks_url="http://127.0.0.1:1/jwks"))


def test_certificate_verification_is_never_disabled(monkeypatch):
    seen = {}

    class FakeSession:
        trust_env = True

        def get(self, url, **kw):
            seen.update(kw)
            raise jwks_mod.requests.ConnectionError("stop")

        def close(self):
            pass

    monkeypatch.setattr(jwks_mod.requests, "Session", FakeSession)
    for bundle, expected in ((None, True), ("/etc/ca.pem", "/etc/ca.pem")):
        with pytest.raises(JWKSUnavailable):
            fetch_jwks(make_config(ca_bundle=bundle))
        assert seen["verify"] == expected
        assert seen["allow_redirects"] is False
        assert seen["timeout"] == jwks_mod.FETCH_TIMEOUT_SECONDS
