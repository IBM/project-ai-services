"""
Db2 client and query builder for IBM Db2 (Db2 for i / Db2 LUW).

Provides connection management, schema inspection, identifier quoting,
and server-side streaming cursor execution.
"""
from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Any, Iterator, List, Optional, Sequence, Tuple

from forecast.data_loader.exceptions import (
    DBAuthError,
    DBColumnNotFoundError,
    DBConnectionError,
    DBTableNotFoundError,
    InvalidTableReferenceError,
)
from forecast.settings import settings

logger = logging.getLogger(__name__)


@dataclass
class ColumnInfo:
    """Metadata for a single database column.

    ``name`` is the catalogue spelling. ``table_schema`` / ``table_name`` are the catalogue
    spelling of the owning table (``table_schema`` is None when the table was requested
    without a schema); they are not part of equality.
    """

    name: str
    type: str
    nullable: bool = True
    table_schema: Optional[str] = field(default=None, compare=False)
    table_name: Optional[str] = field(default=None, compare=False)

    @property
    def table_ref(self) -> Optional[str]:
        """Catalogue-spelled 'SCHEMA.TABLE' or 'TABLE' reference, or None if unknown."""
        if self.table_name is None:
            return None
        if self.table_schema is None:
            return self.table_name
        return f"{self.table_schema}.{self.table_name}"


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
        port: int = settings.forecast.mapepire_port,
        connect_timeout: int = settings.forecast.connector_connect_timeout_s,
        query_timeout: int = settings.forecast.connector_query_timeout_s,
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

    @staticmethod
    def split_table_ref(table: str) -> Tuple[Optional[str], str]:
        """Split a 'SCHEMA.TABLE' or 'TABLE' reference into (schema, table).

        ``schema`` is None when the reference has no schema part; Db2 then resolves the
        table against the session's current schema.
        """
        parts = [part.strip() for part in (table or "").strip().split(".")]
        if len(parts) > 2 or not all(parts):
            raise InvalidTableReferenceError(
                f"Table reference '{table}' must be 'TABLE' or 'SCHEMA.TABLE'"
            )
        if len(parts) == 1:
            return None, parts[0]
        return parts[0], parts[1]

    def list_columns(self, table: str) -> List[ColumnInfo]:
        """Retrieve column names, data types, and nullability for a 'SCHEMA.TABLE' or 'TABLE'.

        The table is matched case-insensitively and the columns carry the catalogue
        spelling of the table and column names, which is what must be quoted in queries
        (quoted identifiers are case-sensitive in Db2). If several tables differ only by
        case, the one spelled exactly as requested is used; otherwise the reference is
        ambiguous and rejected. An unqualified table is looked up in the session's
        current schema.
        """
        schema, name = self.split_table_ref(table)
        conn = self._get_connection()
        cursor = conn.cursor()

        try:
            if schema is None:
                cursor.execute(
                    """
                    SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE
                    FROM QSYS2.SYSCOLUMNS
                    WHERE TABLE_SCHEMA = CURRENT SCHEMA AND UPPER(TABLE_NAME) = ?
                    ORDER BY TABLE_NAME, ORDINAL_POSITION
                    """,
                    [name.upper()],
                )
            else:
                cursor.execute(
                    """
                    SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE
                    FROM QSYS2.SYSCOLUMNS
                    WHERE UPPER(TABLE_SCHEMA) = ? AND UPPER(TABLE_NAME) = ?
                    ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
                    """,
                    [schema.upper(), name.upper()],
                )
            rows = cursor.fetchall()
        except Exception as exc:
            raise DBConnectionError(f"Failed to query columns for table '{table}': {exc}") from exc
        finally:
            cursor.close()

        if not rows:
            raise DBTableNotFoundError(
                f"Table '{table}' not found or not accessible for user '{self.username}'"
            )

        # Group by catalogue (schema, table); more than one group means names differing by case.
        tables: dict[Tuple[str, str], list] = {}
        for row in rows:
            tables.setdefault((row[0].strip(), row[1].strip()), []).append(row)
        if len(tables) > 1:
            exact = [key for key in tables if key[1] == name and (schema is None or key[0] == schema)]
            if len(exact) != 1:
                raise InvalidTableReferenceError(
                    f"Table reference '{table}' matches several tables that differ only by case: "
                    f"{sorted('.'.join(key) for key in tables)}. Use the exact spelling."
                )
            tables = {exact[0]: tables[exact[0]]}
        (table_schema, table_name), table_rows = next(iter(tables.items()))

        return [
            ColumnInfo(
                name=row[2].strip(),
                type=row[3].strip() if row[3] else "VARCHAR",
                nullable=str(row[4]).strip().upper() in ("Y", "YES", "TRUE", "1"),
                table_schema=None if schema is None else table_schema,
                table_name=table_name,
            )
            for row in table_rows
        ]

    @staticmethod
    def quote_identifier(identifier: str) -> str:
        """Safely quote a SQL identifier against injection and keyword clashes."""
        sanitized = identifier.replace('"', '""')
        return f'"{sanitized}"'

    def build_select_query(
        self,
        table: str,
        columns: Sequence[str],
        where_clause: Optional[str] = None,
        batch_size: int = settings.forecast.loader_chunk_rows,
    ) -> str:
        """Build an optimized, safe SELECT SQL statement for streaming reads.

        ``table`` is 'SCHEMA.TABLE' or 'TABLE'; an unqualified table is resolved by Db2
        against the session's current schema.
        """
        schema, name = self.split_table_ref(table)
        quoted_cols = ", ".join(self.quote_identifier(c) for c in columns)
        table_ref = self.quote_identifier(name)
        if schema is not None:
            table_ref = f"{self.quote_identifier(schema)}.{table_ref}"

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
        batch_size: int = settings.forecast.loader_chunk_rows,
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
