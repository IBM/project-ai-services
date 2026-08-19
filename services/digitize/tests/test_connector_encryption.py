"""
Unit tests for services/digitize/connectors/encryption.py

Coverage
--------
_load_key
  - raises RuntimeError when key file is absent
  - raises RuntimeError when key file has wrong byte length
  - returns an AESGCM instance when key is valid 32 bytes

_encrypt_value / _decrypt_value (round-trip)
  - encrypt then decrypt returns original plaintext
  - encrypted value differs from plaintext (not stored in clear)
  - different calls produce different ciphertext (random nonce)

encrypt_secrets
  - encrypts known secret fields and leaves other fields untouched
  - no-ops for connector types with no registered secret fields
  - skips None-valued secret fields
  - returns a copy — does not mutate the original dict

decrypt_secrets
  - decrypts values produced by encrypt_secrets
  - leaves non-secret fields untouched
  - re-raises on invalid ciphertext

strip_secrets
  - removes secret fields from ssh connector
  - removes secret fields from s3 connector
  - returns all fields for unknown connector type
  - does not mutate the original dict

merge_and_encrypt_partial
  - non-secret keys in partial_update are copied verbatim
  - secret keys in partial_update are re-encrypted
  - keys absent from partial_update are preserved from existing_encrypted
  - returns a new dict (does not mutate existing_encrypted)
"""

from __future__ import annotations

import os
import tempfile
from pathlib import Path
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
    strip_secrets,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Fixed 32-byte key that avoids all strippable whitespace characters.
# bytes.strip() strips ASCII whitespace: 0x09 (tab), 0x0a (LF), 0x0b (VT),
# 0x0c (FF), 0x0d (CR), 0x20 (space). We use bytes 0x41-0x60 (letters/symbols)
# to guarantee no stripping occurs.
_FIXED_KEY = bytes(range(0x41, 0x61))  # bytes 65-96 (A-Z and some punctuation)


def _write_key(tmp_path: Path, data: bytes) -> str:
    """Write *data* to a temp file and return its string path."""
    key_file = tmp_path / "enc.key"
    key_file.write_bytes(data)
    return str(key_file)


def _valid_key_path(tmp_path: Path) -> str:
    """Write a valid 32-byte key and return its path. Uses a fixed non-whitespace key."""
    return _write_key(tmp_path, _FIXED_KEY)


# ---------------------------------------------------------------------------
# _load_key
# ---------------------------------------------------------------------------

class TestLoadKey:
    def test_raises_when_file_absent(self, tmp_path):
        missing = str(tmp_path / "no_such_key.bin")
        # Clear cache from other tests that may have cached a value
        _load_key.cache_clear()
        with pytest.raises(RuntimeError, match="not found"):
            _load_key(missing)

    def test_raises_when_key_wrong_length(self, tmp_path):
        short_key_path = _write_key(tmp_path, b"short_key_16byte")  # 16 bytes, not 32
        _load_key.cache_clear()
        with pytest.raises(RuntimeError, match="32 bytes"):
            _load_key(short_key_path)

    def test_raises_when_key_too_long(self, tmp_path):
        long_key_path = _write_key(tmp_path, os.urandom(64))  # 64 bytes
        _load_key.cache_clear()
        with pytest.raises(RuntimeError, match="32 bytes"):
            _load_key(long_key_path)

    def test_returns_aesgcm_for_valid_32_byte_key(self, tmp_path):
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        cipher = _load_key(key_path)
        assert isinstance(cipher, AESGCM)


# ---------------------------------------------------------------------------
# _encrypt_value / _decrypt_value round-trip
# ---------------------------------------------------------------------------

class TestEncryptDecryptRoundTrip:
    def _cipher(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        return _get_cipher(key_path)

    def test_roundtrip_returns_original_plaintext(self, tmp_path):
        cipher = self._cipher(tmp_path)
        plaintext = "super_secret_private_key_data"
        token = _encrypt_value(cipher, plaintext)
        recovered = _decrypt_value(cipher, token)
        assert recovered == plaintext

    def test_encrypted_value_differs_from_plaintext(self, tmp_path):
        cipher = self._cipher(tmp_path)
        plaintext = "my_secret"
        token = _encrypt_value(cipher, plaintext)
        assert token != plaintext

    def test_two_encryptions_produce_different_ciphertext(self, tmp_path):
        """Random nonce → each call produces a unique token."""
        cipher = self._cipher(tmp_path)
        plaintext = "same_value"
        token1 = _encrypt_value(cipher, plaintext)
        token2 = _encrypt_value(cipher, plaintext)
        assert token1 != token2

    def test_empty_string_roundtrip(self, tmp_path):
        cipher = self._cipher(tmp_path)
        token = _encrypt_value(cipher, "")
        assert _decrypt_value(cipher, token) == ""

    def test_unicode_roundtrip(self, tmp_path):
        cipher = self._cipher(tmp_path)
        plaintext = "日本語テスト🔑"
        token = _encrypt_value(cipher, plaintext)
        assert _decrypt_value(cipher, token) == plaintext


# ---------------------------------------------------------------------------
# encrypt_secrets
# ---------------------------------------------------------------------------

class TestEncryptSecrets:
    def test_encrypts_ssh_private_key(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {"host": "example.com", "username": "user", "private_key": "MY_PRIVATE_KEY"}
        encrypted = encrypt_secrets("ssh", details, key_path)
        # The private_key field must be base64-encoded ciphertext, not plain
        assert encrypted["private_key"] != "MY_PRIVATE_KEY"
        assert encrypted["host"] == "example.com"
        assert encrypted["username"] == "user"

    def test_encrypts_s3_secret_access_key(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {"bucket": "my-bucket", "access_key_id": "AKID", "secret_access_key": "MY_SECRET"}
        encrypted = encrypt_secrets("s3", details, key_path)
        assert encrypted["secret_access_key"] != "MY_SECRET"
        assert encrypted["bucket"] == "my-bucket"
        assert encrypted["access_key_id"] == "AKID"

    def test_unknown_connector_type_leaves_all_fields_intact(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {"token": "bearer_token", "endpoint": "https://api.example.com"}
        result = encrypt_secrets("ftp", details, key_path)
        assert result == details

    def test_skips_none_valued_secret_fields(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {"private_key": None, "username": "admin"}
        result = encrypt_secrets("ssh", details, key_path)
        assert result["private_key"] is None
        assert result["username"] == "admin"

    def test_does_not_mutate_original_dict(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        original = {"private_key": "secret", "host": "ssh.example.com"}
        _ = encrypt_secrets("ssh", original, key_path)
        assert original["private_key"] == "secret"


# ---------------------------------------------------------------------------
# decrypt_secrets
# ---------------------------------------------------------------------------

class TestDecryptSecrets:
    def test_decrypts_value_encrypted_by_encrypt_secrets(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {"private_key": "-----BEGIN RSA PRIVATE KEY-----\nFAKE\n-----END RSA PRIVATE KEY-----"}
        encrypted = encrypt_secrets("ssh", details, key_path)
        decrypted = decrypt_secrets("ssh", encrypted, key_path)
        assert decrypted["private_key"] == details["private_key"]

    def test_leaves_non_secret_fields_untouched(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {"host": "sftp.example.com", "private_key": "KEY_DATA", "port": 22}
        encrypted = encrypt_secrets("ssh", details, key_path)
        decrypted = decrypt_secrets("ssh", encrypted, key_path)
        assert decrypted["host"] == "sftp.example.com"
        assert decrypted["port"] == 22

    def test_raises_on_tampered_ciphertext(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        bad_details = {"private_key": "not_valid_base64_ciphertext=="}
        with pytest.raises(Exception):
            decrypt_secrets("ssh", bad_details, key_path)

    def test_skips_none_valued_secret_fields(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {"private_key": None, "host": "host.example.com"}
        result = decrypt_secrets("ssh", details, key_path)
        assert result["private_key"] is None

    def test_s3_full_roundtrip(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        details = {
            "bucket": "my-bucket",
            "access_key_id": "AKID123",
            "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
        }
        encrypted = encrypt_secrets("s3", details, key_path)
        decrypted = decrypt_secrets("s3", encrypted, key_path)
        assert decrypted == details


# ---------------------------------------------------------------------------
# strip_secrets
# ---------------------------------------------------------------------------

class TestStripSecrets:
    def test_removes_private_key_from_ssh_details(self):
        details = {"host": "sftp.example.com", "username": "user", "private_key": "SENSITIVE"}
        result = strip_secrets("ssh", details)
        assert "private_key" not in result
        assert result["host"] == "sftp.example.com"
        assert result["username"] == "user"

    def test_removes_secret_access_key_from_s3_details(self):
        details = {"bucket": "b", "access_key_id": "AK", "secret_access_key": "SK"}
        result = strip_secrets("s3", details)
        assert "secret_access_key" not in result
        assert result["bucket"] == "b"
        assert result["access_key_id"] == "AK"

    def test_returns_all_fields_for_unknown_connector_type(self):
        details = {"token": "tok", "endpoint": "https://api.example.com"}
        result = strip_secrets("ftp", details)
        assert result == details

    def test_does_not_mutate_original_dict(self):
        original = {"private_key": "KEY", "host": "h"}
        _ = strip_secrets("ssh", original)
        assert "private_key" in original

    def test_field_absent_in_details_is_no_op(self):
        """strip_secrets must not raise if a secret field is simply absent."""
        details = {"host": "sftp.example.com", "username": "bob"}
        result = strip_secrets("ssh", details)
        assert result == details


# ---------------------------------------------------------------------------
# merge_and_encrypt_partial
# ---------------------------------------------------------------------------

class TestMergeAndEncryptPartial:
    def test_non_secret_keys_copied_verbatim(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        existing = {"private_key": "ENCRYPTED_BLOB", "host": "old.example.com", "port": 22}
        partial = {"host": "new.example.com"}
        result = merge_and_encrypt_partial("ssh", existing, partial, key_path)
        assert result["host"] == "new.example.com"
        assert result["port"] == 22

    def test_secret_keys_in_partial_are_encrypted(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        existing = {"private_key": "OLD_ENCRYPTED", "host": "sftp.example.com"}
        partial = {"private_key": "NEW_PLAINTEXT_KEY"}
        result = merge_and_encrypt_partial("ssh", existing, partial, key_path)
        # The updated private_key must be a new ciphertext, not plaintext
        assert result["private_key"] != "NEW_PLAINTEXT_KEY"
        assert result["private_key"] != "OLD_ENCRYPTED"

    def test_existing_encrypted_key_preserved_when_not_in_partial(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        existing = {"private_key": "EXISTING_CIPHERTEXT", "host": "sftp.example.com"}
        partial = {"host": "new-sftp.example.com"}
        result = merge_and_encrypt_partial("ssh", existing, partial, key_path)
        # Private key blob unchanged — it was not in partial_update
        assert result["private_key"] == "EXISTING_CIPHERTEXT"

    def test_does_not_mutate_existing_encrypted(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        existing = {"private_key": "CIPHERTEXT", "host": "old.example.com"}
        original_existing = dict(existing)
        merge_and_encrypt_partial("ssh", existing, {"host": "new.example.com"}, key_path)
        assert existing == original_existing

    def test_partial_none_value_for_secret_field_is_passed_through(self, tmp_path):
        """A None update for a secret field must not be encrypted — passed as None."""
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        existing = {"private_key": "CIPHERTEXT", "host": "sftp.example.com"}
        result = merge_and_encrypt_partial("ssh", existing, {"private_key": None}, key_path)
        assert result["private_key"] is None

    def test_s3_partial_update_encrypts_secret_access_key(self, tmp_path):
        key_path = _valid_key_path(tmp_path)
        _load_key.cache_clear()
        existing = {"bucket": "b", "access_key_id": "OLD_AK", "secret_access_key": "OLD_CIPHERTEXT"}
        partial = {"secret_access_key": "NEW_PLAINTEXT_SECRET"}
        result = merge_and_encrypt_partial("s3", existing, partial, key_path)
        assert result["secret_access_key"] != "NEW_PLAINTEXT_SECRET"
        assert result["bucket"] == "b"

# Made with Bob
