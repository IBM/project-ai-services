"""Verification of catalog-issued service tokens (see docs/service-api-authentication-*.md)."""

from common.auth.config import AuthConfig, AuthConfigError
from common.auth.dependencies import get_principal, require_role
from common.auth.jwks import JWKSClient, JWKSUnavailable
from common.auth.middleware import AuthMiddleware
from common.auth.verifier import InvalidToken, Principal, TokenVerifier, get_verifier, set_verifier

__all__ = [
    "AuthConfig", "AuthConfigError", "AuthMiddleware", "InvalidToken", "JWKSClient",
    "JWKSUnavailable", "Principal", "TokenVerifier", "get_principal", "get_verifier",
    "require_role", "set_verifier",
]
