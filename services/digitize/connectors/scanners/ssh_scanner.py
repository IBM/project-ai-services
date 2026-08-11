from __future__ import annotations

import io
import os
import stat
from pathlib import Path
from typing import Iterator, Optional

import paramiko

from common.misc_utils import get_logger
from digitize.connectors.scanners.base_scanner import BaseScanner
from digitize.connectors.scanners.config import SFTPConnectorConfig
from digitize.connectors.scanners.hashing import HashingWriter

logger = get_logger("ssh_scanner")


class SSHScanner(BaseScanner):
    def __init__(self, config: SFTPConnectorConfig) -> None:
        super().__init__(config)
        self._cfg: SFTPConnectorConfig = config
        self._ssh: Optional[paramiko.SSHClient] = None
        self._sftp: Optional[paramiko.SFTPClient] = None

    def connect(self) -> None:
        try:
            pkey = self._load_private_key(self._cfg.private_key_pem)
        except (paramiko.SSHException, ValueError) as exc:
            raise ConnectionError(
                f"[ssh_scanner] Failed to load private key for {self._cfg.host}: {exc}"
            ) from exc

        ssh = paramiko.SSHClient()
        ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        try:
            ssh.connect(
                hostname=self._cfg.host,
                port=self._cfg.port,
                username=self._cfg.username,
                pkey=pkey,
                look_for_keys=False,
                allow_agent=False,
            )
        except (paramiko.AuthenticationException, paramiko.SSHException, OSError) as exc:
            raise ConnectionError(
                f"[ssh_scanner] Cannot connect to {self._cfg.host}:{self._cfg.port} "
                f"as '{self._cfg.username}': {exc}"
            ) from exc

        self._ssh = ssh
        self._sftp = ssh.open_sftp()
        logger.info(
            "[ssh_scanner] Connected — host=%s port=%d user=%s remote_path=%r",
            self._cfg.host, self._cfg.port, self._cfg.username, self._cfg.remote_path,
        )

    def close(self) -> None:
        if self._sftp is not None:
            try:
                self._sftp.close()
            except Exception:
                pass
            self._sftp = None
        if self._ssh is not None:
            try:
                self._ssh.close()
            except Exception:
                pass
            self._ssh = None
        logger.debug("[ssh_scanner] Connection closed.")

    def scan(self) -> list[tuple[str, str]]:
        self._require_connected()
        allowed = frozenset(self._cfg.allowed_extensions)
        found: list[tuple[str, str]] = []
        for remote_path in self._walk_remote_tree(self._cfg.remote_path):
            if os.path.splitext(remote_path)[1].lower() not in allowed:
                logger.debug("[ssh_scanner] Skipping: %r", remote_path)
                continue
            found.append((remote_path, self._remote_md5(remote_path)))
        logger.info(
            "[ssh_scanner] scan complete — %d document(s) found under %r",
            len(found), self._cfg.remote_path,
        )
        return found

    def download_to(self, remote_path: str, local_path: Path) -> str:
        self._require_connected()
        logger.debug("[ssh_scanner] Downloading sftp://%s%s → %s",
                     self._cfg.host, remote_path, local_path)
        with open(local_path, "wb") as fh:
            writer = HashingWriter(fh)
            self._sftp.getfo(remote_path, writer)
        local_md5 = writer.hexdigest
        logger.debug("[ssh_scanner] Downloaded %s — local_md5=%s… size=%d bytes",
                     local_path.name, local_md5[:12], local_path.stat().st_size)
        return local_md5

    def _remote_md5(self, remote_file_path: str) -> str:
        _, stdout, stderr = self._ssh.exec_command(f'md5sum "{remote_file_path}"')
        output = stdout.read().decode().strip()
        error_output = stderr.read().decode().strip()
        exit_status = stdout.channel.recv_exit_status()
        if exit_status != 0:
            raise RuntimeError(
                f"[ssh_scanner] Failed to compute md5 for {remote_file_path!r}: "
                f"stdout={output!r} stderr={error_output!r}"
            )
        return output.split()[0]

    def _walk_remote_tree(self, path: str) -> Iterator[str]:
        try:
            entries = self._sftp.listdir_attr(path)
        except IOError as exc:
            logger.warning("[ssh_scanner] Cannot list %r: %s", path, exc)
            return
        for entry in entries:
            full_path = path.rstrip("/") + "/" + entry.filename
            if stat.S_ISDIR(entry.st_mode):
                yield from self._walk_remote_tree(full_path)
            else:
                yield full_path

    def _require_connected(self) -> None:
        if self._sftp is None or self._ssh is None:
            raise RuntimeError(
                "SSHScanner.connect() must be called before scan() or download_to()."
            )

    @staticmethod
    def _load_private_key(pem: str) -> paramiko.PKey:
        buf = io.StringIO(pem)
        for key_cls in (paramiko.RSAKey, paramiko.ECDSAKey, paramiko.Ed25519Key):
            buf.seek(0)
            try:
                return key_cls.from_private_key(buf)
            except paramiko.SSHException:
                continue
        raise paramiko.SSHException("Cannot parse private key: not RSA, ECDSA, or Ed25519.")
