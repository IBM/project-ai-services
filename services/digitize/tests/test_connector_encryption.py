"""
Unit tests for services/digitize/connectors/encryption.py

Coverage
--------
_load_key
  - raises RuntimeError when DB_ENCRYPTION_KEY env var is absent
  - raises RuntimeError when key has wrong byte length
  - returns an AESGCM instance when key is valid 32 bytes

_encrypt_value / _decrypt_value (round-trip)
  - encrypt then decrypt returns original plaintext
  - encrypted value differs from plaintext (not stored in clear)
  - different calls produce different ciphertext (random nonce)

encrypt_secrets
  - encrypts non-safe fields and leaves safe fields untouched (file_system)
  - encrypts non-safe fields and leaves safe fields untouched (object_storage)
  - encrypts ALL fields for unknown connector types (closed-by-default)
  - skips None-valued non-safe fields
  - returns a copy — does not mutate the original dict

decrypt_secrets
  - decrypts values produced by encrypt_secrets (file_system)
  - decrypts values produced by encrypt_secrets (object_storage)
  - leaves safe fields untouched
  - re-raises on invalid ciphertext
  - decrypts ALL non-safe fields for unknown connector types

safe_connection_details
  - returns only safe (allowlisted) fields for file_system connector
  - returns only safe (allowlisted) fields for object_storage connector
  - returns empty dict for unknown connector type (closed-by-default)
  - does not return private_key
  - does not return secret_access_key
  - does not mutate the original dict

merge_and_encrypt_partial
  - safe keys in partial_update are copied verbatim
  - non-safe keys in partial_update are re-encrypted
  - keys absent from partial_update are preserved from existing_encrypted
  - returns a new dict (does not mutate existing_encrypted)
"""

from __future__ import annotations

import os
from unittest.mock import patch

import pytest

from digitize.connectors.encryption import (
    _decrypt_value,
    _encrypt_value,
    _get_cipher,
    _load_key,
    decrypt_secrets,
    encrypt_secrets,
    merge_and_encrypt_partial,
    safe_connection_details,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Fixed 32-byte key expressed as a 32-character ASCII string (no whitespace).
# bytes 0x41–0x60 → "ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`"
_FIXED_KEY = bytes(range(0x41, 0x61)).decode()  # 32-char ASCII string


def _set_key(monkeypatch, key: str = _FIXED_KEY):
    """Set DB_ENCRYPTION_KEY in the environment and clear the lru_cache."""
    monkeypatch.setenv("DB_ENCRYPTION_KEY", key)
    _load_key.cache_clear()


# ---------------------------------------------------------------------------
# _load_key
# ---------------------------------------------------------------------------

class TestLoadKey:
    def test_raises_when_env_var_absent(self, monkeypatch):
        monkeypatch.delenv("DB_ENCRYPTION_KEY", raising=False)
        _load_key.cache_clear()
        with pytest.raises(RuntimeError, match="not found"):
            _load_key()

    def test_raises_when_key_wrong_length_short(self, monkeypatch):
        monkeypatch.setenv("DB_ENCRYPTION_KEY", "short_key_16byte")  # 16 chars
        _load_key.cache_clear()
        with pytest.raises(RuntimeError, match="32 bytes"):
            _load_key()

    def test_raises_when_key_too_long(self, monkeypatch):
        monkeypatch.setenv("DB_ENCRYPTION_KEY", "A" * 64)  # 64 chars
        _load_key.cache_clear()
        with pytest.raises(RuntimeError, match="32 bytes"):
            _load_key()

    def test_returns_aesgcm_for_valid_32_byte_key(self, monkeypatch):
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
        _set_key(monkeypatch)
        cipher = _load_key()
        assert isinstance(cipher, AESGCM)


# ---------------------------------------------------------------------------
# _encrypt_value / _decrypt_value round-trip
# ---------------------------------------------------------------------------

class TestEncryptDecryptRoundTrip:
    def _cipher(self, monkeypatch):
        _set_key(monkeypatch)
        return _get_cipher()

    def test_roundtrip_returns_original_plaintext(self, monkeypatch):
        cipher = self._cipher(monkeypatch)
        plaintext = "super_secret_private_key_data"
        token = _encrypt_value(cipher, plaintext)
        recovered = _decrypt_value(cipher, token)
        assert recovered == plaintext

    def test_encrypted_value_differs_from_plaintext(self, monkeypatch):
        cipher = self._cipher(monkeypatch)
        plaintext = "my_secret"
        token = _encrypt_value(cipher, plaintext)
        assert token != plaintext

    def test_two_encryptions_produce_different_ciphertext(self, monkeypatch):
        """Random nonce → each call produces a unique token."""
        cipher = self._cipher(monkeypatch)
        plaintext = "same_value"
        token1 = _encrypt_value(cipher, plaintext)
        token2 = _encrypt_value(cipher, plaintext)
        assert token1 != token2

    def test_empty_string_roundtrip(self, monkeypatch):
        cipher = self._cipher(monkeypatch)
        token = _encrypt_value(cipher, "")
        assert _decrypt_value(cipher, token) == ""

    def test_unicode_roundtrip(self, monkeypatch):
        cipher = self._cipher(monkeypatch)
        plaintext = "日本語テスト🔑"
        token = _encrypt_value(cipher, plaintext)
        assert _decrypt_value(cipher, token) == plaintext


# ---------------------------------------------------------------------------
# encrypt_secrets
# ---------------------------------------------------------------------------

class TestEncryptSecrets:
    def test_encrypts_private_key_leaves_safe_fields(self, monkeypatch):
        """private_key is not in the safe allowlist — it must be encrypted."""
        _set_key(monkeypatch)
        details = {"host": "example.com", "username": "user", "private_key": "MY_PRIVATE_KEY"}
        encrypted = encrypt_secrets("file_system", details)
        assert encrypted["private_key"] != "MY_PRIVATE_KEY"
        # Safe fields pass through unchanged.
        assert encrypted["host"] == "example.com"
        assert encrypted["username"] == "user"

    def test_encrypts_s3_secret_access_key_leaves_safe_fields(self, monkeypatch):
        """secret_access_key is not in the safe allowlist — it must be encrypted."""
        _set_key(monkeypatch)
        details = {
            "bucket_name": "my-bucket",
            "access_key_id": "AKID",
            "secret_access_key": "MY_SECRET",
        }
        encrypted = encrypt_secrets("object_storage", details)
        assert encrypted["secret_access_key"] != "MY_SECRET"
        # access_key_id and bucket_name are safe — stored plaintext.
        assert encrypted["bucket_name"] == "my-bucket"
        assert encrypted["access_key_id"] == "AKID"

    def test_unknown_connector_type_encrypts_all_fields(self, monkeypatch):
        """Unknown type → no safe allowlist → every field is encrypted (closed-by-default)."""
        _set_key(monkeypatch)
        details = {"token": "bearer_token", "endpoint": "https://api.example.com"}
        result = encrypt_secrets("ftp", details)
        assert result["token"] != "bearer_token"
        assert result["endpoint"] != "https://api.example.com"

    def test_skips_none_valued_non_safe_fields(self, monkeypatch):
        _set_key(monkeypatch)
        details = {"private_key": None, "username": "admin"}
        result = encrypt_secrets("file_system", details)
        assert result["private_key"] is None
        assert result["username"] == "admin"

    def test_does_not_mutate_original_dict(self, monkeypatch):
        _set_key(monkeypatch)
        original = {"private_key": "secret", "host": "ssh.example.com"}
        _ = encrypt_secrets("file_system", original)
        assert original["private_key"] == "secret"


# ---------------------------------------------------------------------------
# decrypt_secrets
# ---------------------------------------------------------------------------

class TestDecryptSecrets:
    def test_file_system_roundtrip(self, monkeypatch):
        _set_key(monkeypatch)
        details = {
            "host": "sftp.example.com",
            "username": "user",
            "private_key": "-----BEGIN RSA PRIVATE KEY-----\nFAKE\n-----END RSA PRIVATE KEY-----",
        }
        encrypted = encrypt_secrets("file_system", details)
        decrypted = decrypt_secrets("file_system", encrypted)
        assert decrypted == details

    def test_leaves_safe_fields_untouched(self, monkeypatch):
        _set_key(monkeypatch)
        details = {"host": "sftp.example.com", "private_key": "KEY_DATA", "port": 22}
        encrypted = encrypt_secrets("file_system", details)
        decrypted = decrypt_secrets("file_system", encrypted)
        assert decrypted["host"] == "sftp.example.com"
        assert decrypted["port"] == 22

    def test_raises_on_tampered_ciphertext(self, monkeypatch):
        _set_key(monkeypatch)
        bad_details = {"private_key": "not_valid_base64_ciphertext=="}
        with pytest.raises(Exception):
            decrypt_secrets("file_system", bad_details)

    def test_skips_none_valued_non_safe_fields(self, monkeypatch):
        _set_key(monkeypatch)
        details = {"private_key": None, "host": "host.example.com"}
        result = decrypt_secrets("file_system", details)
        assert result["private_key"] is None

    def test_s3_full_roundtrip(self, monkeypatch):
        _set_key(monkeypatch)
        details = {
            "bucket_name": "my-bucket",
            "access_key_id": "AKID123",
            "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
        }
        encrypted = encrypt_secrets("object_storage", details)
        decrypted = decrypt_secrets("object_storage", encrypted)
        assert decrypted == details

    def test_unknown_type_decrypts_all_fields(self, monkeypatch):
        """Unknown connector type: everything was encrypted, everything is decrypted."""
        _set_key(monkeypatch)
        details = {"token": "bearer_token"}
        encrypted = encrypt_secrets("ftp", details)
        assert encrypted["token"] != "bearer_token"
        decrypted = decrypt_secrets("ftp", encrypted)
        assert decrypted["token"] == "bearer_token"


# ---------------------------------------------------------------------------
# safe_connection_details
# ---------------------------------------------------------------------------

class TestSafeConnectionDetails:
    def test_file_system_returns_only_safe_fields(self):
        details = {
            "host": "sftp.example.com",
            "port": 22,
            "username": "sync_user",
            "remote_path": "/exports",
            "private_key": "SENSITIVE_KEY",
        }
        result = safe_connection_details("file_system", details)
        assert result == {
            "host": "sftp.example.com",
            "port": 22,
            "username": "sync_user",
            "remote_path": "/exports",
        }
        assert "private_key" not in result

    def test_object_storage_returns_only_safe_fields(self):
        details = {
            "bucket_name": "my-bucket",
            "access_key_id": "AKID",
            "secret_access_key": "VERY_SECRET",
            "endpoint_url": "https://s3.amazonaws.com",
        }
        result = safe_connection_details("object_storage", details)
        assert result["bucket_name"] == "my-bucket"
        assert result["access_key_id"] == "AKID"
        assert result["endpoint_url"] == "https://s3.amazonaws.com"
        assert "secret_access_key" not in result

    def test_unknown_connector_type_returns_empty_dict(self):
        """Unknown type has no safe allowlist → nothing is returned (closed-by-default)."""
        details = {"token": "tok", "endpoint": "https://api.example.com"}
        result = safe_connection_details("ftp", details)
        assert result == {}

    def test_does_not_return_private_key(self):
        details = {"host": "h", "private_key": "SENSITIVE"}
        assert "private_key" not in safe_connection_details("file_system", details)

    def test_does_not_return_secret_access_key(self):
        details = {"bucket_name": "b", "secret_access_key": "SENSITIVE"}
        assert "secret_access_key" not in safe_connection_details("object_storage", details)

    def test_does_not_mutate_original_dict(self):
        original = {"host": "h", "private_key": "KEY"}
        safe_connection_details("file_system", original)
        assert "private_key" in original

    def test_field_absent_in_details_is_no_op(self):
        """Must not raise when an allowlisted field is simply absent from the dict."""
        details = {"host": "sftp.example.com", "username": "bob"}
        result = safe_connection_details("file_system", details)
        assert result == {"host": "sftp.example.com", "username": "bob"}


# ---------------------------------------------------------------------------
# merge_and_encrypt_partial
# ---------------------------------------------------------------------------

class TestMergeAndEncryptPartial:
    def test_safe_keys_copied_verbatim(self, monkeypatch):
        _set_key(monkeypatch)
        existing = {"private_key": "ENCRYPTED_BLOB", "host": "old.example.com", "port": 22}
        partial = {"host": "new.example.com"}
        result = merge_and_encrypt_partial("file_system", existing, partial)
        assert result["host"] == "new.example.com"
        assert result["port"] == 22

    def test_non_safe_keys_in_partial_are_encrypted(self, monkeypatch):
        _set_key(monkeypatch)
        existing = {"private_key": "OLD_ENCRYPTED", "host": "sftp.example.com"}
        partial = {"private_key": "NEW_PLAINTEXT_KEY"}
        result = merge_and_encrypt_partial("file_system", existing, partial)
        # The updated private_key must be a new ciphertext, not plaintext.
        assert result["private_key"] != "NEW_PLAINTEXT_KEY"
        assert result["private_key"] != "OLD_ENCRYPTED"

    def test_existing_encrypted_key_preserved_when_not_in_partial(self, monkeypatch):
        _set_key(monkeypatch)
        existing = {"private_key": "EXISTING_CIPHERTEXT", "host": "sftp.example.com"}
        partial = {"host": "new-sftp.example.com"}
        result = merge_and_encrypt_partial("file_system", existing, partial)
        # Private key blob unchanged — it was not in partial_update.
        assert result["private_key"] == "EXISTING_CIPHERTEXT"

    def test_does_not_mutate_existing_encrypted(self, monkeypatch):
        _set_key(monkeypatch)
        existing = {"private_key": "CIPHERTEXT", "host": "old.example.com"}
        original_existing = dict(existing)
        merge_and_encrypt_partial("file_system", existing, {"host": "new.example.com"})
        assert existing == original_existing

    def test_partial_none_value_for_non_safe_field_is_passed_through(self, monkeypatch):
        """A None update for a non-safe field must not be encrypted — passed as None."""
        _set_key(monkeypatch)
        existing = {"private_key": "CIPHERTEXT", "host": "sftp.example.com"}
        result = merge_and_encrypt_partial("file_system", existing, {"private_key": None})
        assert result["private_key"] is None

    def test_s3_partial_update_encrypts_secret_access_key(self, monkeypatch):
        _set_key(monkeypatch)
        existing = {
            "bucket_name": "b",
            "access_key_id": "OLD_AK",
            "secret_access_key": "OLD_CIPHERTEXT",
        }
        partial = {"secret_access_key": "NEW_PLAINTEXT_SECRET"}
        result = merge_and_encrypt_partial("object_storage", existing, partial)
        assert result["secret_access_key"] != "NEW_PLAINTEXT_SECRET"
        assert result["bucket_name"] == "b"

# Made with Bob
