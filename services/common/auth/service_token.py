"""Outbound side of DA2: trade the per-deployment credential for a short-lived service token.

A service that calls another service (today: chat -> similarity) mounts the credential the
catalog gave it as a file, POSTs it to the catalog, and caches the returned token until shortly
before it expires. Certificates are always verified, redirects and proxy variables are ignored,
and the credential, the token and the catalog's response body are never logged or put in an
exception message.
"""

import json
import os
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Callable, Optional
from urllib.parse import urlparse

import requests

from common.auth.config import _LOOPBACK_HOSTS, AuthConfigError

TOKEN_PATH = "/api/v1/auth/service-credential/token"
REFRESH_MARGIN_SECONDS = 60     # a token this close to expiry is replaced before it is used
FAILURE_BACKOFF_SECONDS = 5     # after a failed exchange, fail fast instead of calling again
EXCHANGE_TIMEOUT_SECONDS = 5
MAX_RESPONSE_BYTES = 16 * 1024
MAX_CREDENTIAL_BYTES = 1024


class ServiceTokenUnavailable(Exception):
    """No token could be obtained. The caller must not send its request."""


@dataclass(frozen=True)
class ServiceCredentialConfig:
    """Where the catalog is and where the credential file is."""

    catalog_url: str
    credential_file: str
    ca_bundle: Optional[str] = None
    plaintext_hosts: frozenset = frozenset()

    @classmethod
    def from_env(cls) -> Optional["ServiceCredentialConfig"]:
        """Read the configuration; None when no credential file is configured.

        Raises AuthConfigError when a credential file is configured but the catalog URL is
        missing or unsafe, so a half-configured service fails instead of calling without a token.
        """
        credential_file = os.environ.get("AUTH_SERVICE_CREDENTIAL_FILE", "").strip()
        if not credential_file:
            return None
        catalog_url = os.environ.get("AUTH_CATALOG_API_URL", "").strip().rstrip("/")
        if not catalog_url:
            raise AuthConfigError("AUTH_CATALOG_API_URL is required with AUTH_SERVICE_CREDENTIAL_FILE")
        hosts = frozenset(
            h.strip().lower()
            for h in os.environ.get("AUTH_JWKS_PLAINTEXT_HOSTS", "").split(",")
            if h.strip()
        )
        cfg = cls(
            catalog_url=catalog_url,
            credential_file=credential_file,
            ca_bundle=os.environ.get("AUTH_CATALOG_CA_BUNDLE", "").strip() or None,
            plaintext_hosts=hosts,
        )
        cfg._validate_url()
        return cfg

    def _validate_url(self) -> None:
        u = urlparse(self.catalog_url)
        host = (u.hostname or "").lower()
        if not host or u.username or u.password:
            raise AuthConfigError("AUTH_CATALOG_API_URL must be an absolute URL without credentials")
        if u.scheme == "https":
            return
        if u.scheme == "http" and (host in _LOOPBACK_HOSTS or host in self.plaintext_hosts):
            return
        raise AuthConfigError(
            "AUTH_CATALOG_API_URL must use https; http is allowed only for loopback or a host "
            "listed in AUTH_JWKS_PLAINTEXT_HOSTS"
        )


def read_credential(config: ServiceCredentialConfig) -> str:
    """Read the credential file. It is read on every exchange so a rotated file is picked up."""
    try:
        with open(config.credential_file, "rb") as fh:
            raw = fh.read(MAX_CREDENTIAL_BYTES + 1)
    except OSError as exc:
        raise ServiceTokenUnavailable(f"credential file unreadable: {type(exc).__name__}") from exc
    credential = raw.decode("ascii", errors="replace").strip()
    if not credential or len(raw) > MAX_CREDENTIAL_BYTES:
        raise ServiceTokenUnavailable("credential file is empty or too large")
    return credential


def exchange_credential(config: ServiceCredentialConfig, audience: str) -> tuple[str, float]:
    """POST the credential to the catalog; return (token, expiry as a unix timestamp)."""
    credential = read_credential(config)
    session = requests.Session()
    session.trust_env = False  # ignore HTTP(S)_PROXY and netrc: the URL is operator-set
    try:
        resp = session.post(
            config.catalog_url + TOKEN_PATH,
            json={"credential": credential, "service": audience},
            timeout=EXCHANGE_TIMEOUT_SECONDS,
            allow_redirects=False,
            stream=True,
            verify=config.ca_bundle or True,
        )
        with resp:
            if resp.status_code != 200:
                raise ServiceTokenUnavailable(f"catalog refused the credential: HTTP {resp.status_code}")
            body = b""
            for chunk in resp.iter_content(chunk_size=4096):
                body += chunk
                if len(body) > MAX_RESPONSE_BYTES:
                    raise ServiceTokenUnavailable("catalog response exceeds size limit")
    except requests.RequestException as exc:
        raise ServiceTokenUnavailable(f"catalog unreachable: {type(exc).__name__}") from exc
    finally:
        session.close()

    try:
        doc = json.loads(body)
        token = doc["token"]
        expires_at = datetime.strptime(doc["expires_at"], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
    except (ValueError, KeyError, TypeError) as exc:
        raise ServiceTokenUnavailable("catalog response is malformed") from exc
    if not isinstance(token, str) or not token:
        raise ServiceTokenUnavailable("catalog response is malformed")
    return token, expires_at.timestamp()


class ServiceTokenClient:
    """Thread-safe token cache, one entry per audience."""

    def __init__(
        self,
        config: ServiceCredentialConfig,
        exchanger: Optional[Callable[[ServiceCredentialConfig, str], tuple[str, float]]] = None,
        clock: Callable[[], float] = time.time,
    ):
        """Create a client; ``exchanger`` and ``clock`` are replaceable for tests."""
        self._config = config
        self._exchanger = exchanger or exchange_credential
        self._clock = clock
        self._cache: dict[str, tuple[str, float]] = {}
        self._failed_at: dict[str, float] = {}
        self._lock = threading.Lock()

    def get_token(self, audience: str) -> str:
        """Return a token for ``audience``; raise ServiceTokenUnavailable if none can be had."""
        with self._lock:
            now = self._clock()
            cached = self._cache.get(audience)
            if cached and cached[1] - now > REFRESH_MARGIN_SECONDS:
                return cached[0]
            failed_at = self._failed_at.get(audience)
            if failed_at is not None and now - failed_at < FAILURE_BACKOFF_SECONDS:
                raise ServiceTokenUnavailable("token exchange failed recently")
            try:
                token, expires_at = self._exchanger(self._config, audience)
            except ServiceTokenUnavailable:
                self._failed_at[audience] = now
                # A token that has not actually expired may still be used while the catalog is down.
                if cached and cached[1] > now:
                    return cached[0]
                self._cache.pop(audience, None)
                raise
            if expires_at <= now:
                self._failed_at[audience] = now
                raise ServiceTokenUnavailable("catalog returned an expired token")
            self._cache[audience] = (token, expires_at)
            self._failed_at.pop(audience, None)
            return token


_client: Optional[ServiceTokenClient] = None
_client_loaded = False
_client_lock = threading.Lock()


def get_service_token_client() -> Optional[ServiceTokenClient]:
    """Return the process-wide client, or None when no credential file is configured."""
    global _client, _client_loaded
    with _client_lock:
        if not _client_loaded:
            config = ServiceCredentialConfig.from_env()  # raises AuthConfigError if unsafe
            _client = ServiceTokenClient(config) if config else None
            _client_loaded = True
        return _client


def set_service_token_client(client: Optional[ServiceTokenClient]) -> None:
    """Replace the process-wide client (tests); None means "not configured"."""
    global _client, _client_loaded
    with _client_lock:
        _client = client
        _client_loaded = True


def reset_service_token_client() -> None:
    """Forget the process-wide client so the next call reads the environment again (tests)."""
    global _client, _client_loaded
    with _client_lock:
        _client = None
        _client_loaded = False


def get_service_token(audience: str) -> Optional[str]:
    """Return a bearer token for ``audience``, or None when this deployment has no credential.

    None means "not configured" and lets a service still run against an open peer. When a
    credential is configured, a token is always returned or ServiceTokenUnavailable is raised.
    """
    client = get_service_token_client()
    if client is None:
        return None
    return client.get_token(audience)
