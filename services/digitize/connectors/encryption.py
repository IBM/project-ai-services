"""
Connector credential encryption/decryption.

Secret fields are encrypted at rest using AES-256-GCM with a key loaded from
the path configured in settings (default: /run/secrets/connector_encryption_key).

The key file must contain exactly 32 raw bytes (256 bits).
When the key file is absent (e.g. in tests), operations raise RuntimeError.

Ciphertext wire format (stored as base64):
    base64( nonce[12] || tag[16] || ciphertext )
"""

import base64
import os
from functools import lru_cache
from pathlib import Path

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

from common.misc_utils import get_logger

logger = get_logger("connector_encryption")

# Secret fields per connector type that must be encrypted before storage
# and stripped before API responses.
_SECRET_FIELDS: dict[str, set[str]] = {
    "ssh": {"private_key"},
    "s3": {"secret_access_key"},
}

_NONCE_SIZE = 12  # 96-bit nonce recommended for GCM


@lru_cache(maxsize=1)
def _load_key(key_path: str) -> AESGCM:
    """Load and cache the AES-256-GCM cipher from the key file."""
    path = Path(key_path)
    if not path.exists():
        raise RuntimeError(
            f"Connector encryption key not found at {key_path}. "
            "Ensure the secret is mounted before starting the service."
        )
    raw = path.read_bytes().strip()
    if len(raw) != 32:
        raise RuntimeError(
            f"Connector encryption key at {key_path} must be exactly 32 bytes "
            f"(AES-256); got {len(raw)} bytes."
        )
    return AESGCM(raw)


def _get_cipher(key_path: str) -> AESGCM:
    return _load_key(key_path)


def _encrypt_value(cipher: AESGCM, plaintext: str) -> str:
    """Encrypt a plaintext string; return base64(nonce || tag || ciphertext)."""
    nonce = os.urandom(_NONCE_SIZE)
    # AESGCM.encrypt returns ciphertext + tag (tag appended)
    ct_and_tag = cipher.encrypt(nonce, plaintext.encode(), None)
    return base64.b64encode(nonce + ct_and_tag).decode()


def _decrypt_value(cipher: AESGCM, token: str) -> str:
    """Decrypt a base64(nonce || tag || ciphertext) token; return plaintext string."""
    raw = base64.b64decode(token.encode())
    nonce = raw[:_NONCE_SIZE]
    ct_and_tag = raw[_NONCE_SIZE:]
    return cipher.decrypt(nonce, ct_and_tag, None).decode()


def encrypt_secrets(
    connector_type: str,
    connection_details: dict,
    key_path: str,
) -> dict:
    """
    Return a copy of *connection_details* with secret fields encrypted.

    Only the fields listed in _SECRET_FIELDS for the given connector type
    are touched; all other fields are passed through unchanged.
    """
    cipher = _get_cipher(key_path)
    secret_fields = _SECRET_FIELDS.get(connector_type, set())
    result = dict(connection_details)
    for field in secret_fields:
        if field in result and result[field] is not None:
            result[field] = _encrypt_value(cipher, str(result[field]))
    return result


def decrypt_secrets(
    connector_type: str,
    connection_details: dict,
    key_path: str,
) -> dict:
    """
    Return a copy of *connection_details* with secret fields decrypted.
    """
    cipher = _get_cipher(key_path)
    secret_fields = _SECRET_FIELDS.get(connector_type, set())
    result = dict(connection_details)
    for field in secret_fields:
        if field in result and result[field] is not None:
            try:
                result[field] = _decrypt_value(cipher, result[field])
            except Exception as exc:
                logger.error(f"Failed to decrypt field {field!r}: {exc}")
                raise
    return result


def strip_secrets(connector_type: str, connection_details: dict) -> dict:
    """
    Return a copy of *connection_details* with all secret fields removed.

    Used for safe API responses — ensures private keys / secrets are never
    returned to callers.
    """
    secret_fields = _SECRET_FIELDS.get(connector_type, set())
    return {k: v for k, v in connection_details.items() if k not in secret_fields}


def merge_and_encrypt_partial(
    connector_type: str,
    existing_encrypted: dict,
    partial_update: dict,
    key_path: str,
) -> dict:
    """
    Merge *partial_update* into *existing_encrypted* at the key level,
    re-encrypting any secret fields found in *partial_update*.

    Keys absent from *partial_update* are preserved from *existing_encrypted*
    as-is (already encrypted). Only the supplied keys are overwritten.

    Returns the merged dict (all secret fields encrypted).
    """
    cipher = _get_cipher(key_path)
    secret_fields = _SECRET_FIELDS.get(connector_type, set())
    result = dict(existing_encrypted)
    for key, value in partial_update.items():
        if key in secret_fields and value is not None:
            result[key] = _encrypt_value(cipher, str(value))
        else:
            result[key] = value
    return result

# Made with Bob
