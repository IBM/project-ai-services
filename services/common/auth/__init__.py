"""Verification of catalog-issued service tokens (see docs/service-api-authentication-*.md).

The verifier side needs PyJWT. It is loaded on first use, so a service that only calls other
services (chat -> similarity) can import the outbound client without PyJWT being installed.
"""

import importlib

from common.auth.config import AuthConfig, AuthConfigError
from common.auth.service_token import ServiceTokenUnavailable, get_service_token

_LAZY = {
    "get_principal": "common.auth.dependencies",
    "require_role": "common.auth.dependencies",
    "JWKSClient": "common.auth.jwks",
    "JWKSUnavailable": "common.auth.jwks",
    "AuthMiddleware": "common.auth.middleware",
    "InvalidToken": "common.auth.verifier",
    "Principal": "common.auth.verifier",
    "TokenVerifier": "common.auth.verifier",
    "get_verifier": "common.auth.verifier",
    "set_verifier": "common.auth.verifier",
}

__all__ = [
    "AuthConfig", "AuthConfigError", "ServiceTokenUnavailable", "get_service_token", *_LAZY,
]


def __getattr__(name):
    module = _LAZY.get(name)
    if module is None:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
    value = getattr(importlib.import_module(module), name)
    globals()[name] = value
    return value
