"""
Encryption for connector secrets.

The key is read from DB_ENCRYPTION_KEY, as Digitize does, and must be exactly
32 bytes (AES-256-GCM). The value is used as it is: it is not base64-decoded.
``validate_cipher()`` runs in the application lifespan, so a missing or invalid
key stops the service before it serves requests.

Every field of a connector's connection details that is not in ``SAFE_FIELDS``
is a secret: it is encrypted before it is stored and never returned.
Ciphertext wire format (stored as base64): base64(nonce[12] || ciphertext || tag[16]).
"""
import base64
import os

from cryptography.hazmat.primitives.ciphers.aead import AESGCM

from forecast.settings import settings

# Fields stored in plaintext and returned in responses. db2_for_i is the only
# connector type; a new type adds its safe fields here.
SAFE_FIELDS = frozenset({"host", "port", "database", "username"})

_NONCE_SIZE = 12  # 96-bit nonce recommended for GCM


class EncryptionKeyError(RuntimeError):
    """Raised when DB_ENCRYPTION_KEY is missing or is not 32 bytes."""


class DecryptionError(RuntimeError):
    """Raised when a secret token cannot be decrypted (e.g. invalid tag, corrupted data, wrong key)."""


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


def _encrypt_value(cipher: AESGCM, plaintext: str) -> str:
    nonce = os.urandom(_NONCE_SIZE)
    # AESGCM.encrypt returns ciphertext + tag (tag appended)
    return base64.b64encode(nonce + cipher.encrypt(nonce, plaintext.encode(), None)).decode()


def _decrypt_value(cipher: AESGCM, token: str) -> str:
    try:
        raw = base64.b64decode(token.encode())
        return cipher.decrypt(raw[:_NONCE_SIZE], raw[_NONCE_SIZE:], None).decode()
    except Exception as exc:
        raise DecryptionError("Failed to decrypt secret token.") from exc


def encrypt_secrets(connection_details: dict) -> dict:
    """Return a copy of *connection_details* with every secret field encrypted."""
    cipher = validate_cipher()
    return {
        field: value if field in SAFE_FIELDS or value is None else _encrypt_value(cipher, str(value))
        for field, value in connection_details.items()
    }


def decrypt_secrets(connection_details: dict) -> dict:
    """Return a copy of *connection_details* with every secret field decrypted."""
    cipher = validate_cipher()
    return {
        field: value if field in SAFE_FIELDS or value is None else _decrypt_value(cipher, value)
        for field, value in connection_details.items()
    }


def safe_connection_details(connection_details: dict) -> dict:
    """Return only the fields that may be shown in a response."""
    return {field: value for field, value in connection_details.items() if field in SAFE_FIELDS}
