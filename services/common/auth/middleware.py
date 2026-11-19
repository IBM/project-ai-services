"""Deny-by-default authentication as a pure ASGI middleware.

This runs before any route code, so an unauthenticated request is answered before its
body is read. A FastAPI dependency cannot do that: dependencies run after the body of a
multipart route has been parsed.
"""

import json
import logging
from typing import Callable, Iterable, Optional

from common.auth.jwks import JWKSUnavailable
from common.auth.verifier import InvalidToken, TokenVerifier, get_verifier

logger = logging.getLogger("auth")

# Exact (path) allow-list, GET/HEAD only. Everything else needs a valid token.
DEFAULT_PUBLIC_PATHS = frozenset({"/health", "/openapi.json", "/ready"})
_PUBLIC_METHODS = frozenset({"GET", "HEAD"})
MAX_TOKEN_LENGTH = 8192


class AuthMiddleware:
    """Verify the bearer token and store the Principal on ``request.state.principal``."""

    def __init__(
        self,
        app,
        verifier_provider: Callable[[], TokenVerifier] = get_verifier,
        public_paths: Iterable[str] = DEFAULT_PUBLIC_PATHS,
    ):
        """Wrap ``app``; ``verifier_provider`` is called per request (so it can be swapped)."""
        self.app = app
        self._provider = verifier_provider
        self._public = frozenset(public_paths)

    async def __call__(self, scope, receive, send):
        """ASGI entry point."""
        if scope["type"] == "lifespan":
            await self.app(scope, receive, send)
            return
        if scope["type"] != "http":
            # WebSockets and anything else: not used by this API, so refuse.
            await _reject_non_http(scope, receive, send)
            return

        if scope["method"] in _PUBLIC_METHODS and scope["path"] in self._public:
            await self.app(scope, receive, send)
            return

        token = _bearer_token(scope)
        if token is None:
            await _respond(send, 401, "AUTHENTICATION_FAILED", "Authentication required", bearer=True)
            return

        try:
            principal = await self._provider().verify(token)
        except InvalidToken as exc:
            logger.warning("rejected token: %s", exc)
            await _respond(send, 401, "AUTHENTICATION_FAILED", "Invalid credentials", bearer=True)
            return
        except JWKSUnavailable as exc:
            logger.error("cannot verify token, signing keys unavailable: %s", exc)
            await _respond(send, 503, "AUTHENTICATION_UNAVAILABLE", "Authentication is temporarily unavailable")
            return

        scope.setdefault("state", {})["principal"] = principal
        await self.app(scope, receive, send)


def _bearer_token(scope) -> Optional[str]:
    values = [v for k, v in scope["headers"] if k == b"authorization"]
    if len(values) != 1:  # none, or ambiguous
        return None
    try:
        scheme, _, token = values[0].decode("latin-1").partition(" ")
    except UnicodeDecodeError:
        return None
    token = token.strip()
    if scheme.lower() != "bearer" or not token or len(token) > MAX_TOKEN_LENGTH or " " in token:
        return None
    return token


async def _respond(send, status: int, code: str, message: str, bearer: bool = False) -> None:
    body = json.dumps({"error": {"code": code, "message": message, "status": status}}).encode()
    headers = [
        (b"content-type", b"application/json"),
        (b"content-length", str(len(body)).encode()),
        (b"cache-control", b"no-store"),
    ]
    if bearer:
        headers.append((b"www-authenticate", b"Bearer"))
    await send({"type": "http.response.start", "status": status, "headers": headers})
    await send({"type": "http.response.body", "body": body})


async def _reject_non_http(scope, receive, send) -> None:
    if scope["type"] == "websocket":
        await receive()
        await send({"type": "websocket.close", "code": 1008})
