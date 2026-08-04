"""
connector/base_scanner.py — transport-agnostic scanner interface.

Every concrete scanner (S3, SFTP, …) must subclass BaseScanner and implement
the four abstract methods.

Responsibility boundary
-----------------------
Scanners are responsible for:
  * connecting to / disconnecting from the remote source
  * listing ALL remote files and computing their checksum
  * downloading a single file to a local staging path

Scanners are NOT responsible for:
  * dedup classification (skip / cross-connector dup / brand-new)  → worker
  * orphan detection                                                → worker
  * writing to connector_document_checksum                         → worker
  * any DB operation except set_document_metadata (source_checksum)

Usage pattern (called by ConnectorSyncWorker — not yet implemented):
    scanner = build_scanner(connector_config)
    scanner.connect()
    try:
        all_files: list[tuple[str, str]] = scanner.scan()
        # worker classifies all_files against known/all checksums
        for remote_path, checksum in ingest_list:
            local_path = staging_dir / Path(remote_path).name
            local_checksum = scanner.download_to(remote_path, local_path)
            # local_checksum can be compared against checksum for integrity
    finally:
        scanner.close()
"""

from __future__ import annotations

import logging
from abc import ABC, abstractmethod
from pathlib import Path

logger = logging.getLogger(__name__)


class BaseScanner(ABC):
    """
    Abstract base for all data-source scanners.

    Subclasses implement transport-specific listing, hashing, and download.
    The base class holds no state — every instance is created fresh per tick
    by the worker via the scanner factory.

    Parameters
    ----------
    config:
        Connector configuration dataclass for this scanner type
        (e.g. S3ConnectorConfig).  Typed as ``object`` here so the base class
        does not depend on any specific config type.
    """

    def __init__(self, config: object) -> None:
        self._config = config

    # ------------------------------------------------------------------ #
    # Lifecycle — must be called in order: connect → scan/download → close
    # ------------------------------------------------------------------ #

    @abstractmethod
    def connect(self) -> None:
        """
        Open the connection to the remote source.

        Called once per tick before any scan() or download_to() call.
        Implementations should authenticate and establish any required session
        objects (boto3 client, Paramiko transport, etc.).

        Raises
        ------
        Exception
            Any transport-level error (auth failure, unreachable host, …).
            The worker treats this as a tick failure and records it in
            connector_sync_log.
        """

    @abstractmethod
    def scan(self) -> list[tuple[str, str]]:
        """
        Return (remote_path, checksum) for **all** files found on the source.

        No dedup filtering is applied here.  The full list is returned so
        the worker's _classify() can compare it against the known and global
        checksum sets and decide which files to ingest, which to skip, and
        which are orphans.

        ``remote_path`` is the canonical address used to re-download the file
        (S3 object key, SFTP absolute path, etc.).

        ``checksum`` is the content fingerprint:
          - S3:   S3 ETag from list_objects_v2 (MD5 for single-part;
                  MD5(raw_part_md5s)-N for multi-part)
          - SFTP: MD5 hex digest computed on the remote host via md5sum

        Returns
        -------
        list[tuple[str, str]]
            Ordered list of (remote_path, checksum) pairs.  Empty list if
            the remote source contains no supported documents.
        """

    @abstractmethod
    def download_to(self, remote_path: str, local_path: Path) -> str:
        """
        Download the file at ``remote_path`` to ``local_path``.

        Returns
        -------
        str
            Hex digest of the downloaded file computed inline during the
            transfer (no second read required).  The caller may use this to
            verify integrity against the checksum returned by scan().

        Parameters
        ----------
        remote_path:
            The canonical remote address as returned by scan().
        local_path:
            Absolute path where the file should be written.  The parent
            directory is guaranteed to exist (created by the worker).

        Raises
        ------
        Exception
            Any download error.  The worker catches this per-file, increments
            the failed_files counter, and continues with the next file.
        """

    @abstractmethod
    def close(self) -> None:
        """
        Release any held resources (connections, file handles, temp dirs).

        Called in the worker's ``finally`` block — always runs even if
        scan() or download_to() raised.  Implementations must be safe to
        call even when connect() was never successfully called.
        """

    # ------------------------------------------------------------------ #
    # Integrity verification — concrete, overridable                      #
    # ------------------------------------------------------------------ #

    def verify_integrity(self, local_checksum: str, remote_checksum: str) -> bool:
        """
        Verify a downloaded file's integrity against the remote checksum.

        The base implementation performs a direct equality check, which is
        correct for any transport that provides a plain hex digest as the
        checksum (e.g. SFTP/SSH ``md5sum`` output).

        Subclasses may override this method to handle transport-specific
        checksum formats.  ``S3Scanner`` overrides it to skip the check for
        multi-part ETags (``<hex>-N``), which cannot be reproduced locally.

        Parameters
        ----------
        local_checksum:
            Hex digest of the downloaded file, computed inline during
            ``download_to()`` with no second file read.
        remote_checksum:
            Checksum as returned by ``scan()`` for the same file.

        Returns
        -------
        bool
            ``True`` if the file is intact, ``False`` if corrupt.
        """
        match = local_checksum == remote_checksum
        if match:
            logger.debug(
                "verify_integrity OK — checksum=%s…", local_checksum[:12]
            )
        else:
            logger.error(
                "verify_integrity FAILED — local=%r, remote=%r",
                local_checksum, remote_checksum,
            )
        return match
