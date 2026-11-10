"""
Db2 client and query builder for IBM Db2 (Db2 for i / Db2 LUW).

Provides connection management, schema inspection, identifier quoting,
and server-side streaming cursor execution.
"""
from __future__ import annotations

import logging
from dataclasses import dataclass
from typing import Any, Iterator, List, Optional, Sequence

from forecast.data_loader.config import (
    DEFAULT_CONNECT_TIMEOUT_S,
    DEFAULT_LOADER_CHUNK_ROWS,
    DEFAULT_MAPEPIRE_PORT,
    DEFAULT_QUERY_TIMEOUT_S,
)
from forecast.data_loader.exceptions import (
    DBAuthError,
    DBColumnNotFoundError,
    DBConnectionError,
    DBTableNotFoundError,
)

logger = logging.getLogger(__name__)


@dataclass
class ColumnInfo:
    """Metadata for a single database column."""

    name: str
    type: str
    nullable: bool = True


class Db2Client:
    """Client for interacting with IBM Db2 databases.

    Supports connection testing, metadata inspection, and streaming batch extraction.
    """

    def __init__(
        self,
        host: str,
        database: str,
        username: str,
        password: str,
        port: int = DEFAULT_MAPEPIRE_PORT,
        connect_timeout: int = DEFAULT_CONNECT_TIMEOUT_S,
        query_timeout: int = DEFAULT_QUERY_TIMEOUT_S,
        ignore_unauthorized: bool = False,
        ca_cert: Optional[str | bytes] = None,
        custom_connection: Optional[Any] = None,
    ) -> None:
        self.host = host
        self.database = database
        self.username = username
        self.password = password
        self.port = port
        self.connect_timeout = connect_timeout
        self.query_timeout = query_timeout
        self.ignore_unauthorized = ignore_unauthorized
        self.ca_cert = ca_cert
        self._conn = custom_connection

    def _get_connection(self) -> Any:
        """Return or establish the database connection via Mapepire."""
        if self._conn is not None:
            return self._conn

        try:
            import mapepire_python  # noqa: PLC0415
            import mapepire_python.client.websocket_client as mapepire_ws  # noqa: PLC0415
            from websockets.sync.client import connect as ws_sync_connect  # noqa: PLC0415

            # The default websockets limit of 1 MiB causes error 1009 (message too big)
            # when multi-thousand row batches are returned in a single JSON frame.
            # Patch WebsocketConnection.connect to allow up to 64 MiB frames.
            if not getattr(mapepire_ws.WebsocketConnection, "_max_size_configured", False):
                def _configured_ws_connect(ws_self):
                    return ws_sync_connect(
                        ws_self.uri,
                        additional_headers=ws_self.headers,
                        open_timeout=10,
                        ssl=ws_self._create_ssl_context(ws_self.db2_server),
                        max_size=64 * 1024 * 1024,
                    )

                mapepire_ws.WebsocketConnection.connect = _configured_ws_connect
                mapepire_ws.WebsocketConnection._max_size_configured = True
        except ImportError as exc:
            raise DBConnectionError(
                "mapepire-python is not installed. Required for IBM i Db2 connectivity: "
                "pip install mapepire-python"
            ) from exc

        logger.debug(
            "Connecting to IBM i Db2 via Mapepire: host=%s port=%s user=%s",
            self.host,
            self.port,
            self.username,
        )

        server = mapepire_python.DaemonServer(
            host=self.host,
            user=self.username,
            password=self.password,
            port=self.port,
            ignoreUnauthorized=self.ignore_unauthorized,
            ca=self.ca_cert,
        )

        try:
            self._conn = mapepire_python.connect(connection_details=server)
            return self._conn
        except Exception as exc:
            err_str = str(exc).lower()
            logger.error("Db2 connection failure on %s:%s - %s", self.host, self.port, exc)
            if any(kw in err_str for kw in ("password", "auth", "credentials", "not authorized", "user id", "unauthorized")):
                raise DBAuthError(
                    f"Authentication failed for user '{self.username}' on Db2 host '{self.host}'"
                ) from exc
            raise DBConnectionError(
                f"Cannot connect to Db2 at {self.host}:{self.port} - {exc}"
            ) from exc

    def test_connection(self) -> bool:
        """Verify database connectivity and credentials with a lightweight ping query."""
        conn = self._get_connection()
        cursor = conn.cursor()
        try:
            cursor.execute("SELECT 1 FROM SYSIBM.SYSDUMMY1")
            cursor.fetchall()
            return True
        except Exception as exc:
            err_str = str(exc).lower()
            if any(kw in err_str for kw in ("password", "auth", "credentials", "not authorized")):
                raise DBAuthError(f"Authentication failed on Db2 host '{self.host}': {exc}") from exc
            raise DBConnectionError(f"Connection test failed on Db2 host '{self.host}': {exc}") from exc
        finally:
            cursor.close()

    def list_tables(self, schema: str) -> List[str]:
        """Fetch all table names in the given schema."""
        conn = self._get_connection()
        cursor = conn.cursor()
        try:
            # Query IBM i QSYS2.SYSTABLES or standard DB2 catalog
            cursor.execute(
                """
                SELECT TABLE_NAME
                FROM QSYS2.SYSTABLES
                WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('T', 'P', 'V')
                ORDER BY TABLE_NAME
                """,
                [schema.upper()],
            )
            rows = cursor.fetchall()
            return [row[0].strip() for row in rows]
        except Exception as exc:
            raise DBConnectionError(f"Failed to list tables in schema '{schema}': {exc}") from exc
        finally:
            cursor.close()

    def list_columns(self, schema: Optional[str], table: str) -> List[ColumnInfo]:
        """Retrieve column names, data types, and nullability for a table."""
        conn = self._get_connection()
        cursor = conn.cursor()
        schema_param = schema.upper() if schema else None
        table_param = table.upper()

        try:
            if schema_param:
                cursor.execute(
                    """
                    SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE
                    FROM QSYS2.SYSCOLUMNS
                    WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
                    ORDER BY ORDINAL_POSITION
                    """,
                    [schema_param, table_param],
                )
            else:
                cursor.execute(
                    """
                    SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE
                    FROM QSYS2.SYSCOLUMNS
                    WHERE TABLE_NAME = ?
                    ORDER BY ORDINAL_POSITION
                    """,
                    [table_param],
                )
            rows = cursor.fetchall()
        except Exception as exc:
            raise DBConnectionError(f"Failed to query columns for table '{table}': {exc}") from exc
        finally:
            cursor.close()

        if not rows:
            qual = f"{schema}.{table}" if schema else table
            raise DBTableNotFoundError(
                f"Table '{qual}' not found or not accessible for user '{self.username}'"
            )

        return [
            ColumnInfo(
                name=row[0].strip(),
                type=row[1].strip() if row[1] else "VARCHAR",
                nullable=str(row[2]).strip().upper() in ("Y", "YES", "TRUE", "1"),
            )
            for row in rows
        ]

    @staticmethod
    def quote_identifier(identifier: str) -> str:
        """Safely quote a SQL identifier against injection and keyword clashes."""
        sanitized = identifier.replace('"', '""')
        return f'"{sanitized}"'

    def build_select_query(
        self,
        schema: Optional[str],
        table: str,
        columns: Sequence[str],
        where_clause: Optional[str] = None,
        batch_size: int = DEFAULT_LOADER_CHUNK_ROWS,
    ) -> str:
        """Build an optimized, safe SELECT SQL statement for streaming reads."""
        quoted_cols = ", ".join(self.quote_identifier(c) for c in columns)
        if schema:
            table_ref = f"{self.quote_identifier(schema)}.{self.quote_identifier(table)}"
        else:
            table_ref = self.quote_identifier(table)

        sql = f"SELECT {quoted_cols} FROM {table_ref}"

        if where_clause and where_clause.strip():
            predicate = where_clause.strip()
            if predicate.upper().startswith("WHERE "):
                predicate = predicate[6:].strip()
            sql = f"{sql} WHERE {predicate}"

        # Optimization & non-blocking concurrency hints for Db2
        sql = f"{sql} FOR READ ONLY OPTIMIZE FOR {batch_size} ROWS WITH UR"
        return sql

    def stream_query_batches(
        self,
        query: str,
        batch_size: int = DEFAULT_LOADER_CHUNK_ROWS,
        fetch_size: int = 2500,
    ) -> Iterator[List[Sequence[Any]]]:
        """Execute a query and yield result rows in batches using fetchmany.

        Fetches in chunks of ``fetch_size`` from the WebSocket transport to avoid
        server-side message spikes, and yields accumulated batches up to ``batch_size``.
        """
        conn = self._get_connection()
        cursor = conn.cursor()
        ws_chunk = max(1, min(batch_size, fetch_size))
        try:
            cursor.execute(query)
            current_batch: List[Sequence[Any]] = []
            while True:
                rows = cursor.fetchmany(ws_chunk)
                if not rows:
                    if current_batch:
                        yield current_batch
                    break

                current_batch.extend(rows)
                if len(current_batch) >= batch_size:
                    yield current_batch
                    current_batch = []
        except Exception as exc:
            err_str = str(exc).lower()
            if "not found" in err_str or "does not exist" in err_str or "sql0204" in err_str:
                raise DBTableNotFoundError(f"Database table not found during streaming: {exc}") from exc
            if any(kw in err_str for kw in ("column", "undefined column", "sql0206")):
                raise DBColumnNotFoundError(f"Column error during streaming: {exc}") from exc
            if any(kw in err_str for kw in ("auth", "not authorized", "permission", "sql0551")):
                raise DBAuthError(f"Permission denied during streaming: {exc}") from exc
            raise DBConnectionError(f"Error during Db2 batch streaming: {exc}") from exc
        finally:
            cursor.close()

    def close(self) -> None:
        """Close the active database connection if open."""
        if self._conn is not None:
            try:
                self._conn.close()
                logger.debug("Db2 connection closed.")
            except Exception as exc:
                logger.warning("Error closing Db2 connection: %s", exc)
            finally:
                self._conn = None

    def __enter__(self) -> Db2Client:
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self.close()
