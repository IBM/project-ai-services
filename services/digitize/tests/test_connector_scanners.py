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
