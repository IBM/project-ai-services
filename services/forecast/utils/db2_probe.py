"""
Db2 for i connection and schema probe using Mapepire.

Validates remote database connectivity, credentials, and table/column schema
existence before a connector is created or updated in PostgreSQL.
"""
from __future__ import annotations

import ipaddress
import socket
from typing import Optional, Sequence

from fastapi import HTTPException

from common.error_utils import APIError, ErrorCode
from common.misc_utils import get_logger
from forecast.settings import settings

logger = get_logger("forecast_probe")

DEFAULT_PROBE_TIMEOUT_S = 5
DEFAULT_MAPEPIRE_PORT = 8076


def _validate_target_host(host: str, port: int) -> None:
    """Resolve target host and reject loopback, link-local, unspecified, or multicast addresses."""
    if host.lower() in ("localhost", "localhost.localdomain"):
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            f"Target host '{host}' is not a permitted destination.",
        )

    try:
        addr_infos = socket.getaddrinfo(host, port, proto=socket.IPPROTO_TCP)
    except socket.gaierror as exc:
        logger.warning("DNS resolution failed for host %s: %s", host, exc)
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            f"Cannot resolve host '{host}'.",
        )

    for family, socktype, proto, canonname, sockaddr in addr_infos:
        ip_str = sockaddr[0]
        try:
            ip = ipaddress.ip_address(ip_str)
            if ip.is_loopback or ip.is_link_local or ip.is_multicast or ip.is_unspecified or ip.is_reserved:
                APIError.raise_error(
                    ErrorCode.INVALID_REQUEST,
                    f"Target host '{host}' resolves to non-permitted address '{ip_str}'.",
                )
        except ValueError:
            pass


def _tcp_preflight(host: str, port: int, timeout: int) -> None:
    """Perform a fast TCP connection pre-flight to verify host reachability with a timeout."""
    try:
        with socket.create_connection((host, port), timeout=timeout):
            pass
    except OSError as exc:
        logger.warning("Db2 probe TCP pre-flight failed for %s:%s (%s)", host, port, type(exc).__name__)
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            f"Cannot connect to Db2 at {host}:{port}: host unreachable or connection timed out.",
        )


def probe_db2_connector(
    *,
    host: str,
    database: str,
    username: str,
    password: str,
    port: Optional[int] = None,
    table: Optional[str] = None,
    columns: Optional[Sequence[str]] = None,
    timeout: int = DEFAULT_PROBE_TIMEOUT_S,
) -> None:
    """Probe Db2 for i connection, authentication, and table/column existence.

    Raises APIError(ErrorCode.INVALID_REQUEST) if connectivity,
    authentication, or table/column schema inspection fails.
    Never logs or leaks credentials.
    """
    effective_port = port or DEFAULT_MAPEPIRE_PORT

    _validate_target_host(host, effective_port)

    try:
        import mapepire_python  # noqa: PLC0415
    except ImportError:
        logger.warning(
            "mapepire-python is not installed; skipping live Db2 connection probe."
        )
        return

    _tcp_preflight(host, effective_port, timeout)

    logger.debug(
        "Probing Db2 for i via Mapepire: host=%s port=%s user=%s table=%s",
        host,
        effective_port,
        username,
        table,
    )

    server = mapepire_python.DaemonServer(
        host=host,
        user=username,
        password=password,
        port=effective_port,
        ignoreUnauthorized=not settings.forecast.db2_verify_tls,
        ca=str(settings.forecast.db2_ca_cert_path) if settings.forecast.db2_ca_cert_path else None,
    )

    conn = None
    try:
        conn = mapepire_python.connect(connection_details=server)
        cursor = conn.cursor()
        try:
            # 1. Ping / authentication check
            cursor.execute("SELECT 1 FROM SYSIBM.SYSDUMMY1")
            cursor.fetchall()

            # 2. Schema check if table and columns are provided
            if table:
                parts = table.split(".", 1)
                schema_name = parts[0].strip().upper() if len(parts) == 2 else None
                table_name = (parts[1] if len(parts) == 2 else parts[0]).strip().upper()

                if schema_name:
                    cursor.execute(
                        """
                        SELECT COLUMN_NAME
                        FROM QSYS2.SYSCOLUMNS
                        WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
                        """,
                        [schema_name, table_name],
                    )
                    rows = cursor.fetchall()
                    if not rows:
                        APIError.raise_error(
                            ErrorCode.INVALID_REQUEST,
                            f"Table '{schema_name}.{table_name}' not found on Db2 host '{host}' or not accessible for user '{username}'.",
                        )
                    existing_cols = {row[0].strip().upper() for row in rows}
                else:
                    cursor.execute(
                        """
                        SELECT TABLE_SCHEMA, COLUMN_NAME
                        FROM QSYS2.SYSCOLUMNS
                        WHERE TABLE_NAME = ?
                        """,
                        [table_name],
                    )
                    rows = cursor.fetchall()
                    if not rows:
                        APIError.raise_error(
                            ErrorCode.INVALID_REQUEST,
                            f"Table '{table_name}' not found on Db2 host '{host}' or not accessible for user '{username}'.",
                        )
                    schemas = {row[0].strip().upper() for row in rows}
                    if len(schemas) > 1:
                        APIError.raise_error(
                            ErrorCode.INVALID_REQUEST,
                            f"Table '{table_name}' exists in multiple schemas ({', '.join(schemas)}). Please specify SCHEMA.TABLE.",
                        )
                    existing_cols = {row[1].strip().upper() for row in rows}
                if columns:
                    missing = [c for c in columns if c.strip().upper() not in existing_cols]
                    if missing:
                        missing_str = ", ".join(f"'{m}'" for m in missing)
                        APIError.raise_error(
                            ErrorCode.INVALID_REQUEST,
                            f"Columns {missing_str} not found in table '{table}' on Db2 host '{host}'.",
                        )
        finally:
            cursor.close()
    except HTTPException:
        raise
    except Exception as exc:
        err_str = str(exc).lower()
        logger.warning("Db2 connection probe failed for host %s: %s", host, type(exc).__name__)
        if any(
            kw in err_str
            for kw in (
                "password",
                "auth",
                "credentials",
                "not authorized",
                "user id",
                "unauthorized",
            )
        ):
            APIError.raise_error(
                ErrorCode.INVALID_REQUEST,
                f"Authentication failed for user '{username}' on Db2 host '{host}'. Please verify credentials.",
            )
        APIError.raise_error(
            ErrorCode.INVALID_REQUEST,
            f"Cannot connect to Db2 at {host}:{effective_port}. Check credentials and network connectivity.",
        )
    finally:
        if conn is not None:
            try:
                conn.close()
            except Exception:
                pass
