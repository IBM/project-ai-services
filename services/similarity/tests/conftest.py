"""Pytest configuration for similarity service tests."""

import pytest


@pytest.fixture(autouse=True)
def authenticated_admin(request, monkeypatch):
    """Give every TestClient an admin identity so endpoint tests exercise the handlers.

    Authentication itself is covered by common/tests/auth and test_auth_enforcement.py.
    Tests marked ``real_auth`` get the real middleware and no injected token.
    """
    from fastapi.testclient import TestClient
    from common.auth import Principal, TokenVerifier, set_verifier
    from common.auth.config import AuthConfig

    if request.node.get_closest_marker("real_auth"):
        yield
        set_verifier(None)
        return

    class _AdminVerifier(TokenVerifier):
        async def verify(self, token):
            return Principal(
                subject="test-admin", idp="local", kind="user", audience="similarity",
                roles=frozenset({"admin"}), token_id="test",
            )

    set_verifier(_AdminVerifier(AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="similarity")))
    original_init = TestClient.__init__

    def init(self, *args, **kwargs):
        original_init(self, *args, **kwargs)
        self.headers.setdefault("Authorization", "Bearer test-token")

    monkeypatch.setattr(TestClient, "__init__", init)
    yield
    set_verifier(None)
