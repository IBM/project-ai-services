"""
Unit tests for connector secret encryption.

The tests set the key on the settings object, so they need no environment variables.
"""

from unittest.mock import patch

import pytest
from cryptography.exceptions import InvalidTag
from pydantic import SecretStr

from forecast.settings import settings
from forecast.utils import encryption

pytestmark = pytest.mark.unit

KEY = "0123456789abcdef0123456789abcdef"
DETAILS = {"host": "h", "port": 8076, "database": "*LOCAL", "username": "u", "password": "secret", "ca_cert": "PEM"}


@pytest.fixture(autouse=True)
def key():
    """Use a valid DB_ENCRYPTION_KEY for every test unless the test overrides it."""
    with patch.object(settings.forecast, "db_encryption_key", SecretStr(KEY)):
        yield


def test_encrypt_secrets_keeps_only_the_allowlisted_fields_in_plaintext():
    """Host, port, database and username stay readable. Everything else is encrypted."""
    encrypted = encryption.encrypt_secrets("db2_for_i", DETAILS)
    for name in ("host", "port", "database", "username"):
        assert encrypted[name] == DETAILS[name]
    assert encrypted["password"] != "secret" and encrypted["ca_cert"] != "PEM"
    assert "secret" not in str(encrypted)


def test_decrypt_secrets_round_trips_as_strings():
    """Decrypted values come back as strings, which is why the loader converts options."""
    decrypted = encryption.decrypt_secrets("db2_for_i", encryption.encrypt_secrets("db2_for_i", DETAILS))
    assert decrypted["password"] == "secret" and decrypted["ca_cert"] == "PEM"
    assert decrypted["port"] == 8076


def test_encryption_is_randomised():
    """The same secret encrypts differently each time, so equal passwords are not visible."""
    first = encryption.encrypt_secrets("db2_for_i", DETAILS)["password"]
    second = encryption.encrypt_secrets("db2_for_i", DETAILS)["password"]
    assert first != second


def test_none_values_and_the_input_are_left_alone():
    """None values are not encrypted, and the caller's dictionary is not modified."""
    original = {"host": "h", "password": "p", "token": None}
    encrypted = encryption.encrypt_secrets("db2_for_i", original)
    assert encrypted["token"] is None
    assert original == {"host": "h", "password": "p", "token": None}
    assert encryption.decrypt_secrets("db2_for_i", encrypted)["token"] is None


def test_tampered_ciphertext_is_rejected():
    """A changed ciphertext fails authentication instead of returning garbage."""
    encrypted = encryption.encrypt_secrets("db2_for_i", DETAILS)
    token = encrypted["password"]
    encrypted["password"] = token[:-4] + ("AAAA" if token[-4:] != "AAAA" else "BBBB")
    with pytest.raises(InvalidTag):
        encryption.decrypt_secrets("db2_for_i", encrypted)


def test_another_key_cannot_decrypt():
    """Ciphertext made with one key does not decrypt with another."""
    encrypted = encryption.encrypt_secrets("db2_for_i", DETAILS)
    with patch.object(settings.forecast, "db_encryption_key", SecretStr("f" * 32)):
        with pytest.raises(InvalidTag):
            encryption.decrypt_secrets("db2_for_i", encrypted)


def test_unknown_connector_type_uses_the_db2_allowlist():
    """A connector type that is not listed falls back to the Db2 for i plaintext fields."""
    encrypted = encryption.encrypt_secrets("something_else", DETAILS)
    assert encrypted["host"] == "h" and encrypted["password"] != "secret"


@pytest.mark.parametrize("value", ["", "too-short", "x" * 33])
def test_validate_cipher_rejects_missing_or_wrong_size_keys(value):
    """The key must be exactly 32 bytes."""
    with patch.object(settings.forecast, "db_encryption_key", SecretStr(value)):
        with pytest.raises(encryption.EncryptionKeyError):
            encryption.validate_cipher()
