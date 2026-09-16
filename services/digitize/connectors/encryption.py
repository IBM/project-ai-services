"""
Connector credential encryption/decryption.

Secret fields are encrypted at rest using AES-256-GCM with a key loaded from
the DB_ENCRYPTION_KEY environment variable (32 raw bytes, base64-encoded or
as a plain 32-character ASCII string).

When the variable is absent (e.g. in tests), operations raise RuntimeError.

Ciphertext wire format (stored as base64):
    base64( nonce[12] || tag[16] || ciphertext )

Allowlist design
----------------
Rather than enumerating secret field *names* to encrypt/strip (which silently
leaks any credential field not on the list), both protections use an allowlist
of *safe, non-secret* fields that are permitted in API responses and stored
unencrypted.  Any field not on the allowlist is:
  - encrypted before being written to the database, and
  - omitted from every API response.

Adding a new connector type: add its safe fields to ``_SAFE_FIELDS``.
Any field outside that set is treated as a secret automatically.
"""

import base64
import os
from functools import lru_cache

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

from common.misc_utils import get_logger

logger = get_logger("connector_encryption")

# Fields that are safe to store in plaintext and safe to return in API
# responses for each connector type.  Every field NOT listed here is treated
# as a secret: encrypted at rest and stripped from responses.
_SAFE_FIELDS: dict[str, frozenset[str]] = {
    "file_system": frozenset({"host", "port", "username", "remote_path", "allowed_extensions"}),
    "object_storage": frozenset({"bucket_name", "access_key_id", "endpoint_url",
                                 "prefix", "delimiter", "download_concurrency",
                                 "verify_ssl", "allowed_extensions"}),
}

_NONCE_SIZE = 12  # 96-bit nonce recommended for GCM


@lru_cache(maxsize=1)
def _load_key() -> AESGCM:
    """Load and cache the AES-256-GCM cipher from the DB_ENCRYPTION_KEY env var."""
    raw = os.environ.get("DB_ENCRYPTION_KEY", "")
    if not raw:
        raise RuntimeError(
            "Connector encryption key not found. "
            "Ensure the DB_ENCRYPTION_KEY environment variable is set before starting the service."
        )
    key_bytes = raw.encode()
    if len(key_bytes) != 32:
        raise RuntimeError(
            f"DB_ENCRYPTION_KEY must be exactly 32 bytes (AES-256); got {len(key_bytes)} bytes."
        )
    return AESGCM(key_bytes)


def _get_cipher() -> AESGCM:
    return _load_key()


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
) -> dict:
    """
    Return a copy of *connection_details* with all non-safe fields encrypted.

    Fields listed in ``_SAFE_FIELDS`` for the connector type are stored
    verbatim; every other field is AES-256-GCM encrypted before storage.
    """
    cipher = _get_cipher()
    safe = _SAFE_FIELDS.get(connector_type, frozenset())
    result = dict(connection_details)
    for field, value in connection_details.items():
        if field not in safe and value is not None:
            result[field] = _encrypt_value(cipher, str(value))
    return result


def decrypt_secrets(
    connector_type: str,
    connection_details: dict,
) -> dict:
    """
    Return a copy of *connection_details* with all non-safe fields decrypted.
    """
    cipher = _get_cipher()
    safe = _SAFE_FIELDS.get(connector_type, frozenset())
    result = dict(connection_details)
    for field, value in connection_details.items():
        if field not in safe and value is not None:
            try:
                result[field] = _decrypt_value(cipher, value)
            except Exception as exc:
                logger.error(
                    f"Failed to decrypt field {field!r} for connector type {connector_type!r}: {exc}",
                    exc_info=True,
                )
                raise
    return result


def safe_connection_details(connector_type: str, connection_details: dict) -> dict:
    """
    Return a copy of *connection_details* containing only the fields that are
    safe to expose in an API response (i.e. those on the allowlist).

    Any field not explicitly listed in ``_SAFE_FIELDS`` for the connector type
    is omitted — including credentials that may have been stored by older
    versions of this service.  For unknown connector types every field is
    omitted, which is the safe default.
    """
    safe = _SAFE_FIELDS.get(connector_type, frozenset())
    return {k: v for k, v in connection_details.items() if k in safe}


def merge_and_encrypt_partial(
    connector_type: str,
    existing_encrypted: dict,
    partial_update: dict,
) -> dict:
    """
    Merge *partial_update* into *existing_encrypted* at the key level,
    re-encrypting any non-safe fields found in *partial_update*.

    Keys absent from *partial_update* are preserved from *existing_encrypted*
    as-is (already encrypted). Only the supplied keys are overwritten.

    Returns the merged dict (all non-safe fields encrypted).
    """
    cipher = _get_cipher()
    safe = _SAFE_FIELDS.get(connector_type, frozenset())
    result = dict(existing_encrypted)
    for key, value in partial_update.items():
        if key not in safe and value is not None:
            result[key] = _encrypt_value(cipher, str(value))
        else:
            result[key] = value
    return result

# Made with Bob
