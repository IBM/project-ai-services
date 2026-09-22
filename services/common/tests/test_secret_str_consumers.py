"""
Smoke tests: every SecretStr field must be unwrapped with .get_secret_value()
before being passed to external clients.

These tests verify that:
  - LLMConfig.api_key           → get_secret_value() yields the raw string
  - VectorStoreConfig.opensearch_password → get_secret_value() yields the raw string
  - S3ConnectorConfig.secret_access_key   → get_secret_value() yields the raw string
  - SSHConnectorConfig.private_key        → get_secret_value() yields the raw string
  - None of the above fields serialise the secret when repr()ed or printed

They are intentionally free of network I/O — they only instantiate config
objects and assert that the Python type and value are correct.
"""

import os
import sys
import pytest

# ---------------------------------------------------------------------------
# Bring the common package onto the path so the test can be run standalone.
# ---------------------------------------------------------------------------
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))


_FAKE_PEM = (
    "-----BEGIN RSA PRIVATE KEY-----\n"
    "MIIEowIBAAKCAQEA0Z3VS5JJcds3xHn/ygWep4PAtEsHABBBBBBBBBBBBBBBBBBB\n"
    "-----END RSA PRIVATE KEY-----"
)


# ---------------------------------------------------------------------------
# LLMConfig.api_key
# ---------------------------------------------------------------------------

class TestLLMConfigApiKey:
    def test_get_secret_value_returns_raw_string(self):
        from common.settings import LLMConfig
        cfg = LLMConfig(api_key="my-secret-token")
        assert cfg.api_key.get_secret_value() == "my-secret-token"

    def test_empty_default_returns_empty_string(self):
        from common.settings import LLMConfig
        cfg = LLMConfig()
        assert cfg.api_key.get_secret_value() == ""

    def test_repr_does_not_leak_secret(self):
        from common.settings import LLMConfig
        cfg = LLMConfig(api_key="super-secret")
        assert "super-secret" not in repr(cfg)

    def test_str_does_not_leak_secret(self):
        from common.settings import LLMConfig
        cfg = LLMConfig(api_key="super-secret")
        assert "super-secret" not in str(cfg)


# ---------------------------------------------------------------------------
# VectorStoreConfig.opensearch_password
# ---------------------------------------------------------------------------

class TestVectorStoreConfigOpensearchPassword:
    def test_get_secret_value_returns_raw_string(self):
        from common.settings import VectorStoreConfig
        cfg = VectorStoreConfig(opensearch_password="hunter2")
        assert cfg.opensearch_password.get_secret_value() == "hunter2"

    def test_empty_default_returns_empty_string(self):
        from common.settings import VectorStoreConfig
        cfg = VectorStoreConfig()
        assert cfg.opensearch_password.get_secret_value() == ""

    def test_repr_does_not_leak_secret(self):
        from common.settings import VectorStoreConfig
        cfg = VectorStoreConfig(opensearch_password="hunter2")
        assert "hunter2" not in repr(cfg)


# ---------------------------------------------------------------------------
# S3ConnectorConfig.secret_access_key
# ---------------------------------------------------------------------------

class TestS3ConnectorConfigSecretAccessKey:
    def test_get_secret_value_returns_raw_string(self):
        from digitize.connectors.scanners.config import S3ConnectorConfig
        cfg = S3ConnectorConfig(
            bucket_name="my-bucket",
            access_key_id="AKID",
            secret_access_key="wJalrXUtnFEMI",
        )
        assert cfg.secret_access_key.get_secret_value() == "wJalrXUtnFEMI"

    def test_repr_does_not_leak_secret(self):
        from digitize.connectors.scanners.config import S3ConnectorConfig
        cfg = S3ConnectorConfig(
            bucket_name="my-bucket",
            secret_access_key="wJalrXUtnFEMI",
        )
        assert "wJalrXUtnFEMI" not in repr(cfg)


# ---------------------------------------------------------------------------
# SSHConnectorConfig.private_key
# ---------------------------------------------------------------------------

class TestSSHConnectorConfigPrivateKey:
    def test_get_secret_value_returns_raw_pem(self):
        from digitize.connectors.scanners.config import SSHConnectorConfig
        cfg = SSHConnectorConfig(
            host="sftp.example.com",
            username="user",
            private_key=_FAKE_PEM,
        )
        assert cfg.private_key.get_secret_value() == _FAKE_PEM

    def test_repr_does_not_leak_pem(self):
        from digitize.connectors.scanners.config import SSHConnectorConfig
        cfg = SSHConnectorConfig(
            host="sftp.example.com",
            username="user",
            private_key=_FAKE_PEM,
        )
        assert "MIIEowIBAAKCAQEA" not in repr(cfg)
