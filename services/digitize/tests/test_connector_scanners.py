"""
Unit tests for the connector scanner package.

Coverage
--------
S3ConnectorConfig
  - from_connection_details constructs correctly
  - provider auto-detection (aws vs cos)
  - effective_region extracted from endpoint_url
  - empty bucket_name raises ValueError

HashingWriter
  - MD5 of known bytes matches reference
  - empty stream gives correct empty-hash hexdigest
  - chunked writes produce same hash as bulk

S3Scanner
  - connect() builds client via _build_client
  - scan() returns full list of (key, etag) without filtering
  - scan() skips non-document keys
  - scan() strips ETag quotes
  - download_to() calls download_fileobj with correct args
  - download_to() returns the local MD5 hex digest
  - download_to() raises RuntimeError if connect() not called
  - verify_integrity() matches single-part ETag
  - verify_integrity() skips multi-part ETag (returns True)
  - verify_integrity() returns False on mismatch
  - close() resets _client to None

build_scanner factory
  - maps type='s3' to S3Scanner with S3ConnectorConfig
  - accepts dict connector_row
  - accepts ORM-like object connector_row
  - raises ValueError for unknown connector type
"""

from __future__ import annotations

import hashlib
import io
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import botocore.exceptions
import pytest

from digitize.connectors.scanners.config import S3ConnectorConfig
from digitize.connectors.scanners.hashing import HashingWriter
from digitize.connectors.scanners.s3_scanner import S3Scanner
from digitize.connectors.scanners.scanner_factory import build_scanner


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_config(**overrides) -> S3ConnectorConfig:
    defaults = dict(
        bucket_name="test-bucket",
        access_key_id="AKID",
        secret_access_key="SECRET",
        endpoint_url="https://s3.us.cloud-object-storage.appdomain.cloud",
        prefix="",
        delimiter="",
        verify_ssl=False,
        download_concurrency=2,
        allowed_extensions=[".pdf", ".docx"],
    )
    defaults.update(overrides)
    return S3ConnectorConfig(**defaults)


def _make_client_error(code: str = "NoSuchBucket") -> botocore.exceptions.ClientError:
    return botocore.exceptions.ClientError(
        {"Error": {"Code": code, "Message": "test"}}, "HeadBucket"
    )


def _make_mock_client(pages: list[dict] | None = None) -> MagicMock:
    """Return a MagicMock boto3 client pre-wired with a paginator."""
    client = MagicMock()
    paginator = MagicMock()
    paginator.paginate.return_value = pages or []
    client.get_paginator.return_value = paginator
    return client


# ---------------------------------------------------------------------------
# S3ConnectorConfig
# ---------------------------------------------------------------------------

class TestS3ConnectorConfig:
    def test_from_connection_details_basic(self):
        details = {
            "bucket_name": "my-bucket",
            "access_key_id": "AK",
            "secret_access_key": "SK",
            "endpoint_url": "https://s3.us.cloud-object-storage.appdomain.cloud",
        }
        cfg = S3ConnectorConfig.from_connection_details(
            details, allowed_extensions=[".pdf"]
        )
        assert cfg.bucket_name == "my-bucket"
        assert cfg.allowed_extensions == [".pdf"]

    def test_provider_aws_when_endpoint_absent(self):
        cfg = _make_config(endpoint_url="")
        assert cfg.provider == "aws"
        assert cfg.is_aws is True

    def test_provider_aws_when_amazonaws_hostname(self):
        cfg = _make_config(endpoint_url="https://s3.eu-west-1.amazonaws.com")
        assert cfg.provider == "aws"

    def test_provider_cos_for_ibm_endpoint(self):
        cfg = _make_config(
            endpoint_url="https://s3.us.cloud-object-storage.appdomain.cloud"
        )
        assert cfg.provider == "cos"
        assert cfg.is_aws is False

    def test_effective_region_from_aws_endpoint(self):
        cfg = _make_config(endpoint_url="https://s3.eu-west-2.amazonaws.com")
        assert cfg.effective_region == "eu-west-2"

    def test_effective_region_from_cos_direct_endpoint(self):
        cfg = _make_config(
            endpoint_url="https://s3.us-south.cloud-object-storage.appdomain.cloud"
        )
        assert cfg.effective_region == "us-south"

    def test_effective_region_cos_crossregion_us(self):
        """Cross-region alias 'us' must resolve to 'us-south', not 'us'."""
        cfg = _make_config(
            endpoint_url="https://s3.us.cloud-object-storage.appdomain.cloud"
        )
        assert cfg.effective_region == "us-south"

    def test_effective_region_cos_crossregion_eu(self):
        cfg = _make_config(
            endpoint_url="https://s3.eu.cloud-object-storage.appdomain.cloud"
        )
        assert cfg.effective_region == "eu-de"

    def test_effective_region_cos_crossregion_ap(self):
        cfg = _make_config(
            endpoint_url="https://s3.ap.cloud-object-storage.appdomain.cloud"
        )
        assert cfg.effective_region == "jp-tok"

    def test_effective_region_fallback(self):
        cfg = _make_config(endpoint_url="https://custom.example.com")
        assert cfg.effective_region == "us-east-1"

    def test_empty_bucket_raises(self):
        with pytest.raises(ValueError, match="bucket_name"):
            S3ConnectorConfig(bucket_name="  ", access_key_id="a", secret_access_key="b")

    def test_endpoint_url_without_scheme_raises(self):
        with pytest.raises(ValueError, match="https://"):
            _make_config(endpoint_url="s3.us-east-1.amazonaws.com")

    def test_endpoint_url_with_scheme_accepted(self):
        cfg = _make_config(endpoint_url="https://s3.us-east-1.amazonaws.com")
        assert cfg.endpoint_url == "https://s3.us-east-1.amazonaws.com"

    def test_endpoint_url_empty_accepted(self):
        cfg = _make_config(endpoint_url="")
        assert cfg.endpoint_url == ""

    def test_allowed_extensions_without_dot_raises(self):
        with pytest.raises(ValueError, match="'.'"):
            _make_config(allowed_extensions=["pdf", ".docx"])

    def test_allowed_extensions_valid(self):
        cfg = _make_config(allowed_extensions=[".PDF", ".docx"])
        assert cfg.allowed_extensions == [".pdf", ".docx"]

    def test_cos_requires_access_key_id(self):
        """IBM COS endpoint without access_key_id must raise."""
        with pytest.raises(ValueError, match="access_key_id"):
            S3ConnectorConfig(
                bucket_name="b",
                endpoint_url="https://s3.us-south.cloud-object-storage.appdomain.cloud",
                access_key_id="",
                secret_access_key="secret",
            )

    def test_cos_requires_secret_access_key(self):
        """IBM COS endpoint without secret_access_key must raise."""
        with pytest.raises(ValueError, match="secret_access_key"):
            S3ConnectorConfig(
                bucket_name="b",
                endpoint_url="https://s3.us-south.cloud-object-storage.appdomain.cloud",
                access_key_id="key",
                secret_access_key="",
            )

    def test_aws_allows_empty_credentials(self):
        """AWS S3 with empty credentials is valid — boto3 uses instance profile."""
        cfg = S3ConnectorConfig(
            bucket_name="b",
            endpoint_url="",
            access_key_id="",
            secret_access_key="",
        )
        assert cfg.is_aws is True


# ---------------------------------------------------------------------------
# HashingWriter
# ---------------------------------------------------------------------------

class TestHashingWriter:
    def test_md5_known_bytes(self):
        data = b"hello connector world"
        buf = io.BytesIO()
        writer = HashingWriter(buf)
        writer.write(data)
        assert writer.hexdigest == hashlib.md5(data).hexdigest()

    def test_md5_empty_stream(self):
        buf = io.BytesIO()
        writer = HashingWriter(buf)
        assert writer.hexdigest == hashlib.md5(b"").hexdigest()

    def test_chunked_same_as_bulk(self):
        data = b"abcdefgh" * 128
        buf = io.BytesIO()
        writer = HashingWriter(buf)
        for i in range(0, len(data), 16):
            writer.write(data[i : i + 16])
        assert writer.hexdigest == hashlib.md5(data).hexdigest()

    def test_bytes_forwarded_to_dest(self):
        data = b"scanner bytes"
        buf = io.BytesIO()
        writer = HashingWriter(buf)
        writer.write(data)
        assert buf.getvalue() == data

    def test_readable_false_writable_true(self):
        writer = HashingWriter(io.BytesIO())
        assert writer.readable() is False
        assert writer.writable() is True


# ---------------------------------------------------------------------------
# S3Scanner
# ---------------------------------------------------------------------------

class TestS3ScannerConnect:
    def test_connect_builds_client_and_preflight(self):
        """connect() must build the client AND call head_bucket for pre-flight."""
        scanner = S3Scanner(_make_config())
        mock_client = MagicMock()
        with patch.object(scanner, "_build_client", return_value=mock_client):
            scanner.connect()
        assert scanner._client is mock_client
        mock_client.head_bucket.assert_called_once_with(Bucket="test-bucket")

    def test_connect_raises_connection_error_on_bad_credentials(self):
        """connect() must raise ConnectionError (not silently succeed) on auth failure."""
        scanner = S3Scanner(_make_config())
        mock_client = MagicMock()
        mock_client.head_bucket.side_effect = _make_client_error("403")
        with patch.object(scanner, "_build_client", return_value=mock_client):
            with pytest.raises(ConnectionError, match="403"):
                scanner.connect()
        # client must be reset to None so scanner is not left in a connected state
        assert scanner._client is None

    def test_close_resets_client(self):
        cfg = _make_config()
        scanner = S3Scanner(cfg)
        scanner._client = MagicMock()
        scanner.close()
        assert scanner._client is None

    def test_scan_raises_if_not_connected(self):
        scanner = S3Scanner(_make_config())
        with pytest.raises(RuntimeError, match="connect()"):
            scanner.scan()

    def test_download_to_raises_if_not_connected(self, tmp_path):
        scanner = S3Scanner(_make_config())
        with pytest.raises(RuntimeError, match="connect()"):
            scanner.download_to("some/key.pdf", tmp_path / "key.pdf")


class TestS3ScannerScan:
    def _make_page(self, objects: list[dict], prefixes: list[str] | None = None):
        page: dict = {"Contents": objects}
        if prefixes:
            page["CommonPrefixes"] = [{"Prefix": p} for p in prefixes]
        return page

    def _make_obj(self, key: str, etag: str) -> dict:
        return {"Key": key, "ETag": f'"{etag}"'}

    def test_returns_all_supported_files(self):
        cfg = _make_config()
        scanner = S3Scanner(cfg)
        page = self._make_page([
            self._make_obj("docs/report.pdf", "abc123"),
            self._make_obj("docs/manual.docx", "def456"),
        ])
        scanner._client = _make_mock_client([page])

        result = scanner.scan()

        assert result == [("docs/report.pdf", "abc123"), ("docs/manual.docx", "def456")]

    def test_skips_non_document_extensions(self):
        cfg = _make_config()
        scanner = S3Scanner(cfg)
        page = self._make_page([
            self._make_obj("report.pdf", "aaa"),
            self._make_obj("readme.txt", "bbb"),
            self._make_obj("archive.zip", "ccc"),
        ])
        scanner._client = _make_mock_client([page])

        result = scanner.scan()
        keys = [r[0] for r in result]

        assert "report.pdf" in keys
        assert "readme.txt" not in keys
        assert "archive.zip" not in keys

    def test_strips_etag_quotes(self):
        cfg = _make_config()
        scanner = S3Scanner(cfg)
        page = self._make_page([self._make_obj("a.pdf", "etag_no_quotes")])
        scanner._client = _make_mock_client([page])

        result = scanner.scan()
        assert result[0][1] == "etag_no_quotes"

    def test_empty_bucket_returns_empty_list(self):
        cfg = _make_config()
        scanner = S3Scanner(cfg)
        scanner._client = _make_mock_client([{"Contents": []}])
        assert scanner.scan() == []

    def test_no_dedup_filtering(self):
        """scan() must return all files — no dedup in scanner."""
        cfg = _make_config()
        scanner = S3Scanner(cfg)
        page = self._make_page([
            self._make_obj("a.pdf", "same_etag"),
            self._make_obj("b.pdf", "same_etag"),
        ])
        scanner._client = _make_mock_client([page])

        result = scanner.scan()
        assert len(result) == 2  # both returned even with identical ETags


class TestS3ScannerDownloadTo:
    def _fake_download(self, data: bytes):
        def _side_effect(Bucket, Key, Fileobj):
            Fileobj.write(data)
        return _side_effect

    def test_calls_download_fileobj(self, tmp_path):
        scanner = S3Scanner(_make_config())
        mock_client = MagicMock()
        scanner._client = mock_client
        mock_client.download_fileobj.side_effect = self._fake_download(b"pdf content bytes")

        local_path = tmp_path / "report.pdf"
        scanner.download_to("docs/report.pdf", local_path)

        call_kwargs = mock_client.download_fileobj.call_args.kwargs
        assert call_kwargs["Bucket"] == "test-bucket"
        assert call_kwargs["Key"] == "docs/report.pdf"
        assert isinstance(call_kwargs["Fileobj"], HashingWriter)

    def test_returns_local_md5(self, tmp_path):
        import hashlib
        content = b"some file bytes"
        scanner = S3Scanner(_make_config())
        mock_client = MagicMock()
        scanner._client = mock_client
        mock_client.download_fileobj.side_effect = self._fake_download(content)

        local_path = tmp_path / "doc.pdf"
        result = scanner.download_to("doc.pdf", local_path)

        assert result == hashlib.md5(content).hexdigest()

    def test_staged_file_written(self, tmp_path):
        content = b"real pdf file content"
        scanner = S3Scanner(_make_config())
        mock_client = MagicMock()
        scanner._client = mock_client
        mock_client.download_fileobj.side_effect = self._fake_download(content)

        local_path = tmp_path / "doc.pdf"
        scanner.download_to("doc.pdf", local_path)

        assert local_path.read_bytes() == content


class TestBaseVerifyIntegrity:
    """Tests for the base-class equality check (transport-agnostic)."""

    def _make_scanner(self) -> S3Scanner:
        return S3Scanner(_make_config())

    def test_match_returns_true(self):
        assert self._make_scanner().verify_integrity("abcdef", "abcdef") is True

    def test_mismatch_returns_false(self):
        assert self._make_scanner().verify_integrity("aabbcc", "ddeeff") is False


class TestS3ScannerVerifyIntegrity:
    """Tests for the S3-specific override (multi-part ETag handling)."""

    def test_single_part_match(self):
        import hashlib
        data = b"hello"
        md5 = hashlib.md5(data).hexdigest()
        assert S3Scanner(_make_config()).verify_integrity(md5, md5) is True

    def test_single_part_mismatch(self):
        assert S3Scanner(_make_config()).verify_integrity("aabbcc", "ddeeff") is False

    def test_multipart_etag_always_passes(self):
        """Multi-part ETags contain '-N'; integrity check must be skipped."""
        assert S3Scanner(_make_config()).verify_integrity("anylocalmd5", "abc123-4") is True


# ---------------------------------------------------------------------------
# build_scanner factory
# ---------------------------------------------------------------------------

class TestBuildScanner:
    def _make_connector_dict(self, connector_type: str = "s3") -> dict:
        return {
            "type": connector_type,
            "connection_details": {
                "bucket_name": "my-bucket",
                "access_key_id": "AK",
                "secret_access_key": "SK",
                "endpoint_url": "https://s3.us.cloud-object-storage.appdomain.cloud",
            },
            "allowed_extensions": [".pdf", ".docx"],
        }

    def test_s3_type_returns_s3_scanner(self):
        row = self._make_connector_dict("s3")
        scanner = build_scanner(row)
        assert isinstance(scanner, S3Scanner)

    def test_s3_scanner_config_populated(self):
        row = self._make_connector_dict("s3")
        scanner = build_scanner(row)
        assert scanner._cfg.bucket_name == "my-bucket"
        assert scanner._cfg.allowed_extensions == [".pdf", ".docx"]

    def test_accepts_orm_like_object(self):
        row = SimpleNamespace(
            type="s3",
            connection_details={
                "bucket_name": "ns-bucket",
                "access_key_id": "AK",
                "secret_access_key": "SK",
            },
            allowed_extensions=[".pdf"],
        )
        scanner = build_scanner(row)
        assert isinstance(scanner, S3Scanner)
        assert scanner._cfg.bucket_name == "ns-bucket"

    def test_unknown_type_raises_value_error(self):
        row = {"type": "ftp", "connection_details": {}, "allowed_extensions": []}
        with pytest.raises(ValueError, match="ftp"):
            build_scanner(row)


# ---------------------------------------------------------------------------
# Helpers — fake PEM key for unit tests (no real SSH needed)
# ---------------------------------------------------------------------------

# A minimal dummy PEM string that satisfies SSHConnectorConfig validation
# without being a real parseable key (the key is never sent to Paramiko in
# unit tests — Paramiko is always mocked).
_FAKE_PEM = "-----BEGIN RSA PRIVATE KEY-----\nFAKEKEYDATA\n-----END RSA PRIVATE KEY-----"


def _make_sftp_config(**overrides) -> "SSHConnectorConfig":
    from digitize.connectors.scanners.config import SSHConnectorConfig
    defaults = dict(
        host="sftp.example.com",
        port=22,
        username="user",
        private_key_pem=_FAKE_PEM,
        remote_path="/data",
        allowed_extensions=[".pdf", ".docx"],
    )
    defaults.update(overrides)
    return SSHConnectorConfig(**defaults)


def _make_ssh_scanner(**overrides) -> "SSHScanner":
    from digitize.connectors.scanners.ssh_scanner import SSHScanner
    return SSHScanner(_make_sftp_config(**overrides))


# ---------------------------------------------------------------------------
# SSHConnectorConfig
# ---------------------------------------------------------------------------

class TestSSHConnectorConfig:
    def test_basic_construction(self):
        cfg = _make_sftp_config()
        assert cfg.host == "sftp.example.com"
        assert cfg.port == 22
        assert cfg.username == "user"
        assert cfg.remote_path == "/data"
        assert cfg.allowed_extensions == [".pdf", ".docx"]

    def test_default_port_is_22(self):
        cfg = _make_sftp_config()
        assert cfg.port == 22

    def test_default_remote_path_is_root(self):
        from digitize.connectors.scanners.config import SSHConnectorConfig
        cfg = SSHConnectorConfig(
            host="h",
            username="u",
            private_key_pem=_FAKE_PEM,
        )
        assert cfg.remote_path == "/"

    def test_empty_host_raises(self):
        with pytest.raises(ValueError, match="host"):
            _make_sftp_config(host="  ")

    def test_empty_username_raises(self):
        with pytest.raises(ValueError, match="username"):
            _make_sftp_config(username="")

    def test_empty_private_key_raises(self):
        with pytest.raises(ValueError, match="private_key_pem"):
            _make_sftp_config(private_key_pem="  ")

    def test_invalid_private_key_raises(self):
        with pytest.raises(ValueError, match="PRIVATE KEY"):
            _make_sftp_config(private_key_pem="not a pem string")

    def test_allowed_extensions_without_dot_raises(self):
        with pytest.raises(ValueError, match="'.'"):
            _make_sftp_config(allowed_extensions=["pdf"])

    def test_allowed_extensions_normalised_to_lowercase(self):
        cfg = _make_sftp_config(allowed_extensions=[".PDF", ".DOCX"])
        assert cfg.allowed_extensions == [".pdf", ".docx"]

    def test_empty_remote_path_raises(self):
        with pytest.raises(ValueError, match="remote_path"):
            _make_sftp_config(remote_path="")

    def test_from_connection_details(self):
        from digitize.connectors.scanners.config import SSHConnectorConfig
        details = {
            "host": "sftp.example.com",
            "username": "u",
            "private_key_pem": _FAKE_PEM,
        }
        cfg = SSHConnectorConfig.from_connection_details(
            details, allowed_extensions=[".pdf"]
        )
        assert cfg.host == "sftp.example.com"
        assert cfg.allowed_extensions == [".pdf"]

    def test_from_connection_details_extensions_override(self):
        """allowed_extensions kwarg takes precedence over any embedded value."""
        from digitize.connectors.scanners.config import SSHConnectorConfig
        details = {
            "host": "h",
            "username": "u",
            "private_key_pem": _FAKE_PEM,
            "allowed_extensions": [".txt"],
        }
        cfg = SSHConnectorConfig.from_connection_details(
            details, allowed_extensions=[".pdf"]
        )
        assert cfg.allowed_extensions == [".pdf"]

    def test_extra_fields_ignored(self):
        """model_config extra='ignore' — unknown keys must not raise."""
        from digitize.connectors.scanners.config import SSHConnectorConfig
        cfg = SSHConnectorConfig.model_validate({
            "host": "h",
            "username": "u",
            "private_key_pem": _FAKE_PEM,
            "unknown_field": "should_be_ignored",
        })
        assert cfg.host == "h"


# ---------------------------------------------------------------------------
# SSHScanner — connect / close
# ---------------------------------------------------------------------------

class TestSSHScannerConnect:
    def test_connect_opens_ssh_and_sftp(self):
        """connect() must create an SSHClient, call connect(), and open SFTP."""
        from digitize.connectors.scanners.ssh_scanner import SSHScanner
        import paramiko

        scanner = _make_ssh_scanner()

        mock_ssh = MagicMock(spec=paramiko.SSHClient)
        mock_sftp = MagicMock(spec=paramiko.SFTPClient)
        mock_ssh.open_sftp.return_value = mock_sftp
        mock_pkey = MagicMock()

        with patch("digitize.connectors.scanners.ssh_scanner.paramiko.SSHClient",
                   return_value=mock_ssh), \
             patch.object(SSHScanner, "_load_private_key", return_value=mock_pkey):
            scanner.connect()

        mock_ssh.connect.assert_called_once_with(
            hostname="sftp.example.com",
            port=22,
            username="user",
            pkey=mock_pkey,
            look_for_keys=False,
            allow_agent=False,
        )
        mock_ssh.open_sftp.assert_called_once()
        assert scanner._ssh is mock_ssh
        assert scanner._sftp is mock_sftp

    def test_connect_raises_connection_error_on_bad_key(self):
        import paramiko
        scanner = _make_ssh_scanner()

        with patch.object(
            scanner.__class__, "_load_private_key",
            side_effect=paramiko.SSHException("bad key"),
        ):
            with pytest.raises(ConnectionError, match="Failed to load private key"):
                scanner.connect()

    def test_connect_raises_connection_error_on_auth_failure(self):
        import paramiko
        scanner = _make_ssh_scanner()
        mock_ssh = MagicMock(spec=paramiko.SSHClient)
        mock_ssh.connect.side_effect = paramiko.AuthenticationException("denied")
        mock_pkey = MagicMock()

        with patch("digitize.connectors.scanners.ssh_scanner.paramiko.SSHClient",
                   return_value=mock_ssh), \
             patch.object(scanner.__class__, "_load_private_key", return_value=mock_pkey):
            with pytest.raises(ConnectionError, match="Cannot connect"):
                scanner.connect()

    def test_close_clears_ssh_and_sftp(self):
        import paramiko
        scanner = _make_ssh_scanner()
        scanner._ssh = MagicMock(spec=paramiko.SSHClient)
        scanner._sftp = MagicMock(spec=paramiko.SFTPClient)

        scanner.close()

        assert scanner._ssh is None
        assert scanner._sftp is None

    def test_close_is_safe_when_not_connected(self):
        """close() must not raise when called before connect()."""
        scanner = _make_ssh_scanner()
        scanner.close()  # should not raise

    def test_scan_raises_if_not_connected(self):
        scanner = _make_ssh_scanner()
        with pytest.raises(RuntimeError, match="connect()"):
            scanner.scan()

    def test_download_to_raises_if_not_connected(self, tmp_path):
        scanner = _make_ssh_scanner()
        with pytest.raises(RuntimeError, match="connect()"):
            scanner.download_to("/data/doc.pdf", tmp_path / "doc.pdf")


# ---------------------------------------------------------------------------
# SSHScanner — scan (recursive listing + extension filtering)
# ---------------------------------------------------------------------------

class TestSSHScannerScan:
    def _attach(self, scanner, mock_sftp, mock_ssh):
        """Wire pre-built mocks directly onto the scanner."""
        scanner._sftp = mock_sftp
        scanner._ssh = mock_ssh

    def _make_stat(self, name: str, is_dir: bool = False):
        """Return a minimal Paramiko SFTPAttributes-like object."""
        import stat as stat_module
        attrs = MagicMock()
        attrs.filename = name
        attrs.st_mode = (
            stat_module.S_IFDIR | 0o755
            if is_dir
            else stat_module.S_IFREG | 0o644
        )
        return attrs

    def test_scan_returns_allowed_files(self):
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_ssh = MagicMock()

        # /data contains report.pdf and manual.docx
        mock_sftp.listdir_attr.return_value = [
            self._make_stat("report.pdf"),
            self._make_stat("manual.docx"),
        ]
        def exec_command_side_effect(cmd):
            stdout = MagicMock()
            stdout.read.return_value = b"abc123  " + cmd.split('"')[1].encode()
            stdout.channel.recv_exit_status.return_value = 0
            stderr = MagicMock()
            stderr.read.return_value = b""
            return None, stdout, stderr

        mock_ssh.exec_command.side_effect = exec_command_side_effect
        self._attach(scanner, mock_sftp, mock_ssh)

        result = scanner.scan()
        paths = [r[0] for r in result]
        assert "/data/report.pdf" in paths
        assert "/data/manual.docx" in paths

    def test_scan_filters_non_allowed_extensions(self):
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_ssh = MagicMock()

        mock_sftp.listdir_attr.return_value = [
            self._make_stat("report.pdf"),
            self._make_stat("readme.txt"),
            self._make_stat("archive.zip"),
        ]
        stdout = MagicMock()
        stdout.read.return_value = b"aabbcc  /data/report.pdf"
        stdout.channel.recv_exit_status.return_value = 0
        stderr = MagicMock()
        stderr.read.return_value = b""
        mock_ssh.exec_command.return_value = (None, stdout, stderr)
        self._attach(scanner, mock_sftp, mock_ssh)

        result = scanner.scan()
        paths = [r[0] for r in result]
        assert any(p.endswith(".pdf") for p in paths)
        assert not any(p.endswith(".txt") for p in paths)
        assert not any(p.endswith(".zip") for p in paths)

    def test_scan_recurses_into_subdirectories(self):
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_ssh = MagicMock()

        # /data → subdir/, report.pdf
        # /data/subdir → nested.docx
        def listdir_attr_side_effect(path):
            if path == "/data":
                return [
                    self._make_stat("subdir", is_dir=True),
                    self._make_stat("report.pdf"),
                ]
            if path == "/data/subdir":
                return [self._make_stat("nested.docx")]
            return []

        mock_sftp.listdir_attr.side_effect = listdir_attr_side_effect
        stdout = MagicMock()
        stdout.read.return_value = b"deadbeef  file"
        stdout.channel.recv_exit_status.return_value = 0
        stderr = MagicMock()
        stderr.read.return_value = b""
        mock_ssh.exec_command.return_value = (None, stdout, stderr)
        self._attach(scanner, mock_sftp, mock_ssh)

        result = scanner.scan()
        paths = [r[0] for r in result]
        assert "/data/report.pdf" in paths
        assert "/data/subdir/nested.docx" in paths

    def test_scan_empty_remote_dir_returns_empty_list(self):
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_sftp.listdir_attr.return_value = []
        self._attach(scanner, mock_sftp, MagicMock())

        assert scanner.scan() == []

    def test_scan_checksum_from_remote_md5(self):
        """scan() must use the first token of md5sum output as the checksum."""
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_ssh = MagicMock()

        mock_sftp.listdir_attr.return_value = [self._make_stat("doc.pdf")]

        expected_md5 = "d41d8cd98f00b204e9800998ecf8427e"
        stdout = MagicMock()
        stdout.read.return_value = f"{expected_md5}  /data/doc.pdf".encode()
        stdout.channel.recv_exit_status.return_value = 0
        stderr = MagicMock()
        stderr.read.return_value = b""
        mock_ssh.exec_command.return_value = (None, stdout, stderr)
        self._attach(scanner, mock_sftp, mock_ssh)

        result = scanner.scan()
        assert result == [("/data/doc.pdf", expected_md5)]

    def test_scan_raises_when_remote_md5_command_fails(self):
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_ssh = MagicMock()

        mock_sftp.listdir_attr.return_value = [self._make_stat("doc.pdf")]

        stdout = MagicMock()
        stdout.read.return_value = b""
        stdout.channel.recv_exit_status.return_value = 1
        stderr = MagicMock()
        stderr.read.return_value = b"md5sum: /data/doc.pdf: No such file or directory"
        mock_ssh.exec_command.return_value = (None, stdout, stderr)
        self._attach(scanner, mock_sftp, mock_ssh)

        with pytest.raises(RuntimeError, match="Failed to compute md5"):
            scanner.scan()

    def test_scan_skips_unreadable_dirs_gracefully(self):
        """An IOError from listdir_attr must be swallowed (logged, not raised)."""
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_sftp.listdir_attr.side_effect = IOError("Permission denied")
        self._attach(scanner, mock_sftp, MagicMock())

        # Must return empty list without raising
        result = scanner.scan()
        assert result == []

    def test_scan_returns_full_list_without_dedup(self):
        """All matched files must be returned — no dedup filtering in the scanner."""
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_ssh = MagicMock()

        same_md5 = "aaaa1111bbbb2222cccc3333dddd4444"
        mock_sftp.listdir_attr.return_value = [
            self._make_stat("a.pdf"),
            self._make_stat("b.pdf"),
        ]
        stdout = MagicMock()
        stdout.read.return_value = f"{same_md5}  file".encode()
        stdout.channel.recv_exit_status.return_value = 0
        stderr = MagicMock()
        stderr.read.return_value = b""
        mock_ssh.exec_command.return_value = (None, stdout, stderr)
        self._attach(scanner, mock_sftp, mock_ssh)

        result = scanner.scan()
        assert len(result) == 2  # both returned even with identical checksums


# ---------------------------------------------------------------------------
# SSHScanner — download_to
# ---------------------------------------------------------------------------

class TestSSHScannerDownloadTo:
    def _attach(self, scanner, mock_sftp, mock_ssh=None):
        scanner._sftp = mock_sftp
        scanner._ssh = mock_ssh or MagicMock()

    def _fake_getfo(self, data: bytes):
        """Return a side_effect for sftp.getfo that writes bytes to the fileobj."""
        def _side_effect(remote_path, fileobj):
            fileobj.write(data)
        return _side_effect

    def test_writes_bytes_to_local_path(self, tmp_path):
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        content = b"pdf bytes here"
        mock_sftp.getfo.side_effect = self._fake_getfo(content)
        self._attach(scanner, mock_sftp)

        local = tmp_path / "doc.pdf"
        scanner.download_to("/data/doc.pdf", local)

        assert local.read_bytes() == content

    def test_returns_local_md5_hex(self, tmp_path):
        import hashlib
        content = b"some document content"
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_sftp.getfo.side_effect = self._fake_getfo(content)
        self._attach(scanner, mock_sftp)

        result = scanner.download_to("/data/doc.pdf", tmp_path / "doc.pdf")
        assert result == hashlib.md5(content).hexdigest()

    def test_calls_getfo_with_correct_remote_path(self, tmp_path):
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_sftp.getfo.side_effect = self._fake_getfo(b"x")
        self._attach(scanner, mock_sftp)

        scanner.download_to("/data/subdir/report.pdf", tmp_path / "report.pdf")

        call_args = mock_sftp.getfo.call_args
        assert call_args[0][0] == "/data/subdir/report.pdf"
        assert isinstance(call_args[0][1], HashingWriter)

    def test_hashing_writer_used_for_inline_md5(self, tmp_path):
        """getfo Fileobj argument must be a HashingWriter — no second file read."""
        scanner = _make_ssh_scanner()
        mock_sftp = MagicMock()
        mock_sftp.getfo.side_effect = self._fake_getfo(b"data")
        self._attach(scanner, mock_sftp)

        scanner.download_to("/remote/file.pdf", tmp_path / "file.pdf")

        call_args = mock_sftp.getfo.call_args
        assert isinstance(call_args[0][1], HashingWriter)


# ---------------------------------------------------------------------------
# SSHScanner — verify_integrity (inherits base class direct equality)
# ---------------------------------------------------------------------------

class TestSSHScannerVerifyIntegrity:
    def test_matching_checksums_returns_true(self):
        scanner = _make_ssh_scanner()
        assert scanner.verify_integrity("abc123", "abc123") is True

    def test_mismatched_checksums_returns_false(self):
        scanner = _make_ssh_scanner()
        assert scanner.verify_integrity("abc123", "def456") is False

    def test_uses_direct_equality_not_s3_logic(self):
        """SFTP checksums are plain MD5 hex — a dash does NOT mean skip."""
        # S3Scanner would skip a checksum containing '-', but SSHScanner must not.
        scanner = _make_ssh_scanner()
        assert scanner.verify_integrity("abc123", "abc-123") is False


# ---------------------------------------------------------------------------
# SSHScanner — _load_private_key
# ---------------------------------------------------------------------------

class TestSFTPLoadPrivateKey:
    def test_rsa_key_loaded(self):
        """_load_private_key must succeed for an RSA PEM string."""
        from digitize.connectors.scanners.ssh_scanner import SSHScanner
        import paramiko

        mock_rsa_key = MagicMock(spec=paramiko.RSAKey)
        with patch.object(paramiko.RSAKey, "from_private_key", return_value=mock_rsa_key):
            result = SSHScanner._load_private_key(_FAKE_PEM)
        assert result is mock_rsa_key

    def test_falls_back_to_ecdsa_when_rsa_fails(self):
        from digitize.connectors.scanners.ssh_scanner import SSHScanner
        import paramiko

        mock_ecdsa_key = MagicMock(spec=paramiko.ECDSAKey)
        with patch.object(paramiko.RSAKey, "from_private_key",
                          side_effect=paramiko.SSHException("not rsa")), \
             patch.object(paramiko.ECDSAKey, "from_private_key",
                          return_value=mock_ecdsa_key):
            result = SSHScanner._load_private_key(_FAKE_PEM)
        assert result is mock_ecdsa_key

    def test_falls_back_to_ed25519_when_rsa_and_ecdsa_fail(self):
        from digitize.connectors.scanners.ssh_scanner import SSHScanner
        import paramiko

        mock_ed_key = MagicMock(spec=paramiko.Ed25519Key)
        with patch.object(paramiko.RSAKey, "from_private_key",
                          side_effect=paramiko.SSHException("not rsa")), \
             patch.object(paramiko.ECDSAKey, "from_private_key",
                          side_effect=paramiko.SSHException("not ecdsa")), \
             patch.object(paramiko.Ed25519Key, "from_private_key",
                          return_value=mock_ed_key):
            result = SSHScanner._load_private_key(_FAKE_PEM)
        assert result is mock_ed_key

    def test_raises_ssh_exception_when_no_key_type_matches(self):
        from digitize.connectors.scanners.ssh_scanner import SSHScanner
        import paramiko

        with patch.object(paramiko.RSAKey, "from_private_key",
                          side_effect=paramiko.SSHException("no")), \
             patch.object(paramiko.ECDSAKey, "from_private_key",
                          side_effect=paramiko.SSHException("no")), \
             patch.object(paramiko.Ed25519Key, "from_private_key",
                          side_effect=paramiko.SSHException("no")):
            with pytest.raises(paramiko.SSHException, match="RSA, ECDSA, or Ed25519"):
                SSHScanner._load_private_key(_FAKE_PEM)


# ---------------------------------------------------------------------------
# build_scanner factory — ssh type
# ---------------------------------------------------------------------------

class TestBuildScannerSSH:
    def _make_ssh_connector_dict(self) -> dict:
        return {
            "type": "ssh",
            "connection_details": {
                "host": "sftp.example.com",
                "username": "user",
                "private_key_pem": _FAKE_PEM,
            },
            "allowed_extensions": [".pdf", ".docx"],
        }

    def test_ssh_type_returns_ssh_scanner(self):
        from digitize.connectors.scanners.ssh_scanner import SSHScanner
        row = self._make_ssh_connector_dict()
        scanner = build_scanner(row)
        assert isinstance(scanner, SSHScanner)

    def test_ssh_scanner_config_populated(self):
        row = self._make_ssh_connector_dict()
        scanner = build_scanner(row)
        assert scanner._cfg.host == "sftp.example.com"
        assert scanner._cfg.username == "user"
        assert scanner._cfg.allowed_extensions == [".pdf", ".docx"]

    def test_ssh_accepts_orm_like_object(self):
        from digitize.connectors.scanners.ssh_scanner import SSHScanner
        row = SimpleNamespace(
            type="ssh",
            connection_details={
                "host": "sftp.host",
                "username": "admin",
                "private_key_pem": _FAKE_PEM,
            },
            allowed_extensions=[".pdf"],
        )
        scanner = build_scanner(row)
        assert isinstance(scanner, SSHScanner)
        assert scanner._cfg.host == "sftp.host"
