"""Configuration for verifying catalog-issued service tokens."""

import os
from dataclasses import dataclass
from urllib.parse import urlparse

DEFAULT_ISSUER = "ai-services-catalog-server"
_LOOPBACK_HOSTS = frozenset({"localhost", "127.0.0.1", "::1"})


class AuthConfigError(RuntimeError):
    """The auth configuration is missing or unsafe. The service must not start."""


@dataclass(frozen=True)
class AuthConfig:
    """Settings for the token verifier; build it with ``AuthConfig.from_env()``."""

    jwks_url: str
    audience: str
    issuer: str = DEFAULT_ISSUER
    ca_bundle: str | None = None
    plaintext_hosts: frozenset = frozenset()

    @classmethod
    def from_env(cls) -> "AuthConfig":
        """Read and validate the configuration; raise AuthConfigError if it is unsafe."""
        jwks_url = os.environ.get("AUTH_JWKS_URL", "").strip()
        audience = os.environ.get("AUTH_AUDIENCE", "").strip()
        if not jwks_url:
            raise AuthConfigError("AUTH_JWKS_URL is required")
        if not audience:
            # No default: a copied config must not accept another service's tokens.
            raise AuthConfigError("AUTH_AUDIENCE is required")

        hosts = frozenset(
            h.strip().lower()
            for h in os.environ.get("AUTH_JWKS_PLAINTEXT_HOSTS", "").split(",")
            if h.strip()
        )
        cfg = cls(
            jwks_url=jwks_url,
            audience=audience,
            issuer=os.environ.get("AUTH_ISSUER", "").strip() or DEFAULT_ISSUER,
            ca_bundle=os.environ.get("AUTH_CATALOG_CA_BUNDLE", "").strip() or None,
            plaintext_hosts=hosts,
        )
        cfg._validate_url()
        return cfg

    def _validate_url(self) -> None:
        u = urlparse(self.jwks_url)
        host = (u.hostname or "").lower()
        if not host or u.username or u.password:
            raise AuthConfigError("AUTH_JWKS_URL must be an absolute URL without credentials")
        if u.scheme == "https":
            return
        if u.scheme == "http" and (host in _LOOPBACK_HOSTS or host in self.plaintext_hosts):
            return
        raise AuthConfigError(
            "AUTH_JWKS_URL must use https; http is allowed only for loopback or a host "
            "listed in AUTH_JWKS_PLAINTEXT_HOSTS"
        )
