"""AuthConfig: fail closed on missing or unsafe settings."""

import pytest

from common.auth.config import AuthConfig, AuthConfigError


def _env(monkeypatch, **kw):
    for k in ("AUTH_JWKS_URL", "AUTH_AUDIENCE", "AUTH_ISSUER", "AUTH_JWKS_PLAINTEXT_HOSTS", "AUTH_CATALOG_CA_BUNDLE"):
        monkeypatch.delenv(k, raising=False)
    for k, v in kw.items():
        monkeypatch.setenv(k, v)


def test_missing_url_stops_startup(monkeypatch):
    _env(monkeypatch, AUTH_AUDIENCE="digitize")
    with pytest.raises(AuthConfigError):
        AuthConfig.from_env()


def test_missing_audience_stops_startup(monkeypatch):
    _env(monkeypatch, AUTH_JWKS_URL="https://c/.well-known/jwks.json")
    with pytest.raises(AuthConfigError):
        AuthConfig.from_env()


def test_defaults(monkeypatch):
    _env(monkeypatch, AUTH_JWKS_URL="https://c/.well-known/jwks.json", AUTH_AUDIENCE="digitize")
    cfg = AuthConfig.from_env()
    assert cfg.issuer == "ai-services-catalog-server" and cfg.ca_bundle is None


@pytest.mark.parametrize("url", [
    "http://catalog.example/jwks",
    "ftp://x/jwks",
    "https://user:pw@c/jwks",
    "/relative",
    "https:///nohost",
])
def test_unsafe_urls_rejected(monkeypatch, url):
    _env(monkeypatch, AUTH_JWKS_URL=url, AUTH_AUDIENCE="digitize")
    with pytest.raises(AuthConfigError):
        AuthConfig.from_env()


@pytest.mark.parametrize("url", ["http://localhost:8080/jwks", "http://127.0.0.1/jwks"])
def test_plaintext_allowed_for_loopback(monkeypatch, url):
    _env(monkeypatch, AUTH_JWKS_URL=url, AUTH_AUDIENCE="digitize")
    AuthConfig.from_env()


def test_plaintext_allowed_only_for_listed_host(monkeypatch):
    _env(monkeypatch, AUTH_JWKS_URL="http://catalog-api.ns.svc:8080/x", AUTH_AUDIENCE="digitize",
         AUTH_JWKS_PLAINTEXT_HOSTS="catalog-api.ns.svc")
    AuthConfig.from_env()
    _env(monkeypatch, AUTH_JWKS_URL="http://other.ns.svc:8080/x", AUTH_AUDIENCE="digitize",
         AUTH_JWKS_PLAINTEXT_HOSTS="catalog-api.ns.svc")
    with pytest.raises(AuthConfigError):
        AuthConfig.from_env()
