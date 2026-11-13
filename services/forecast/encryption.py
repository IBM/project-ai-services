"""
Encryption key and credential encryption/decryption for connector secrets.

Secret fields are encrypted at rest using AES-256-GCM with a key loaded from
DB_ENCRYPTION_KEY (32 raw ASCII characters / bytes).

Ciphertext wire format (stored as base64):
    base64( nonce[12] || tag[16] || ciphertext )

Allowlist design:
Only safe, non-secret fields (host, port, database, username) are stored in plaintext.
Every other field (password, tokens, etc.) is encrypted and never leaked in error logs.
"""
from __future__ import annotations

import base64
import os
from typing import Any, Dict

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

from forecast.settings import settings

_NONCE_SIZE = 12  # 96-bit nonce for GCM

# Allowlist of safe fields per connector type
_SAFE_FIELDS: dict[str, frozenset[str]] = {
    "db2_for_i": frozenset({"host", "port", "database", "username"}),
    "db2i": frozenset({"host", "port", "database", "username"}),
}


class EncryptionKeyError(RuntimeError):
    """Raised when DB_ENCRYPTION_KEY is missing or is not 32 bytes."""


def get_cipher() -> AESGCM:
    """Return the AES-256-GCM cipher built from DB_ENCRYPTION_KEY."""
    raw = settings.forecast.db_encryption_key.get_secret_value()
    if not raw:
        raise EncryptionKeyError(
            "Connector encryption key not found. "
            "Ensure the DB_ENCRYPTION_KEY environment variable is set before starting the service."
        )
    key_bytes = raw.encode()
    if len(key_bytes) != 32:
        raise EncryptionKeyError(
            f"DB_ENCRYPTION_KEY must be exactly 32 bytes (AES-256); got {len(key_bytes)} bytes."
        )
    return AESGCM(key_bytes)


def encrypt_value(cipher: AESGCM, plaintext: str) -> str:
    """Encrypt a plaintext string; returns base64(nonce || tag || ciphertext)."""
    nonce = os.urandom(_NONCE_SIZE)
    ct_and_tag = cipher.encrypt(nonce, plaintext.encode("utf-8"), None)
    return base64.b64encode(nonce + ct_and_tag).decode("ascii")


def decrypt_value(cipher: AESGCM, token: str) -> str:
    """Decrypt a base64(nonce || tag || ciphertext) token; returns plaintext string."""
    raw = base64.b64decode(token.encode("ascii"))
    nonce = raw[:_NONCE_SIZE]
    ct_and_tag = raw[_NONCE_SIZE:]
    return cipher.decrypt(nonce, ct_and_tag, None).decode("utf-8")


def encrypt_secrets(connector_type: str, connection_details: Dict[str, Any]) -> Dict[str, Any]:
    """Return a copy of connection_details with all non-safe fields encrypted."""
    cipher = get_cipher()
    safe = _SAFE_FIELDS.get(connector_type, frozenset(_SAFE_FIELDS["db2_for_i"]))
    result = dict(connection_details)
    for field, value in connection_details.items():
        if field not in safe and value is not None:
            result[field] = encrypt_value(cipher, str(value))
    return result


def decrypt_secrets(connector_type: str, connection_details: Dict[str, Any]) -> Dict[str, Any]:
    """Return a copy of connection_details with all non-safe fields decrypted."""
    cipher = get_cipher()
    safe = _SAFE_FIELDS.get(connector_type, frozenset(_SAFE_FIELDS["db2_for_i"]))
    result = dict(connection_details)
    for field, value in connection_details.items():
        if field not in safe and value is not None:
            result[field] = decrypt_value(cipher, str(value))
    return result
