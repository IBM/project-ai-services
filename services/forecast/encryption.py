"""
Encryption key for connector secrets.

The key is read from DB_ENCRYPTION_KEY, as Digitize does, and must be exactly
32 bytes (AES-256-GCM). The value is used as it is: it is not base64-decoded.
Without a valid key every operation that needs the cipher raises
``EncryptionKeyError``, so a connector cannot be created.
"""
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

from forecast.settings import settings


class EncryptionKeyError(RuntimeError):
    """Raised when DB_ENCRYPTION_KEY is missing or is not 32 bytes."""


def validate_cipher() -> AESGCM:
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
