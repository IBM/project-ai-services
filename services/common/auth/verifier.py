"""Verification of catalog-issued ES256 service tokens."""

from dataclasses import dataclass
from typing import Optional

import jwt

from common.auth.config import AuthConfig
from common.auth.jwks import JWKSClient, UnknownKeyID

ALGORITHM = "ES256"
# Role hierarchy (DA1): each role also grants the roles listed here.
_IMPLIED_ROLES = {
    "admin": frozenset({"write", "read"}),
    "write": frozenset({"read"}),
}
LEEWAY_SECONDS = 30
_REQUIRED_CLAIMS = ["exp", "nbf", "iat", "iss", "aud", "sub", "jti", "scope"]


class InvalidToken(Exception):
    """The token is missing, malformed, unsigned, expired or for another audience."""


@dataclass(frozen=True)
class Principal:
    """The verified caller. Read-only; for authorization and logging, never for secrets."""

    subject: str
    idp: str
    kind: str
    audience: str
    roles: frozenset
    token_id: str

    def has_role(self, role: str) -> bool:
        """True if the token grants this role, directly or through the hierarchy.

        ``admin`` implies ``write`` and ``read``; ``write`` implies ``read``.
        """
        if role in self.roles:
            return True
        return any(role in _IMPLIED_ROLES.get(held, ()) for held in self.roles)


class TokenVerifier:
    """Verifies a bearer token against the catalog's published keys."""

    def __init__(self, config: AuthConfig, jwks: Optional[JWKSClient] = None):
        """Create a verifier for one audience (the service name)."""
        self.config = config
        self.jwks = jwks or JWKSClient(config)

    async def verify(self, token: str) -> Principal:
        """Return the Principal, or raise InvalidToken / JWKSUnavailable."""
        try:
            header = jwt.get_unverified_header(token)
        except jwt.PyJWTError as exc:
            raise InvalidToken("malformed token") from exc
        # Reject before any key lookup: never let the token choose the algorithm.
        if header.get("alg") != ALGORITHM:
            raise InvalidToken("unexpected algorithm")
        kid = header.get("kid")
        if not isinstance(kid, str) or not kid:
            raise InvalidToken("missing kid")

        try:
            key = await self.jwks.get_key(kid)
        except UnknownKeyID as exc:
            raise InvalidToken("unknown kid") from exc

        try:
            claims = jwt.decode(
                token,
                key,
                algorithms=[ALGORITHM],
                audience=self.config.audience,
                issuer=self.config.issuer,
                leeway=LEEWAY_SECONDS,
                options={"require": _REQUIRED_CLAIMS},
            )
        except jwt.PyJWTError as exc:
            raise InvalidToken(type(exc).__name__) from exc

        scope = claims.get("scope")
        if not isinstance(scope, str):
            raise InvalidToken("bad scope")
        prefix = self.config.audience + ":"
        # Scopes for other services are ignored.
        roles = frozenset(s[len(prefix):] for s in scope.split() if s.startswith(prefix))
        return Principal(
            subject=claims["sub"],
            idp=str(claims.get("idp", "")),
            kind=str(claims.get("typ", "")),
            audience=self.config.audience,
            roles=roles,
            token_id=claims["jti"],
        )


_verifier: Optional[TokenVerifier] = None


def get_verifier() -> TokenVerifier:
    """Return the process-wide verifier, building it from the environment on first use.

    Raises AuthConfigError if the configuration is missing or unsafe.
    """
    global _verifier
    if _verifier is None:
        _verifier = TokenVerifier(AuthConfig.from_env())
    return _verifier


def set_verifier(verifier: Optional[TokenVerifier]) -> None:
    """Replace the process-wide verifier (tests)."""
    global _verifier
    _verifier = verifier
