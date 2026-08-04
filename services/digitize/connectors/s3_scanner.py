"""
connector/s3_scanner.py — S3 data-source scanner.

Implements BaseScanner for AWS S3 and IBM COS (S3-compatible) sources.

Design decisions
----------------
* ``scan()`` returns the full object list with no dedup filtering.  All
  classification logic lives in the worker's _classify() method.

* ``download_to()`` streams the file through _HashingWriter (inline MD5) so
  there is no second file-read to verify integrity.

* ``source_checksum`` (S3 ETag) is written into ``documents.metadata`` after
  ingest via ``set_document_metadata()``.  No other fields are written to
  documents.metadata here.

* ``connector_document_checksum`` writes are intentionally excluded — the
  worker calls ``add_connector_to_membership()`` after all per-file ingest
  jobs complete, under the process-wide ingest_lock.

ETag format reminder
--------------------
  Single-part upload : ETag = MD5(file_bytes) — 32-char hex, no suffix
  Multi-part upload  : ETag = MD5(raw_part_digests)-N — hex + "-N" suffix

The ETag is used as-is as the dedup key.  For single-part objects the local
MD5 computed by _HashingWriter will equal the ETag (without quotes); for
multi-part objects the local MD5 equals MD5(full_file_bytes) which differs
from the stored multi-part ETag — this is expected and harmless because the
worker deduplication key is always the S3 ETag from list_objects_v2.
"""

from __future__ import annotations

import hashlib
import io
import os
from pathlib import Path
from typing import Iterator, Optional, Tuple

import boto3
import botocore.config
import botocore.exceptions

from common.misc_utils import get_logger
from digitize.connectors.base_scanner import BaseScanner
from digitize.connectors.config import S3ConnectorConfig

logger = get_logger("s3_scanner")


# ---------------------------------------------------------------------------
# Inline MD5 write wrapper
# ---------------------------------------------------------------------------

class _HashingWriter(io.RawIOBase):
    """
    Write-only file-like object that tees bytes to a destination file handle
    while computing an MD5 digest inline in a single streaming pass.

    Passed as ``Fileobj`` to ``boto3.client.download_fileobj()``.  boto3 calls
    ``write(chunk)`` for every arriving network chunk; this wrapper:
      - updates a running MD5 state with each chunk
      - forwards every byte to the underlying real file handle

    The full-file MD5 is available via ``hexdigest`` the instant
    ``download_fileobj()`` returns — no second read of the staged file needed.
    """

    def __init__(self, dest_fh: io.BufferedWriter) -> None:
        super().__init__()
        self._dest = dest_fh
        self._md5 = hashlib.md5()

    def write(self, b: bytes) -> int:  # type: ignore[override]
        self._md5.update(b)
        return self._dest.write(b)

    def readable(self) -> bool:
        return False

    def writable(self) -> bool:
        return True

    @property
    def hexdigest(self) -> str:
        """Hex MD5 of all bytes written so far."""
        return self._md5.hexdigest()


# ---------------------------------------------------------------------------
# S3Scanner
# ---------------------------------------------------------------------------

class S3Scanner(BaseScanner):
    """
    Scanner for AWS S3 and IBM COS (S3-compatible) sources.

    One instance is created per sync tick by the worker via build_scanner().
    The boto3 client is built lazily in connect().

    Parameters
    ----------
    config:
        S3ConnectorConfig populated from the connector's connection_details.
    """

    def __init__(self, config: S3ConnectorConfig) -> None:
        super().__init__(config)
        self._cfg: S3ConnectorConfig = config
        self._client: Optional[object] = None  # set in connect()

    # ------------------------------------------------------------------ #
    # Lifecycle                                                            #
    # ------------------------------------------------------------------ #

    def connect(self) -> None:
        """Build the boto3 S3 client and verify the bucket is reachable.

        Runs ``head_bucket`` as a pre-flight check immediately after building
        the client so credential or endpoint errors are raised here — at the
        point the caller expects a connection to be established — rather than
        surfacing later inside scan() or download_to().

        Raises
        ------
        ConnectionError
            If the bucket is unreachable or credentials are rejected.
        """
        self._client = self._build_client()
        try:
            self._head_bucket(self._client)
        except ConnectionError:
            self._client = None
            raise
        logger.info(
            f"[s3_scanner] Connected — bucket={self._cfg.bucket_name}, "
            f"provider={self._cfg.provider}, region={self._cfg.effective_region}, "
            f"prefix={self._cfg.prefix!r}"
        )

    def close(self) -> None:
        """No-op for S3 — boto3 clients are stateless; underlying urllib3 pool
        is garbage-collected when the client object is released."""
        self._client = None
        logger.debug("[s3_scanner] Client released.")

    # ------------------------------------------------------------------ #
    # Core interface                                                       #
    # ------------------------------------------------------------------ #

    def scan(self) -> list[tuple[str, str]]:
        """
        Return ``(key, checksum)`` for all supported documents in the bucket.

        ``checksum`` is the raw S3 ETag (quotes stripped), available for free
        from ``list_objects_v2`` with no extra API call.

        No dedup filtering is applied — the worker's _classify() receives the
        full list and decides what to ingest, skip, or mark as an orphan.

        Returns
        -------
        list[tuple[str, str]]
            All (key, etag) pairs for documents with allowed extensions.
            Empty list if the bucket/prefix contains no matching objects.
        """
        self._require_connected()
        all_files = list(self._list_document_keys())
        logger.info(
            f"[s3_scanner] scan complete — "
            f"{len(all_files)} document(s) found in "
            f"s3://{self._cfg.bucket_name}/{self._cfg.prefix or ''}"
        )
        return all_files

    def download_to(self, remote_path: str, local_path: Path) -> str:
        """
        Download the S3 object at ``remote_path`` to ``local_path``.

        Returns the full-file MD5 hex digest computed inline during the
        transfer via ``_HashingWriter`` — no second file read is needed.

        Parameters
        ----------
        remote_path:
            S3 object key as returned by scan().
        local_path:
            Absolute path where the file bytes should be written.

        Returns
        -------
        str
            Hex MD5 digest of the downloaded bytes.

        Raises
        ------
        botocore.exceptions.ClientError
            On S3 API failure.
        OSError
            On local file system error.
        """
        self._require_connected()
        logger.debug(
            f"[s3_scanner] Downloading s3://{self._cfg.bucket_name}/{remote_path} "
            f"→ {local_path}"
        )
        with open(local_path, "wb") as fh:
            writer = _HashingWriter(fh)
            self._client.download_fileobj(
                Bucket=self._cfg.bucket_name,
                Key=remote_path,
                Fileobj=writer,
            )

        local_md5 = writer.hexdigest
        logger.debug(
            f"[s3_scanner] Downloaded {local_path.name} — "
            f"local_md5={local_md5[:12]}… size={local_path.stat().st_size} bytes"
        )
        return local_md5

    # ------------------------------------------------------------------ #
    # Private helpers                                                      #
    # ------------------------------------------------------------------ #

    def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
        """
        S3-aware integrity check.

        Multi-part ETags have the form ``<hex>-<N>`` — the hash is
        ``MD5(raw_concatenated_part_md5s)`` and cannot be reproduced from
        the full file bytes alone.  The check is skipped for those objects
        and ``True`` is returned.

        For single-part ETags the base-class direct equality check is used.
        """
        if "-" in remote_checksum:
            logger.debug(
                "[s3_scanner] Skipping integrity check for multi-part object "
                "(etag=%r)", remote_checksum,
            )
            return True
        return super().verify_integrity(local_checksum, remote_checksum)

    def _build_client(self):
        """
        Construct a boto3 S3 client from the connector config.

        The region is always derived from endpoint_url (via effective_region) for
        correct SigV4 signing, but endpoint_url is only forwarded to boto3 for
        IBM COS / S3-compatible stores — never for AWS S3.

        AWS S3: endpoint_url must NOT be passed to boto3.  When an explicit URL
        is supplied boto3 prepends the bucket name to the full hostname, producing
        a malformed double-domain URL that AWS rejects with 301 Moved Permanently.
        boto3 auto-resolves the correct virtual-hosted endpoint from region_name.

        IBM COS: endpoint_url must be forwarded so boto3 knows the COS host.
        Path-style addressing is used because COS requires it.
        """
        session = boto3.Session(
            aws_access_key_id=self._cfg.access_key_id or None,
            aws_secret_access_key=self._cfg.secret_access_key or None,
            region_name=self._cfg.effective_region,
        )

        addressing_style = "auto" if self._cfg.is_aws else "path"

        client_kwargs: dict = {
            "service_name": "s3",
            "config": botocore.config.Config(
                max_pool_connections=self._cfg.download_concurrency,
                retries={"max_attempts": 3, "mode": "standard"},
                signature_version="s3v4",
                s3={"addressing_style": addressing_style},
            ),
            "verify": self._cfg.verify_ssl,
        }

        # Forward endpoint_url only for non-AWS providers.
        # For AWS the region extracted from endpoint_url is passed via
        # region_name above — the URL itself is intentionally withheld.
        if self._cfg.endpoint_url and not self._cfg.is_aws:
            client_kwargs["endpoint_url"] = self._cfg.endpoint_url

        return session.client(**client_kwargs)

    def _list_document_keys(self) -> Iterator[Tuple[str, str]]:
        """
        Yield ``(key, etag)`` for each supported document under the configured prefix.

        ETag is the raw value returned by S3 (quotes stripped).  Available
        for free in the list_objects_v2 response — no head_object needed.

        Files whose extension is not in ``allowed_extensions`` are skipped.
        CommonPrefixes (collapsed sub-folders when a delimiter is active) are
        logged and skipped.
        """
        paginator = self._client.get_paginator("list_objects_v2")

        # Prefix="" is safe to pass — S3 treats it as "no prefix" (bucket root).
        # Delimiter="" must NOT be passed — S3/COS treats an empty string as a
        # live delimiter, unlike omitting the key entirely which means "recurse".
        paginate_kwargs: dict = {
            "Bucket": self._cfg.bucket_name,
            "Prefix": self._cfg.prefix,
            **({"Delimiter": self._cfg.delimiter} if self._cfg.delimiter else {}),
        }

        allowed = frozenset(ext.lower() for ext in self._cfg.allowed_extensions)

        for page in paginator.paginate(**paginate_kwargs):
            for cp in page.get("CommonPrefixes", []):
                logger.debug(
                    f"[s3_scanner] Delimiter '{self._cfg.delimiter}' collapsed "
                    f"sub-folder: {cp.get('Prefix', '')!r} — skipped"
                )
            for obj in page.get("Contents", []):
                key: str = obj["Key"]
                ext = os.path.splitext(key)[1].lower()
                if ext in allowed:
                    etag: str = obj.get("ETag", "").strip('"')
                    yield key, etag
                else:
                    logger.debug(f"[s3_scanner] Skipping non-document key: {key!r}")

    def _require_connected(self) -> None:
        if self._client is None:
            raise RuntimeError(
                "S3Scanner.connect() must be called before scan() or download_to()."
            )

    # ------------------------------------------------------------------ #
    # Connection test (utility — used by connector CRUD attach flow)      #
    # ------------------------------------------------------------------ #

    def test_connection(self) -> bool:
        """
        Return True if the bucket is reachable with the configured credentials.

        Calls ``head_bucket`` which requires only s3:ListBucket permission and
        transfers no object data.  Uses the existing client when already
        connected, otherwise builds a temporary one.
        """
        try:
            self._head_bucket(self._client or self._build_client())
            return True
        except ConnectionError:
            return False

    # ------------------------------------------------------------------ #
    # Private helpers                                                      #
    # ------------------------------------------------------------------ #

    def _head_bucket(self, client) -> None:
        """Call head_bucket and raise ConnectionError on failure.

        Shared by connect() (raises) and test_connection() (catches and
        returns bool).  Keeps the bucket-reachability check in one place.

        Raises
        ------
        ConnectionError
            Wraps botocore.exceptions.ClientError with bucket name and HTTP
            code in the message.
        """
        try:
            client.head_bucket(Bucket=self._cfg.bucket_name)
            logger.info(
                "[s3_scanner] head_bucket OK — bucket=%s",
                self._cfg.bucket_name,
            )
        except botocore.exceptions.ClientError as exc:
            code = exc.response["Error"]["Code"]
            raise ConnectionError(
                f"[s3_scanner] Cannot reach bucket '{self._cfg.bucket_name}' "
                f"(code={code}): {exc}"
            ) from exc
