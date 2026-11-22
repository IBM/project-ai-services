"""Chat enforces catalog-issued tokens on every route (deny by default).

It also keeps the vLLM API key out of the caller's hands: the inbound Authorization header is a
catalog token and is never forwarded, and the outgoing vLLM key comes only from LLM_API_KEY.
"""

import re
import sys
from pathlib import Path
from unittest.mock import Mock, patch

import pytest
from fastapi.routing import APIRoute
from fastapi.testclient import TestClient
from pydantic import SecretStr

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

import chatbot.app as chat_app
import common.misc_utils as misc_utils
from common.auth import AuthConfigError, JWKSClient, TokenVerifier, set_verifier
from common.auth.config import AuthConfig
from common.tests.auth.helpers import CountingFetcher, Signer, jwks_body

pytestmark = pytest.mark.real_auth

PUBLIC = {("GET", "/health"), ("GET", "/openapi.json"), ("GET", "/ready")}
CHAT = ("POST", "/v1/chat/completions")
BODY = {"messages": [{"content": "What is AI?"}], "stream": False, "max_tokens": 64}
VLLM_KEY = "vllm-deployment-secret"


class ChatSigner(Signer):
    """A signer whose default tokens are for the chat audience."""

    def claims(self, **over):
        base = {"aud": ["chat"], "scope": "chat:admin"}
        base.update(over)
        return super().claims(**base)


@pytest.fixture
def signer():
    return ChatSigner()


@pytest.fixture
def client(signer):
    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="chat")
    set_verifier(TokenVerifier(config, JWKSClient(config, CountingFetcher(jwks_body(signer)))))
    return TestClient(chat_app.app, raise_server_exceptions=False)


@pytest.fixture
def vllm_key(monkeypatch):
    monkeypatch.setattr(chat_app.settings.common.llm, "api_key", SecretStr(VLLM_KEY))


@pytest.fixture
def wired(monkeypatch, sample_documents):
    """Everything a completion needs, with the vLLM calls mocked so their arguments can be read."""
    monkeypatch.setattr(chat_app, "vectorstore", Mock())
    monkeypatch.setattr(chat_app, "emb_model_dict", {"emb_model": "m", "emb_endpoint": "http://e", "max_model_len": 512})
    monkeypatch.setattr(chat_app, "llm_model_dict", {"llm_model": "m", "llm_endpoint": "http://llm"})
    monkeypatch.setattr(chat_app, "validate_query_length", Mock(return_value=(True, None)))
    monkeypatch.setattr(chat_app, "detect_language", Mock(return_value="EN"))
    monkeypatch.setattr(chat_app, "search_only", Mock(return_value=(sample_documents, {})))
    non_stream = Mock(return_value={"choices": [{"message": {"content": "answer"}}]})
    stream = Mock(return_value=iter(['data: {"choices":[{"delta":{"content":"hi"}}]}\n\n', "data: [DONE]\n\n"]))
    monkeypatch.setattr(chat_app, "query_vllm_non_stream", non_stream)
    monkeypatch.setattr(chat_app, "query_vllm_stream", stream)
    return Mock(non_stream=non_stream, stream=stream)


def _routes():
    out = []
    for r in chat_app.app.routes:
        if isinstance(r, APIRoute):
            for m in r.methods - {"HEAD", "OPTIONS"}:
                out.append((m, r.path))
    return out


def _bearer(token):
    return {"Authorization": f"Bearer {token}"}


def _token(signer, scope):
    return signer.token(sub="user-1", scope=scope)


# ---- deny by default ----

def test_route_matrix_is_not_empty():
    routes = _routes()
    for expected in (CHAT, ("GET", "/v1/models"), ("GET", "/v1/perf_metrics"), ("GET", "/db-status"),
                     ("GET", "/"), ("GET", "/ready"), ("GET", "/health")):
        assert expected in routes


@pytest.mark.parametrize("method,path", _routes())
def test_every_route_requires_a_token(client, method, path):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    assert client.request(method, path).status_code == 401


@pytest.mark.parametrize("method,path", _routes())
@pytest.mark.parametrize("aud,scope", [("digitize", "digitize:admin"), ("similarity", "similarity:admin"),
                                       ("chatbot", "chatbot:admin")])
def test_token_for_another_audience_is_refused_everywhere(client, signer, method, path, aud, scope):
    if (method, path) in PUBLIC:
        pytest.skip("public by design")
    tok = signer.token(aud=[aud], scope=scope)
    assert client.request(method, path, headers=_bearer(tok)).status_code == 401


def test_unknown_path_is_denied_before_routing(client):
    assert client.get("/v2/anything").status_code == 401


def test_builtin_docs_are_gone(client, signer):
    assert client.get("/docs").status_code == 401
    assert client.get("/redoc").status_code == 401
    assert client.get("/docs", headers=_bearer(signer.token())).status_code == 404
    assert client.get("/redoc", headers=_bearer(signer.token())).status_code == 404


def test_expired_token_is_refused(client, signer):
    assert client.post(CHAT[1], json=BODY, headers=_bearer(signer.token(exp=1))).status_code == 401


def test_body_is_not_validated_before_authentication(client):
    assert client.post(CHAT[1], json={"messages": "bogus"}).status_code == 401
    assert client.post(CHAT[1], content=b"not json").status_code == 401


# ---- roles ----

def test_swagger_root_needs_admin(client, signer):
    assert client.get("/").status_code == 401
    for scope in ("chat:read", "chat:write"):
        assert client.get("/", headers=_bearer(_token(signer, scope))).status_code == 403, scope
    assert client.get("/", headers=_bearer(_token(signer, "chat:admin"))).status_code == 200


def test_perf_metrics_needs_admin(client, signer):
    path = "/v1/perf_metrics"
    for scope in ("chat:read", "chat:write"):
        assert client.get(path, headers=_bearer(_token(signer, scope))).status_code == 403, scope
    assert client.get(path, headers=_bearer(_token(signer, "chat:admin"))).status_code == 200


def test_a_token_with_no_chat_role_is_forbidden(client, signer):
    tok = _token(signer, "digitize:admin")
    for method, path in ((CHAT[0], CHAT[1]), ("GET", "/v1/models"), ("GET", "/db-status")):
        kwargs = {"json": BODY} if method == "POST" else {}
        assert client.request(method, path, headers=_bearer(tok), **kwargs).status_code == 403, path


@pytest.mark.parametrize("scope", ["chat:read", "chat:write", "chat:admin"])
def test_completion_models_and_db_status_are_allowed_for_read_and_above(client, signer, wired, monkeypatch, scope):
    """DA1: admin implies write implies read."""
    monkeypatch.setattr(chat_app, "query_vllm_models", Mock(return_value={"object": "list", "data": []}))
    monkeypatch.setattr(chat_app.vectorstore, "check_db_populated", Mock(return_value=True))
    h = _bearer(_token(signer, scope))
    assert client.post(CHAT[1], json=BODY, headers=h).status_code == 200
    assert client.get("/v1/models", headers=h).status_code == 200
    assert client.get("/db-status", headers=h).status_code == 200


# ---- the vLLM key is the deployment's, never the caller's ----

def test_inbound_bearer_is_never_sent_to_vllm_non_stream(client, signer, wired, vllm_key):
    tok = _token(signer, "chat:read")
    assert client.post(CHAT[1], json=BODY, headers=_bearer(tok)).status_code == 200
    args = wired.non_stream.call_args.args
    assert args[9] == VLLM_KEY
    assert tok not in map(str, args)


def test_inbound_bearer_is_never_sent_to_vllm_stream(client, signer, wired, vllm_key):
    tok = _token(signer, "chat:read")
    r = client.post(CHAT[1], json={**BODY, "stream": True}, headers=_bearer(tok))
    assert r.status_code == 200
    args = wired.stream.call_args.args
    assert args[9] == VLLM_KEY
    assert tok not in map(str, args)


def test_no_vllm_key_configured_sends_none(client, signer, wired, monkeypatch):
    monkeypatch.setattr(chat_app.settings.common.llm, "api_key", SecretStr(""))
    assert client.post(CHAT[1], json=BODY, headers=_bearer(_token(signer, "chat:read"))).status_code == 200
    assert wired.non_stream.call_args.args[9] is None


def test_rephrasing_gets_the_deployment_key(client, signer, wired, vllm_key, monkeypatch):
    rephrase = Mock()

    async def fake(**kwargs):
        rephrase(**kwargs)
        return "rephrased"

    monkeypatch.setattr(chat_app, "rephrase_query_with_context", fake)
    monkeypatch.setattr(chat_app, "tokenize_with_llm", Mock(return_value=[0]))
    monkeypatch.setattr(chat_app, "truncate_history_by_tokens", Mock(return_value=[{"role": "user", "content": "x"}]))
    body = {**BODY, "messages": [{"role": "user", "content": "first"}, {"role": "assistant", "content": "a"},
                                 {"role": "user", "content": "second"}]}
    tok = _token(signer, "chat:read")
    assert client.post(CHAT[1], json=body, headers=_bearer(tok)).status_code == 200
    assert rephrase.call_args.kwargs["api_key"] == VLLM_KEY


def _session(monkeypatch):
    session = Mock()
    session.get.return_value = Mock(json=Mock(return_value={"object": "list", "data": []}))
    monkeypatch.setattr(misc_utils, "SESSION", session)
    monkeypatch.setattr(chat_app, "llm_model_dict", {"llm_model": "m", "llm_endpoint": "http://llm"})
    return session


def test_models_sends_the_deployment_key_to_vllm_not_the_callers_token(client, signer, vllm_key, monkeypatch):
    session = _session(monkeypatch)
    tok = _token(signer, "chat:read")
    assert client.get("/v1/models", headers=_bearer(tok)).status_code == 200
    sent = session.get.call_args.kwargs["headers"]
    assert sent["Authorization"] == f"Bearer {VLLM_KEY}"
    assert tok not in sent["Authorization"]


def test_models_sends_no_authorization_when_vllm_auth_is_off(client, signer, monkeypatch):
    monkeypatch.setattr(chat_app.settings.common.llm, "api_key", SecretStr(""))
    session = _session(monkeypatch)
    assert client.get("/v1/models", headers=_bearer(_token(signer, "chat:read"))).status_code == 200
    assert "Authorization" not in session.get.call_args.kwargs["headers"]


def test_a_vllm_key_sent_as_the_inbound_bearer_is_refused(client, vllm_key):
    """The old behaviour: callers passed the vLLM key. It is not a catalog token, so it gets 401."""
    for method, path in (("GET", "/v1/models"), CHAT, ("GET", "/db-status")):
        kwargs = {"json": BODY} if method == "POST" else {}
        assert client.request(method, path, headers=_bearer(VLLM_KEY), **kwargs).status_code == 401, path


# ---- probes ----

def test_health_is_public(client):
    assert client.get("/health").status_code == 200


def test_ready_is_public_and_performs_jwks_check(client):
    r = client.get("/ready")
    assert r.status_code == 200
    assert r.json() == {"status": "ready"}


def test_ready_is_503_without_a_leak_when_jwks_is_unavailable():
    from common.auth import JWKSUnavailable

    config = AuthConfig(jwks_url="https://catalog.invalid/jwks", audience="chat")
    failing = CountingFetcher(exc=JWKSUnavailable("secret-host.internal refused"))
    set_verifier(TokenVerifier(config, JWKSClient(config, failing)))
    r = TestClient(chat_app.app, raise_server_exceptions=False).get("/ready")
    assert r.status_code == 503
    assert "secret-host" not in r.text


# ---- error sweep ----

def test_models_error_does_not_leak_exception_text(client, signer, monkeypatch):
    monkeypatch.setattr(chat_app, "llm_model_dict", {"llm_model": "m", "llm_endpoint": "http://llm"})
    boom = Mock(side_effect=RuntimeError("password=hunter2 at 10.0.0.5:8000"))
    monkeypatch.setattr(chat_app, "query_vllm_models", boom)
    r = client.get("/v1/models", headers=_bearer(_token(signer, "chat:read")))
    assert r.status_code == 500
    for secret in ("hunter2", "10.0.0.5", "RuntimeError"):
        assert secret not in r.text


def test_completion_error_does_not_leak_exception_text(client, signer, wired):
    wired.non_stream.side_effect = None
    chat_app.search_only.side_effect = RuntimeError("password=hunter2 at 10.0.0.5:9200")
    r = client.post(CHAT[1], json=BODY, headers=_bearer(_token(signer, "chat:read")))
    assert r.status_code == 500
    for secret in ("hunter2", "10.0.0.5", "RuntimeError"):
        assert secret not in r.text


def test_vllm_error_body_does_not_leak(client, signer, wired):
    wired.non_stream.return_value = {"error": "backend at 10.0.0.5 said hunter2"}
    r = client.post(CHAT[1], json=BODY, headers=_bearer(_token(signer, "chat:read")))
    assert r.status_code == 500
    assert "hunter2" not in r.text
    assert "10.0.0.5" not in r.text


def test_db_status_error_does_not_leak(client, signer, monkeypatch):
    store = Mock()
    store.check_db_populated.side_effect = RuntimeError("password=hunter2")
    monkeypatch.setattr(chat_app, "vectorstore", store)
    r = client.get("/db-status", headers=_bearer(_token(signer, "chat:read")))
    assert "hunter2" not in r.text


def test_vectorstore_not_ready_does_not_leak(client, signer, wired, monkeypatch):
    from common.vector_db import VectorStoreNotReadyError

    chat_app.search_only.side_effect = VectorStoreNotReadyError("opensearch at 10.0.0.9 refused")
    r = client.post(CHAT[1], json=BODY, headers=_bearer(_token(signer, "chat:read")))
    assert r.status_code == 503
    assert "10.0.0.9" not in r.text


def test_vllm_stream_failure_does_not_leak_to_the_caller(monkeypatch):
    from common.llm_utils import query_vllm_stream

    session = Mock()
    session.post.side_effect = RuntimeError("password=hunter2 at 10.0.0.5:8000")
    monkeypatch.setattr(misc_utils, "SESSION", session)
    with patch("common.llm_utils.query_vllm_payload", return_value=({}, {})):
        out = "".join(query_vllm_stream("q", [], "http://llm", "m", [], 10, 0.1, {}, "EN"))
    assert "hunter2" not in out
    assert "10.0.0.5" not in out
    assert "[DONE]" in out


def test_no_error_text_is_formatted_into_client_responses():
    """Static guard: no repr/str/exception formatting in raise_error calls or db-status messages."""
    src = Path(chat_app.__file__).read_text()
    for call in re.findall(r"APIError\.raise_error\([^)]*\)", src):
        assert not re.search(r"repr\(|str\(|\{e\}|\{exc\}|\{err\}", call), call
    assert "message=str(e)" not in src


# ---- startup ----

def test_startup_fails_closed_without_auth_config(monkeypatch):
    set_verifier(None)
    for k in ("AUTH_JWKS_URL", "AUTH_AUDIENCE"):
        monkeypatch.delenv(k, raising=False)
    with pytest.raises(AuthConfigError):
        with TestClient(chat_app.app):
            pass


def test_the_vllm_key_scheme_is_gone():
    """The caller-supplied vLLM key plumbing must not come back."""
    assert not hasattr(chat_app, "is_auth_required")
    assert not hasattr(chat_app, "security")
    assert "HTTPBearer" not in Path(chat_app.__file__).read_text()
