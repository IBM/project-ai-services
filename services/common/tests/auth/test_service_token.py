"""Outbound service-token client: config rules, cache, fail-closed behaviour, request shape."""

import json
import time
from datetime import datetime, timezone

import pytest
import requests

from common.auth import service_token as st
from common.auth.config import AuthConfigError
from common.auth.service_token import (
    ServiceCredentialConfig,
    ServiceTokenClient,
    ServiceTokenUnavailable,
    exchange_credential,
    get_service_token,
    read_credential,
    reset_service_token_client,
    set_service_token_client,
)

ENV = ("AUTH_SERVICE_CREDENTIAL_FILE", "AUTH_CATALOG_API_URL", "AUTH_CATALOG_CA_BUNDLE",
       "AUTH_JWKS_PLAINTEXT_HOSTS")


@pytest.fixture(autouse=True)
def _clean(monkeypatch):
    for name in ENV:
        monkeypatch.delenv(name, raising=False)
    reset_service_token_client()
    yield
    reset_service_token_client()


def cfg(tmp_path, content="id.secret\n", **kw):
    f = tmp_path / "credential"
    if content is not None:
        f.write_text(content)
    base = dict(catalog_url="https://catalog.example", credential_file=str(f))
    base.update(kw)
    return ServiceCredentialConfig(**base)


# ---- configuration ----

def test_no_credential_file_means_not_configured():
    assert ServiceCredentialConfig.from_env() is None
    assert get_service_token("similarity") is None


def test_credential_file_without_catalog_url_is_an_error(monkeypatch):
    monkeypatch.setenv("AUTH_SERVICE_CREDENTIAL_FILE", "/x")
    with pytest.raises(AuthConfigError):
        ServiceCredentialConfig.from_env()
    with pytest.raises(AuthConfigError):
        get_service_token("similarity")


@pytest.mark.parametrize("url", [
    "http://catalog.example", "ftp://x", "https://u:p@catalog.example", "catalog.example", "https://",
])
def test_unsafe_catalog_url_is_refused(monkeypatch, url):
    monkeypatch.setenv("AUTH_SERVICE_CREDENTIAL_FILE", "/x")
    monkeypatch.setenv("AUTH_CATALOG_API_URL", url)
    with pytest.raises(AuthConfigError):
        ServiceCredentialConfig.from_env()


def test_https_loopback_and_listed_plaintext_hosts_are_allowed(monkeypatch):
    monkeypatch.setenv("AUTH_SERVICE_CREDENTIAL_FILE", "/x")
    for url in ("https://catalog.example/", "http://127.0.0.1:8080", "http://localhost:8080"):
        monkeypatch.setenv("AUTH_CATALOG_API_URL", url)
        assert ServiceCredentialConfig.from_env().catalog_url == url.rstrip("/")

    monkeypatch.setenv("AUTH_CATALOG_API_URL", "http://catalog-api.ns.svc:8080")
    monkeypatch.setenv("AUTH_JWKS_PLAINTEXT_HOSTS", "Catalog-API.ns.svc")
    monkeypatch.setenv("AUTH_CATALOG_CA_BUNDLE", "/ca.crt")
    c = ServiceCredentialConfig.from_env()
    assert c.ca_bundle == "/ca.crt"
    assert "catalog-api.ns.svc" in c.plaintext_hosts


# ---- credential file ----

def test_credential_file_is_read_and_trimmed(tmp_path):
    assert read_credential(cfg(tmp_path, " id.secret \n")) == "id.secret"


@pytest.mark.parametrize("content", [None, "", "  \n", "x" * 2000])
def test_missing_empty_or_huge_credential_file_fails(tmp_path, content):
    with pytest.raises(ServiceTokenUnavailable):
        read_credential(cfg(tmp_path, content))


# ---- cache ----

class Exchanger:
    def __init__(self, lifetime=900, fail=False):
        self.calls, self.lifetime, self.fail, self.audiences = 0, lifetime, fail, []

    def __call__(self, config, audience):
        self.calls += 1
        self.audiences.append(audience)
        if self.fail:
            raise ServiceTokenUnavailable("down")
        return f"tok-{self.calls}", time.time() + self.lifetime


def test_token_is_cached_per_audience(tmp_path):
    ex = Exchanger()
    client = ServiceTokenClient(cfg(tmp_path), ex)
    assert client.get_token("similarity") == client.get_token("similarity") == "tok-1"
    assert client.get_token("other") == "tok-2"
    assert ex.calls == 2
    assert ex.audiences == ["similarity", "other"]


def test_token_near_expiry_is_replaced_before_use(tmp_path):
    ex = Exchanger(lifetime=st.REFRESH_MARGIN_SECONDS - 5)
    client = ServiceTokenClient(cfg(tmp_path), ex)
    assert client.get_token("similarity") == "tok-1"
    assert client.get_token("similarity") == "tok-2"


def test_failure_with_no_cached_token_raises_and_backs_off(tmp_path):
    ex = Exchanger(fail=True)
    client = ServiceTokenClient(cfg(tmp_path), ex)
    with pytest.raises(ServiceTokenUnavailable):
        client.get_token("similarity")
    with pytest.raises(ServiceTokenUnavailable):
        client.get_token("similarity")
    assert ex.calls == 1, "second call fails fast inside the back-off window"


def test_failure_after_back_off_retries_and_recovers(tmp_path):
    now = [time.time()]
    ex = Exchanger(fail=True)
    client = ServiceTokenClient(cfg(tmp_path), ex, clock=lambda: now[0])
    with pytest.raises(ServiceTokenUnavailable):
        client.get_token("similarity")
    now[0] += st.FAILURE_BACKOFF_SECONDS + 1
    ex.fail = False
    assert client.get_token("similarity") == "tok-2"
    assert ex.calls == 2


def test_unexpired_token_survives_a_catalog_outage(tmp_path):
    now = [time.time()]
    ex = Exchanger(lifetime=st.REFRESH_MARGIN_SECONDS + 30)
    client = ServiceTokenClient(cfg(tmp_path), ex, clock=lambda: now[0])
    assert client.get_token("similarity") == "tok-1"
    now[0] += 40  # inside the refresh margin, but not yet expired
    ex.fail = True
    assert client.get_token("similarity") == "tok-1"


def test_expired_token_is_never_returned_when_the_catalog_is_down(tmp_path):
    now = [time.time()]
    ex = Exchanger(lifetime=st.REFRESH_MARGIN_SECONDS + 30)
    client = ServiceTokenClient(cfg(tmp_path), ex, clock=lambda: now[0])
    client.get_token("similarity")
    now[0] += 3600
    ex.fail = True
    with pytest.raises(ServiceTokenUnavailable):
        client.get_token("similarity")


def test_an_already_expired_token_from_the_catalog_is_refused(tmp_path):
    client = ServiceTokenClient(cfg(tmp_path), lambda c, a: ("t", time.time() - 1))
    with pytest.raises(ServiceTokenUnavailable):
        client.get_token("similarity")


def test_get_service_token_uses_the_installed_client(tmp_path):
    set_service_token_client(ServiceTokenClient(cfg(tmp_path), Exchanger()))
    assert get_service_token("similarity") == "tok-1"
    set_service_token_client(None)
    assert get_service_token("similarity") is None


# ---- request shape ----

class FakeResp:
    def __init__(self, status=200, body=b"", chunks=None):
        self.status_code = status
        self._chunks = chunks if chunks is not None else [body]

    def iter_content(self, chunk_size=0):
        return iter(self._chunks)

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


class FakeSession:
    last = None

    def __init__(self, resp=None, exc=None):
        self.resp, self.exc, self.trust_env, self.kwargs, self.url = resp, exc, True, None, None
        FakeSession.last = self

    def post(self, url, **kwargs):
        self.url, self.kwargs = url, kwargs
        if self.exc:
            raise self.exc
        return self.resp

    def close(self):
        pass


def good_body():
    exp = datetime.fromtimestamp(time.time() + 900, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    return json.dumps({"token": "a.b.c", "expires_at": exp}).encode()


def patch_session(monkeypatch, **kw):
    monkeypatch.setattr(st.requests, "Session", lambda: FakeSession(**kw))


def test_exchange_request_shape(monkeypatch, tmp_path):
    patch_session(monkeypatch, resp=FakeResp(body=good_body()))
    token, exp = exchange_credential(cfg(tmp_path, ca_bundle="/ca.crt"), "similarity")

    s = FakeSession.last
    assert token == "a.b.c" and exp > time.time()
    assert s.url == "https://catalog.example/api/v1/auth/service-credential/token"
    assert s.trust_env is False
    assert s.kwargs["json"] == {"credential": "id.secret", "service": "similarity"}
    assert s.kwargs["allow_redirects"] is False
    assert s.kwargs["verify"] == "/ca.crt"
    assert s.kwargs["timeout"] == st.EXCHANGE_TIMEOUT_SECONDS


def test_exchange_verifies_certificates_by_default(monkeypatch, tmp_path):
    patch_session(monkeypatch, resp=FakeResp(body=good_body()))
    exchange_credential(cfg(tmp_path), "similarity")
    assert FakeSession.last.kwargs["verify"] is True


@pytest.mark.parametrize("resp", [
    FakeResp(status=401, body=b'{"error":"invalid credential"}'),
    FakeResp(status=302), FakeResp(status=500), FakeResp(body=b"not json"),
    FakeResp(body=b'{"token":"x"}'), FakeResp(body=b'{"token":"","expires_at":"2030-01-01T00:00:00Z"}'),
    FakeResp(body=b'{"token":"x","expires_at":"soon"}'), FakeResp(body=b"[]"),
    FakeResp(chunks=[b"x" * 9000, b"x" * 9000]),
])
def test_bad_responses_are_unavailable(monkeypatch, tmp_path, resp):
    patch_session(monkeypatch, resp=resp)
    with pytest.raises(ServiceTokenUnavailable):
        exchange_credential(cfg(tmp_path), "similarity")


def test_network_error_message_has_no_url_or_credential(monkeypatch, tmp_path):
    patch_session(monkeypatch, exc=requests.ConnectionError("https://catalog.example id.secret"))
    with pytest.raises(ServiceTokenUnavailable) as e:
        exchange_credential(cfg(tmp_path), "similarity")
    assert "id.secret" not in str(e.value) and "catalog.example" not in str(e.value)


def test_error_messages_never_contain_the_credential_or_response_body(monkeypatch, tmp_path):
    patch_session(monkeypatch, resp=FakeResp(status=401, body=b"SECRET-BODY"))
    with pytest.raises(ServiceTokenUnavailable) as e:
        exchange_credential(cfg(tmp_path), "similarity")
    assert "SECRET-BODY" not in str(e.value) and "id.secret" not in str(e.value)
