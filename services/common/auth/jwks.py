"""JWKS client: cached, size-capped, no redirects, certificate verification always on."""

import asyncio
import json
import time
from typing import Callable, Optional

import requests
from jwt.algorithms import ECAlgorithm

from common.auth.config import AuthConfig

CACHE_TTL_SECONDS = 600       # a fresh cache is used without any network call
MAX_STALE_SECONDS = 3600      # keys older than this are not used even if the fetch fails
REFETCH_MIN_INTERVAL = 30     # at most one forced refetch (unknown kid) in this window
FETCH_TIMEOUT_SECONDS = 5
FAILURE_BACKOFF_SECONDS = 5   # after a failed fetch, do not retry (and block requests) sooner
MAX_JWKS_BYTES = 64 * 1024


class JWKSUnavailable(Exception):
    """Keys could not be obtained and none are cached. Maps to HTTP 503."""


class UnknownKeyID(Exception):
    """The token names a key the catalog does not publish. Maps to HTTP 401."""


def fetch_jwks(config: AuthConfig) -> bytes:
    """GET the JWKS document. Raises JWKSUnavailable on any failure."""
    session = requests.Session()
    session.trust_env = False  # ignore HTTP(S)_PROXY and netrc: the URL is operator-set
    try:
        resp = session.get(
            config.jwks_url,
            timeout=FETCH_TIMEOUT_SECONDS,
            allow_redirects=False,
            stream=True,
            verify=config.ca_bundle or True,
        )
        with resp:
            if resp.status_code != 200:
                raise JWKSUnavailable(f"JWKS endpoint returned HTTP {resp.status_code}")
            body = b""
            for chunk in resp.iter_content(chunk_size=8192):
                body += chunk
                if len(body) > MAX_JWKS_BYTES:
                    raise JWKSUnavailable("JWKS response exceeds size limit")
            return body
    except requests.RequestException as exc:
        raise JWKSUnavailable(f"JWKS fetch failed: {type(exc).__name__}") from exc
    finally:
        session.close()


def parse_jwks(body: bytes) -> dict:
    """Return {kid: public key} for the usable ES256 keys in a JWKS document."""
    try:
        doc = json.loads(body)
        entries = doc["keys"]
        if not isinstance(entries, list):
            raise TypeError("keys is not a list")
    except (ValueError, KeyError, TypeError) as exc:
        raise JWKSUnavailable("JWKS document is malformed") from exc

    keys = {}
    for entry in entries:
        if not isinstance(entry, dict):
            continue
        kid = entry.get("kid")
        # Only P-256 signing keys; anything else (and anything private) is ignored.
        if (
            not isinstance(kid, str) or not kid
            or entry.get("kty") != "EC" or entry.get("crv") != "P-256"
            or entry.get("alg", "ES256") != "ES256"
            or "d" in entry
        ):
            continue
        try:
            keys[kid] = ECAlgorithm.from_jwk(json.dumps(entry))
        except Exception:  # noqa: BLE001 - one bad key must not hide the others
            continue
    if not keys:
        raise JWKSUnavailable("JWKS contains no usable keys")
    return keys


class JWKSClient:
    """Resolves a ``kid`` to a public key, refreshing the cache when needed."""

    def __init__(self, config: AuthConfig, fetcher: Optional[Callable[[AuthConfig], bytes]] = None):
        """Create a client; ``fetcher`` is replaceable for tests."""
        self._config = config
        self._fetcher = fetcher or fetch_jwks
        self._keys: dict = {}
        self._fetched_at = 0.0
        self._last_forced = 0.0
        self._last_failure = float("-inf")
        self._lock = asyncio.Lock()

    async def get_key(self, kid: str):
        """Return the public key for ``kid``; see the module constants for the policy."""
        now = time.monotonic()
        cache_fresh = bool(self._keys) and now - self._fetched_at < CACHE_TTL_SECONDS
        if cache_fresh and kid in self._keys:
            return self._keys[kid]

        async with self._lock:
            now = time.monotonic()
            cache_fresh = bool(self._keys) and now - self._fetched_at < CACHE_TTL_SECONDS
            if cache_fresh and kid in self._keys:
                return self._keys[kid]
            # An unknown kid forces a refetch, at most once per window, so a flood of
            # forged kids cannot turn this service into a request amplifier.
            if cache_fresh and now - self._last_forced < REFETCH_MIN_INTERVAL:
                raise UnknownKeyID()
            if cache_fresh:
                self._last_forced = now
            await self._refresh()

            if kid not in self._keys:
                raise UnknownKeyID()
            return self._keys[kid]

    async def _refresh(self) -> None:
        recently_failed = time.monotonic() - self._last_failure < FAILURE_BACKOFF_SECONDS
        try:
            if recently_failed:
                # Do not touch _last_failure here, or steady traffic would extend it forever.
                raise JWKSUnavailable("JWKS fetch failed recently")
            body = await asyncio.to_thread(self._fetcher, self._config)
            keys = parse_jwks(body)
        except JWKSUnavailable:
            if not recently_failed:
                self._last_failure = time.monotonic()
            # Keep serving known keys for a bounded time; never serve forever.
            if self._keys and time.monotonic() - self._fetched_at < MAX_STALE_SECONDS:
                return
            self._keys = {}
            raise
        self._keys = keys
        self._fetched_at = time.monotonic()

    async def check(self) -> None:
        """Raise JWKSUnavailable unless keys are available (for a readiness probe).

        Uses the cache when it is fresh, so an unauthenticated caller of the probe cannot
        drive repeated requests to the catalog.
        """
        async with self._lock:
            if self._keys and time.monotonic() - self._fetched_at < CACHE_TTL_SECONDS:
                return
            await self._refresh()
