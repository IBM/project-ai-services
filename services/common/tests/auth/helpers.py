"""Token and JWKS builders shared by the auth tests."""

import json
import time

import jwt
from cryptography.hazmat.primitives.asymmetric import ec, rsa
from cryptography.hazmat.primitives import serialization
from jwt.algorithms import ECAlgorithm

from common.auth.config import AuthConfig

AUDIENCE = "digitize"
ISSUER = "ai-services-catalog-server"


def make_config(**kw) -> AuthConfig:
    """AuthConfig for tests."""
    base = dict(jwks_url="https://catalog.example/.well-known/jwks.json", audience=AUDIENCE)
    base.update(kw)
    return AuthConfig(**base)


class Signer:
    """An ES256 key pair that can publish a JWKS and sign tokens."""

    def __init__(self, kid="k1"):
        """Generate a fresh P-256 key."""
        self.kid = kid
        self.key = ec.generate_private_key(ec.SECP256R1())

    def jwk(self) -> dict:
        """Public JWK."""
        jwk = json.loads(ECAlgorithm.to_jwk(self.key.public_key()))
        jwk.update({"kid": self.kid, "use": "sig", "alg": "ES256"})
        return jwk

    def claims(self, **over) -> dict:
        """Valid claims, overridable. A value of None removes the claim."""
        now = int(time.time())
        c = {
            "iss": ISSUER, "aud": [AUDIENCE], "sub": "uid_1", "iat": now, "nbf": now,
            "exp": now + 900, "jti": "abc", "scope": f"{AUDIENCE}:admin", "idp": "local", "typ": "user",
        }
        c.update(over)
        return {k: v for k, v in c.items() if v is not None}

    def token(self, **over) -> str:
        """Signed token."""
        return jwt.encode(self.claims(**over), self.key, algorithm="ES256", headers={"kid": self.kid})


def jwks_body(*signers) -> bytes:
    """JWKS document for the given signers."""
    return json.dumps({"keys": [s.jwk() for s in signers]}).encode()


class CountingFetcher:
    """A fetcher that returns a body (or raises) and counts calls."""

    def __init__(self, body=None, exc=None):
        """Set the response."""
        self.body, self.exc, self.calls = body, exc, 0

    def __call__(self, config):
        """Fetch."""
        self.calls += 1
        if self.exc:
            raise self.exc
        return self.body


def rsa_token(kid="k1") -> str:
    """A validly RS256-signed token, to prove the verifier rejects the algorithm."""
    key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    pem = key.private_bytes(
        serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()
    )
    return jwt.encode(Signer().claims(), pem, algorithm="RS256", headers={"kid": kid})
