"""
connectors/hashing.py — streaming hash utility for connector download paths.

``HashingWriter`` is transport-agnostic: it works with any caller that writes
bytes in chunks (boto3 ``download_fileobj``, Paramiko SFTP ``readinto``, etc.).
"""

from __future__ import annotations

import hashlib
import io


class HashingWriter(io.RawIOBase):
    """
    Write-only file-like object that tees bytes to a destination file handle
    while computing an MD5 digest inline in a single streaming pass.

    Usage with boto3::

        with open(local_path, "wb") as fh:
            writer = HashingWriter(fh)
            client.download_fileobj(Bucket=bucket, Key=key, Fileobj=writer)
        md5 = writer.hexdigest

    Usage with Paramiko SFTP::

        with open(local_path, "wb") as fh:
            writer = HashingWriter(fh)
            sftp_file.readinto(writer)
        md5 = writer.hexdigest

    The full-file MD5 is available via ``hexdigest`` the instant the transfer
    call returns — no second read of the staged file needed.
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
