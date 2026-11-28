"""
Unit tests for the Db2 data loader.

The tests run without a Db2 server. The Mapepire connection is replaced by a fake
connection that serves a column catalogue and table rows, so the SQL, the column
resolution, the Parquet output and the error mapping are checked on their own.
"""

import re
import ssl
import warnings
from datetime import date, datetime
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pyarrow as pa
import pyarrow.parquet as pq
import pytest
from pydantic import SecretStr

from forecast.data_loader import (
    ColumnInfo,
    ConnectorDecryptionError,
    Db2Client,
    Db2DataLoader,
    DBAuthError,
    DBColumnNotFoundError,
    DBConnectionError,
    DBTableNotFoundError,
    DBTLSConfigError,
    InvalidTableReferenceError,
    ParquetBatchWriter,
    ParquetWriteError,
    UnsupportedColumnTypeError,
    build_arrow_schema,
    map_db_type_to_arrow,
)
from forecast.data_loader import loader as loader_module
from forecast.settings import settings
from forecast.utils.encryption import EncryptionKeyError, encrypt_secrets

pytestmark = pytest.mark.unit

# (TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE)
CATALOGUE = [
    ("TEST", "ORDERS", "ID", "INTEGER", "N"),
    ("TEST", "ORDERS", "Amount", "DECIMAL", "Y"),
    ("TEST", "ORDERS", "REGION", "VARCHAR", "Y"),
    ("TEST", "ORDERS", "FLAG", "SMALLINT", "Y"),
]
ENCRYPTION_KEY = "0123456789abcdef0123456789abcdef"


class _FakeCursor:
    """Serve catalogue or table rows and record what was asked for."""

    def __init__(self, conn):
        self.conn = conn
        self.rows = []
        self.pos = 0
        self.closed = False

    def execute(self, sql, params=None):
        self.conn.executed.append((" ".join(sql.split()), params))
        if self.conn.fail_on and self.conn.fail_on[0] in sql:
            raise RuntimeError(self.conn.fail_on[1])
        if "SYSCOLUMNS" in sql:
            self.rows = self.conn.catalogue
        else:
            # Return only the selected columns, in the order they were selected.
            names = [row[2] for row in self.conn.catalogue]
            selected = re.match(r"SELECT (.*?) FROM", sql).group(1)
            indexes = [names.index(name) for name in re.findall(r'"((?:[^"]|"")*)"', selected)]
            self.rows = [tuple(row[i] for i in indexes) for row in self.conn.data]
        self.pos = 0

    def fetchall(self):
        rows = self.rows[self.pos :]
        self.pos = len(self.rows)
        return rows

    def fetchmany(self, size):
        self.conn.fetch_sizes.append(size)
        rows = self.rows[self.pos : self.pos + size]
        self.pos += len(rows)
        return rows

    def close(self):
        self.closed = True


class _FakeConnection:
    """Stand-in for a Mapepire connection."""

    def __init__(self, catalogue=None, data=None, fail_on=None):
        self.catalogue = CATALOGUE if catalogue is None else catalogue
        self.data = _rows(10) if data is None else data
        self.fail_on = fail_on
        self.executed = []
        self.fetch_sizes = []
        self.closed = False

    def cursor(self):
        return _FakeCursor(self)

    def close(self):
        self.closed = True


def _rows(count):
    """Return rows for the ORDERS catalogue, with a NULL REGION on every 7th row."""
    return [(i, 1.5 * i, None if i % 7 == 0 else f"R{i % 3}", i % 2) for i in range(count)]


def _client(conn=None, **kwargs):
    """Return a Db2Client that uses a fake connection."""
    return Db2Client("host", "db", "user", "secret", custom_connection=conn or _FakeConnection(), **kwargs)


@pytest.fixture
def encryption_key():
    """Set a valid DB_ENCRYPTION_KEY for the duration of a test."""
    with patch.object(settings.forecast, "db_encryption_key", SecretStr(ENCRYPTION_KEY)):
        yield ENCRYPTION_KEY


# --- Table references and SQL building


def test_split_table_ref_accepts_table_and_schema_table():
    """A reference is TABLE or SCHEMA.TABLE, and whitespace is ignored."""
    assert Db2Client.split_table_ref("a.b") == ("a", "b")
    assert Db2Client.split_table_ref(" A . B ") == ("A", "B")
    assert Db2Client.split_table_ref("T") == (None, "T")


@pytest.mark.parametrize("ref", ["a.b.c", "", "a.", ".b", "  ", None])
def test_split_table_ref_rejects_malformed_references(ref):
    """References with three parts or empty parts are rejected as permanent failures."""
    with pytest.raises(InvalidTableReferenceError) as excinfo:
        Db2Client.split_table_ref(ref)
    assert excinfo.value.is_permanent_failure is True


def test_quote_identifier_doubles_embedded_quotes():
    """An embedded quote cannot end the quoted identifier early."""
    assert Db2Client.quote_identifier("ID") == '"ID"'
    assert Db2Client.quote_identifier('a"b; DROP') == '"a""b; DROP"'


def test_build_select_query_is_read_only_and_quoted():
    """The query quotes every identifier and reads without locks."""
    query = _client().build_select_query("TEST.ORDERS", ["ID", 'we"ird'], batch_size=500)
    assert query == 'SELECT "ID", "we""ird" FROM "TEST"."ORDERS" FOR READ ONLY OPTIMIZE FOR 500 ROWS WITH UR'


def test_build_select_query_unqualified_table_has_no_schema():
    """An unqualified table is left to Db2 to resolve in the current schema."""
    query = _client().build_select_query("ORDERS", ["ID"], batch_size=10)
    assert query.startswith('SELECT "ID" FROM "ORDERS" FOR READ ONLY')


def test_raw_sql_filters_are_rejected(tmp_path):
    """Neither the query builder nor the loader has a filter parameter for raw SQL."""
    with pytest.raises(TypeError):
        _client().build_select_query("T", ["a"], where_clause="X = 1")
    with pytest.raises(TypeError):
        Db2DataLoader(_client()).extract_to_parquet("T", ["a"], tmp_path / "o.parquet", where_clause="X = 1")


# --- Catalogue lookups


def test_list_columns_uses_catalogue_spelling_and_nullability():
    """The table is matched case-insensitively and columns keep the catalogue spelling."""
    conn = _FakeConnection()
    columns = _client(conn).list_columns("test.orders")
    assert [c.name for c in columns] == ["ID", "Amount", "REGION", "FLAG"]
    assert [c.type for c in columns] == ["INTEGER", "DECIMAL", "VARCHAR", "SMALLINT"]
    assert [c.nullable for c in columns] == [False, True, True, True]
    assert columns[0].table_ref == "TEST.ORDERS"
    sql, params = conn.executed[0]
    assert "UPPER(TABLE_SCHEMA) = ?" in sql and params == ["TEST", "ORDERS"]


def test_list_columns_unqualified_uses_current_schema():
    """An unqualified table is looked up in the current schema and keeps no schema part."""
    conn = _FakeConnection()
    columns = _client(conn).list_columns("orders")
    assert columns[0].table_ref == "ORDERS"
    sql, params = conn.executed[0]
    assert "CURRENT SCHEMA" in sql and params == ["ORDERS"]


def test_list_columns_unknown_table_is_a_permanent_failure():
    """A table that returns no catalogue rows is reported as not found."""
    with pytest.raises(DBTableNotFoundError) as excinfo:
        _client(_FakeConnection(catalogue=[])).list_columns("TEST.NOPE")
    assert excinfo.value.is_permanent_failure is True


def test_list_columns_tables_differing_by_case_need_exact_spelling():
    """Two tables that differ only by case are ambiguous unless one matches exactly."""
    catalogue = [
        ("TEST", "Orders", "ID", "INTEGER", "N"),
        ("TEST", "ORDERS", "ID", "BIGINT", "N"),
    ]
    client = _client(_FakeConnection(catalogue=catalogue))
    with pytest.raises(InvalidTableReferenceError):
        client.list_columns("test.orders")
    assert client.list_columns("TEST.Orders")[0].type == "INTEGER"
    assert client.list_columns("TEST.ORDERS")[0].type == "BIGINT"


def test_list_columns_catalogue_failure_is_a_connection_error():
    """A failing catalogue query is reported as a connection problem."""
    client = _client(_FakeConnection(fail_on=("SYSCOLUMNS", "socket closed")))
    with pytest.raises(DBConnectionError):
        client.list_columns("TEST.ORDERS")


# --- Column resolution


def _columns():
    return [ColumnInfo(name=row[2], type=row[3], nullable=True, table_schema="TEST", table_name="ORDERS") for row in CATALOGUE]


def test_resolve_columns_prefers_exact_then_case_insensitive():
    """Requested names map to the catalogue spelling and keep the requested order."""
    resolved = loader_module._resolve_columns(["flag", "AMOUNT", "ID"], _columns(), "TEST.ORDERS")
    assert [c.name for c in resolved] == ["FLAG", "Amount", "ID"]


def test_resolve_columns_lists_every_missing_column():
    """All missing columns are named along with the available ones."""
    with pytest.raises(DBColumnNotFoundError) as excinfo:
        loader_module._resolve_columns(["ID", "NOPE", "ALSO_NOPE"], _columns(), "TEST.ORDERS")
    message = str(excinfo.value)
    assert "NOPE" in message and "ALSO_NOPE" in message and "REGION" in message


def test_resolve_columns_rejects_a_column_requested_twice():
    """A column asked for twice, in any spelling, is rejected so the file has unique column names."""
    for requested in (["ID", "ID"], ["ID", "id"]):
        with pytest.raises(DBColumnNotFoundError, match="more than once"):
            loader_module._resolve_columns(requested, _columns(), "TEST.ORDERS")


def test_resolve_columns_rejects_ambiguous_case_insensitive_match():
    """A name that matches two columns by case needs the exact spelling."""
    available = [
        ColumnInfo(name="Value", type="INTEGER", nullable=True),
        ColumnInfo(name="VALUE", type="INTEGER", nullable=True),
    ]
    with pytest.raises(DBColumnNotFoundError, match="ambiguous"):
        loader_module._resolve_columns(["value"], available, "T")
    assert loader_module._resolve_columns(["Value"], available, "T")[0].name == "Value"


# --- Type mapping


@pytest.mark.parametrize(
    "db_type, expected",
    [
        ("INTEGER", pa.int32()),
        ("smallint", pa.int16()),
        ("BIGINT", pa.int64()),
        ("DECIMAL(10,2)", pa.float64()),
        ("VARCHAR(50)", pa.string()),
        ("DATE", pa.date32()),
        ("TIMESTAMP", pa.timestamp("us")),
        ("TIMESTMP", pa.timestamp("us")),  # spelling in the Db2 for i catalogue
        ("SOMETHING_NEW", pa.string()),
        ("", pa.string()),
    ],
)
def test_map_db_type_to_arrow(db_type, expected):
    """Parameters are stripped and unknown types fall back to strings."""
    assert map_db_type_to_arrow(db_type) == expected


def test_build_arrow_schema_matches_names_case_insensitively():
    """An exact name wins, then a case-insensitive match, and the default is a string."""
    schema = build_arrow_schema(["ID", "amount", "extra"], {"ID": "INTEGER", "AMOUNT": "DECIMAL"})
    assert schema.names == ["ID", "amount", "extra"]
    assert [f.type for f in schema] == [pa.int32(), pa.float64(), pa.string()]
    assert all(f.nullable for f in schema)


# --- Parquet writing


def test_parquet_writer_writes_empty_dataset_as_valid_file(tmp_path):
    """No rows still produces a readable Parquet file with the schema."""
    target = tmp_path / "empty.parquet"
    with ParquetBatchWriter(target, build_arrow_schema(["ID"], {"ID": "INTEGER"})) as writer:
        assert writer.write_batch([]) == 0
    table = pq.read_table(target)
    assert table.num_rows == 0 and table.schema.names == ["ID"]


def test_parquet_writer_aborts_on_error_and_keeps_previous_file(tmp_path):
    """A failed write removes the temporary file and leaves the earlier target untouched."""
    target = tmp_path / "out.parquet"
    schema = build_arrow_schema(["ID"], {"ID": "INTEGER"})
    with ParquetBatchWriter(target, schema) as writer:
        writer.write_batch([(1,), (2,)])

    with pytest.raises(ParquetWriteError):
        with ParquetBatchWriter(target, schema) as writer:
            writer.write_batch([("not a number",)])

    assert sorted(p.name for p in tmp_path.iterdir()) == ["out.parquet"]
    assert pq.read_table(target).column("ID").to_pylist() == [1, 2]


def test_parquet_writer_rejects_rows_with_wrong_width(tmp_path):
    """A row with the wrong number of values is a Parquet write failure."""
    writer = ParquetBatchWriter(tmp_path / "bad.parquet", build_arrow_schema(["A", "B"]))
    with pytest.raises(ParquetWriteError, match="Row length"):
        writer.write_batch([(1,)])
    assert list(tmp_path.iterdir()) == []


def test_parquet_writer_rejects_writes_after_close(tmp_path):
    """A closed writer cannot be written to."""
    writer = ParquetBatchWriter(tmp_path / "a.parquet", build_arrow_schema(["A"]))
    writer.close()
    with pytest.raises(ParquetWriteError, match="closed"):
        writer.write_batch([("x",)])


def test_parquet_writer_parses_iso_dates_and_timestamps(tmp_path):
    """DATE and TIMESTAMP strings from Db2 become Arrow date and timestamp values."""
    schema = build_arrow_schema(["D", "TS"], {"D": "DATE", "TS": "TIMESTAMP"})
    with ParquetBatchWriter(tmp_path / "t.parquet", schema) as writer:
        writer.write_batch(
            [
                ("2026-11-23", "2026-11-23 10:42:24.623709"),
                (" 1999-01-02 ", "1999-01-02T03:04:05"),
                ("2026-02-28", "2026-11-23 10:42:24.123456789012"),  # TIMESTAMP(12)
                (None, None),
            ]
        )
    table = pq.read_table(tmp_path / "t.parquet")
    assert [str(f.type) for f in table.schema] == ["date32[day]", "timestamp[us]"]
    assert table.column("D").to_pylist()[:2] == [date(2026, 11, 23), date(1999, 1, 2)]
    assert table.column("D").null_count == 1
    assert table.column("TS").to_pylist()[0] == datetime(2026, 11, 23, 10, 42, 24, 623709)
    assert table.column("TS").to_pylist()[2] == datetime(2026, 11, 23, 10, 42, 24, 123456)


def test_parquet_writer_accepts_date_objects_unchanged(tmp_path):
    """Values that are already dates or datetimes are written as they are."""
    schema = build_arrow_schema(["D", "TS"], {"D": "DATE", "TS": "TIMESTAMP"})
    with ParquetBatchWriter(tmp_path / "t.parquet", schema) as writer:
        writer.write_batch([(date(2020, 5, 6), datetime(2020, 5, 6, 7, 8, 9))])
    assert pq.read_table(tmp_path / "t.parquet").num_rows == 1


@pytest.mark.parametrize("db_type, bad", [("DATE", "11/23/26"), ("DATE", "2026-13-40"), ("TIMESTAMP", "yesterday")])
def test_parquet_writer_rejects_dates_that_are_not_iso(tmp_path, db_type, bad):
    """A date that is not ISO fails the write and leaves no file, instead of being guessed."""
    schema = build_arrow_schema(["V"], {"V": db_type})
    with pytest.raises(ParquetWriteError):
        with ParquetBatchWriter(tmp_path / "t.parquet", schema) as writer:
            writer.write_batch([(bad,)])
    assert list(tmp_path.iterdir()) == []


# --- Extraction


def test_extract_to_parquet_writes_typed_columns_and_nulls(tmp_path):
    """Rows are written with the mapped Arrow types and NULLs are kept."""
    conn = _FakeConnection(data=_rows(10))
    result = Db2DataLoader(_client(conn)).extract_to_parquet(
        "TEST.ORDERS", ["ID", "Amount", "REGION", "FLAG"], tmp_path / "orders.parquet", batch_size=4
    )

    table = pq.read_table(result.output_path)
    assert [str(f.type) for f in table.schema] == ["int32", "double", "string", "int16"]
    assert table.num_rows == result.rows_written == 10
    assert table.column("ID").to_pylist() == list(range(10))
    assert table.column("REGION").null_count == 2  # rows 0 and 7
    assert result.columns == ["ID", "Amount", "REGION", "FLAG"]
    assert result.table == "TEST.ORDERS"
    assert result.bytes_written == (tmp_path / "orders.parquet").stat().st_size


def test_extract_to_parquet_queries_with_catalogue_spelling(tmp_path):
    """Lowercase table and column names are rewritten to the catalogue spelling in the SQL."""
    conn = _FakeConnection()
    Db2DataLoader(_client(conn)).extract_to_parquet("test.orders", ["amount", "id"], tmp_path / "o.parquet")
    select_sql = conn.executed[-1][0]
    assert select_sql == 'SELECT "Amount", "ID" FROM "TEST"."ORDERS" FOR READ ONLY OPTIMIZE FOR 100000 ROWS WITH UR'


def test_extract_to_parquet_unqualified_table_is_left_to_the_current_schema(tmp_path):
    """Without a schema the query has no schema part and uses the catalogue spelling."""
    conn = _FakeConnection()
    result = Db2DataLoader(_client(conn)).extract_to_parquet("orders", ["id"], tmp_path / "o.parquet")
    assert 'SELECT "ID" FROM "ORDERS" FOR READ ONLY' in conn.executed[-1][0]
    assert result.table == "ORDERS"


def test_extract_to_parquet_batches_rows(tmp_path):
    """Rows are written in batches and the transport fetches in smaller chunks."""
    conn = _FakeConnection(data=_rows(10))
    result = Db2DataLoader(_client(conn)).extract_to_parquet("TEST.ORDERS", ["ID"], tmp_path / "o.parquet", batch_size=4)
    assert result.batches_count == 3  # 4 + 4 + 2
    assert conn.fetch_sizes[0] == 4


def test_extract_to_parquet_limits_transport_fetch_size(tmp_path):
    """A large batch is fetched from the websocket in chunks of at most 2500 rows."""
    conn = _FakeConnection(data=_rows(6000))
    result = Db2DataLoader(_client(conn)).extract_to_parquet(
        "TEST.ORDERS", ["ID"], tmp_path / "o.parquet", batch_size=100_000
    )
    assert set(conn.fetch_sizes) == {2500}
    assert result.batches_count == 1 and result.rows_written == 6000


def test_extract_to_parquet_empty_table_writes_empty_file(tmp_path):
    """A table without rows gives an empty file with the right schema."""
    result = Db2DataLoader(_client(_FakeConnection(data=[]))).extract_to_parquet(
        "TEST.ORDERS", ["ID", "REGION"], tmp_path / "o.parquet"
    )
    table = pq.read_table(result.output_path)
    assert result.rows_written == 0 and result.batches_count == 0
    assert table.num_rows == 0 and table.schema.names == ["ID", "REGION"]


def test_extract_to_parquet_subset_keeps_requested_order(tmp_path):
    """Only the requested columns are written, in the requested order."""
    result = Db2DataLoader(_client()).extract_to_parquet("TEST.ORDERS", ["FLAG", "ID"], tmp_path / "o.parquet")
    assert pq.read_table(result.output_path).schema.names == ["FLAG", "ID"]


def test_extract_to_parquet_missing_column_writes_nothing(tmp_path):
    """A missing column fails before any file is written."""
    with pytest.raises(DBColumnNotFoundError):
        Db2DataLoader(_client()).extract_to_parquet("TEST.ORDERS", ["ID", "NOPE"], tmp_path / "o.parquet")
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize("db_type", ["BINARY", "VARBINARY", "VARBIN", "BLOB", "binary(16)"])
def test_extract_to_parquet_names_binary_columns_before_querying(tmp_path, db_type):
    """A binary column fails with a clear permanent error that names it, before any row is read."""
    catalogue = CATALOGUE + [("TEST", "ORDERS", "RAW", db_type, "Y")]
    conn = _FakeConnection(catalogue=catalogue)
    with pytest.raises(UnsupportedColumnTypeError) as excinfo:
        Db2DataLoader(_client(conn)).extract_to_parquet("TEST.ORDERS", ["ID", "RAW"], tmp_path / "o.parquet")
    assert f"RAW ({db_type})" in str(excinfo.value) and "TEST.ORDERS" in str(excinfo.value)
    assert "Leave them out" in str(excinfo.value)
    assert excinfo.value.is_permanent_failure is True
    assert len(conn.executed) == 1  # only the catalogue lookup, no SELECT
    assert list(tmp_path.iterdir()) == []


def test_extract_to_parquet_ignores_binary_columns_that_are_not_requested(tmp_path):
    """A table with a binary column can still be extracted when that column is left out."""
    catalogue = CATALOGUE + [("TEST", "ORDERS", "RAW", "BLOB", "Y")]
    result = Db2DataLoader(_client(_FakeConnection(catalogue=catalogue))).extract_to_parquet(
        "TEST.ORDERS", ["ID", "FLAG"], tmp_path / "o.parquet"
    )
    assert result.rows_written == 10


def test_extract_to_parquet_requires_columns(tmp_path):
    """An empty column list is rejected."""
    with pytest.raises(DBColumnNotFoundError):
        Db2DataLoader(_client()).extract_to_parquet("TEST.ORDERS", [], tmp_path / "o.parquet")


def test_extract_to_parquet_stream_failure_leaves_no_file(tmp_path):
    """A failure while streaming rows removes the temporary file."""
    conn = _FakeConnection(fail_on=("FOR READ ONLY", "connection reset"))
    with pytest.raises(DBConnectionError):
        Db2DataLoader(_client(conn)).extract_to_parquet("TEST.ORDERS", ["ID"], tmp_path / "o.parquet")
    assert list(tmp_path.iterdir()) == []


def test_extract_to_parquet_requires_a_client_or_connector_details(tmp_path):
    """Without a client or connector details there is nothing to connect with."""
    with pytest.raises(ValueError, match="Db2Client"):
        Db2DataLoader().extract_to_parquet("TEST.ORDERS", ["ID"], tmp_path / "o.parquet")


# --- Streaming error mapping


@pytest.mark.parametrize(
    "message, expected, permanent",
    [
        ("SQL0204 TEST.ORDERS in *LIBL type *FILE not found", DBTableNotFoundError, True),
        ("SQL0206 Column FOO not in table", DBColumnNotFoundError, True),
        ("SQL0551 not authorized to object", DBAuthError, True),
        ("websocket closed unexpectedly", DBConnectionError, False),
    ],
)
def test_stream_query_batches_maps_errors(message, expected, permanent):
    """A failure while streaming becomes a typed error with the right retry behaviour."""
    client = _client(_FakeConnection(fail_on=("SELECT", message)))
    with pytest.raises(expected) as excinfo:
        list(client.stream_query_batches('SELECT "ID" FROM "T"'))
    assert excinfo.value.is_permanent_failure is permanent


def test_stream_query_batches_closes_cursor():
    """The cursor is closed once the stream ends."""
    conn = _FakeConnection(data=_rows(3))
    cursors = []
    original = conn.cursor
    conn.cursor = lambda: cursors.append(original()) or cursors[-1]
    batches = list(_client(conn).stream_query_batches('SELECT "ID" FROM "T"', batch_size=2))
    assert [len(b) for b in batches] == [2, 1]
    assert cursors[0].closed is True


# --- Connection handling


def test_test_connection_succeeds_and_reports_auth_errors():
    """A ping succeeds, and credential problems are reported as authentication errors."""
    assert _client().test_connection() is True
    with pytest.raises(DBAuthError):
        _client(_FakeConnection(fail_on=("SYSDUMMY1", "Password is incorrect"))).test_connection()
    with pytest.raises(DBConnectionError):
        _client(_FakeConnection(fail_on=("SYSDUMMY1", "socket closed"))).test_connection()


def test_client_context_manager_closes_connection():
    """Leaving the context closes the connection."""
    conn = _FakeConnection()
    with _client(conn) as client:
        client.test_connection()
    assert conn.closed is True


@pytest.mark.parametrize(
    "message, expected, permanent",
    [
        ("java.sql.SQLNonTransientConnectionException: Password is incorrect.:USER", DBAuthError, True),
        ("[Errno 111] Connection refused", DBConnectionError, False),
        # A certificate that does not verify cannot be fixed by trying again.
        ("[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed", DBTLSConfigError, True),
    ],
)
@pytest.mark.usefixtures("no_certificate_probe")
def test_get_connection_maps_connect_errors(message, expected, permanent):
    """Connect failures are split into authentication, certificate and connection errors."""
    client = Db2Client("host", "db", "user", "secret")
    with patch("mapepire_python.connect", side_effect=RuntimeError(message)):
        with pytest.raises(expected) as excinfo:
            client.list_columns("TEST.ORDERS")
    assert excinfo.value.is_permanent_failure is permanent
    assert "secret" not in str(excinfo.value)


@pytest.mark.parametrize(
    "error",
    [
        ssl.SSLCertVerificationError(1, "[SSL: CERTIFICATE_VERIFY_FAILED] self-signed certificate"),
        ssl.SSLCertVerificationError(1, "Hostname mismatch, certificate is not valid for 'host'."),
    ],
    ids=["untrusted", "hostname"],
)
@pytest.mark.usefixtures("no_certificate_probe")
def test_get_connection_treats_a_certificate_failure_as_permanent_whatever_wraps_it(error):
    """The SSL error is found by type, also when Mapepire re-raises it as another exception."""
    client = Db2Client("host", "db", "user", "secret")
    wrapped = RuntimeError("Failed to connect")
    wrapped.__cause__ = error
    for raised in (error, wrapped):
        with patch("mapepire_python.connect", side_effect=raised):
            with pytest.raises(DBTLSConfigError) as excinfo:
                client.list_columns("TEST.ORDERS")
        assert excinfo.value.is_permanent_failure is True
        assert "secret" not in str(excinfo.value)


@pytest.mark.usefixtures("no_certificate_probe")
def test_get_connection_passes_connection_settings_to_mapepire():
    """Host, port and credentials reach the Mapepire daemon description."""
    client = Db2Client("host", "db", "user", "secret", port=9999, allow_insecure_connection=True)
    with patch("mapepire_python.DaemonServer") as daemon, patch("mapepire_python.connect") as connect:
        assert client._get_connection() is connect.return_value
    daemon.assert_called_once_with(
        host="host", user="user", password="secret", port=9999, ignoreUnauthorized=True
    )
    connect.assert_called_once_with(connection_details=daemon.return_value, opts={"date format": "iso"})


# --- TLS


def _make_cert(tmp_path, name="test-ca", san_ip=None):
    """Write a throw-away self-signed certificate; return (path, key, cert)."""
    from datetime import timedelta, timezone
    import ipaddress

    from cryptography import x509
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    from cryptography.x509.oid import NameOID

    key = ec.generate_private_key(ec.SECP256R1())
    subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, name)])
    now = datetime.now(timezone.utc)
    builder = (
        x509.CertificateBuilder()
        .subject_name(subject).issuer_name(subject).public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - timedelta(days=1)).not_valid_after(now + timedelta(days=1))
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), critical=True)
    )
    # The key identifiers keep the certificate valid under the strict verification of newer Pythons.
    builder = builder.add_extension(x509.SubjectKeyIdentifier.from_public_key(key.public_key()), False)
    builder = builder.add_extension(x509.AuthorityKeyIdentifier.from_issuer_public_key(key.public_key()), False)
    if san_ip:
        builder = builder.add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address(san_ip))]), False)
    cert = builder.sign(key, hashes.SHA256())
    path = tmp_path / f"{name}.pem"
    path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
    return path, key, cert


@pytest.fixture
def ca_pem(tmp_path):
    """The PEM content of a throw-away self-signed CA, as sent in the connector's certificate."""
    return _make_cert(tmp_path)[0].read_text()


@pytest.fixture
def no_certificate_probe():
    """Skip fetching the server certificate: these tests mock Mapepire and have no server to ask."""
    with patch.object(Db2Client, "_build_ssl_context", return_value=ssl.create_default_context()):
        yield


def _connect_with(client):
    """Connect through a mocked Mapepire and return the DaemonServer that was used."""
    with patch("mapepire_python.DaemonServer") as daemon, patch("mapepire_python.connect"):
        client._get_connection()
    return daemon.return_value


def test_a_certificate_is_added_to_the_system_trust_store(ca_pem):
    """The certificate is trusted in addition to the system CAs; verification and host name checks stay on."""
    system_cas = ssl.create_default_context(ssl.Purpose.SERVER_AUTH).cert_store_stats()["x509_ca"]
    server = _connect_with(Db2Client("host", "db", "user", "secret", certificate=ca_pem))
    context = server.ssl_context
    assert context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname is True
    assert context.cert_store_stats()["x509_ca"] == system_cas + 1


@pytest.mark.parametrize("content", ["  \n", "not a certificate", "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"])
def test_invalid_certificate_does_not_expose_its_content(content):
    """A certificate that is not a PEM certificate gives a permanent error that does not echo it.

    A blank (whitespace) value is invalid too. Only the empty string means "no certificate".
    """
    client = Db2Client("host", "db", "user", "secret", certificate=content)
    with patch("mapepire_python.DaemonServer"), patch("mapepire_python.connect") as connect:
        with pytest.raises(DBTLSConfigError, match="valid PEM certificate") as excinfo:
            client._get_connection()
    connect.assert_not_called()
    assert excinfo.value.is_permanent_failure is True
    message = str(excinfo.value)
    assert "AAAA" not in message and "not a certificate" not in message and "secret" not in message


def test_ca_content_is_never_logged(ca_pem, caplog):
    """Neither the CA content nor the password appears in the logs, at any level."""
    with caplog.at_level("DEBUG"):
        _connect_with(Db2Client("host", "db", "user", "secret", certificate=ca_pem))
    assert "BEGIN CERTIFICATE" not in caplog.text and "secret" not in caplog.text


def test_a_certificate_verifies_the_server_and_does_not_set_ignore_unauthorized(ca_pem):
    """With a certificate Mapepire's skip switch is off and the client context verifies chain and host name."""
    with patch("mapepire_python.DaemonServer") as daemon, patch("mapepire_python.connect"):
        Db2Client("host", "db", "user", "secret", certificate=ca_pem)._get_connection()
    assert daemon.call_args.kwargs["ignoreUnauthorized"] is False
    context = daemon.return_value.ssl_context
    assert context.verify_mode == ssl.CERT_REQUIRED and context.check_hostname is True


@pytest.mark.parametrize("certificate", [None, ""])
def test_allow_insecure_connection_sets_ignore_unauthorized_and_uses_no_certificate(certificate, caplog):
    """allow_insecure_connection without a certificate: Mapepire's skip switch is on, nothing is loaded
    (not even the system store), and a warning is logged."""
    client = Db2Client("host", "db", "user", "secret", certificate=certificate, allow_insecure_connection=True)
    assert client.certificate is None
    with patch("mapepire_python.DaemonServer") as daemon, patch("mapepire_python.connect"):
        with caplog.at_level("WARNING"):
            client._get_connection()
    assert daemon.call_args.kwargs["ignoreUnauthorized"] is True
    context = daemon.return_value.ssl_context
    assert context.verify_mode == ssl.CERT_NONE and context.check_hostname is False
    assert context.cert_store_stats() == {"x509": 0, "crl": 0, "x509_ca": 0}
    assert "INSECURE" in caplog.text and "allow_insecure_connection" in caplog.text
    assert "secret" not in caplog.text


@pytest.mark.parametrize(
    "certificate, ignore, message",
    [
        (None, False, "not both and not neither"),
        ("", False, "not both and not neither"),
        ("PEM", True, "not both and not neither"),
    ],
)
def test_an_invalid_combination_is_refused_and_never_connects(certificate, ignore, message, ca_pem):
    """Neither mode (or both) is a permanent configuration error: nothing silently falls back."""
    certificate = ca_pem if certificate == "PEM" else certificate
    client = Db2Client("host", "db", "user", "secret", certificate=certificate, allow_insecure_connection=ignore)
    with patch("mapepire_python.DaemonServer"), patch("mapepire_python.connect") as connect:
        with pytest.raises(DBTLSConfigError, match=message) as excinfo:
            client._get_connection()
    connect.assert_not_called()
    assert excinfo.value.is_permanent_failure is True
    assert "BEGIN CERTIFICATE" not in str(excinfo.value) and "secret" not in str(excinfo.value)


def test_the_server_certificate_is_never_fetched_or_trusted(ca_pem):
    """Whatever the mode, the client never connects just to read the server's own certificate."""
    assert not hasattr(Db2Client, "_pin_server_certificate")
    with patch("socket.create_connection") as sock:
        Db2Client("host", "db", "user", "secret", allow_insecure_connection=True)._build_ssl_context()
        Db2Client("host", "db", "user", "secret", certificate=ca_pem)._build_ssl_context()
    sock.assert_not_called()


def test_the_old_flags_are_not_client_options():
    """ignore_secure_connection and ignore_unauthorized do not exist: allow_insecure_connection replaces them."""
    import inspect

    parameters = inspect.signature(Db2Client.__init__).parameters
    assert "ignore_secure_connection" not in parameters and "ignore_unauthorized" not in parameters
    assert "allow_insecure_connection" in parameters


def test_the_loader_passes_the_certificate_and_the_flag_to_the_client(ca_pem):
    """The decrypted connection details decide the TLS mode; a stored string flag is parsed."""
    base = {"host": "h", "database": "d", "username": "u", "password": "p"}
    insecure = loader_module._client_from_config({**base, "allow_insecure_connection": True})
    insecure_str = loader_module._client_from_config({**base, "allow_insecure_connection": "true"})
    verified = loader_module._client_from_config(
        {**base, "certificate": ca_pem, "allow_insecure_connection": False}
    )
    assert insecure.certificate is None and insecure.allow_insecure_connection is True
    assert insecure_str.allow_insecure_connection is True
    assert verified.certificate == ca_pem and verified.allow_insecure_connection is False


@pytest.mark.parametrize(
    "value, expected",
    [
        (True, True), ("true", True), ("True ", True), ("1", True), ("yes", True),
        (False, False), ("false", False), ("0", False), ("off", False), ("", False), ("  ", False),
        (None, False),
    ],
)
def test_the_stored_flag_is_parsed_so_that_only_a_clear_yes_skips_verification(value, expected):
    """A flag stored as text is parsed; anything that is not a clear yes keeps the server verified."""
    assert loader_module._as_bool(value) is expected
    client = loader_module._client_from_config(
        {"host": "h", "database": "d", "username": "u", "password": "p", "allow_insecure_connection": value}
    )
    assert client.allow_insecure_connection is expected


def test_an_old_connector_without_the_flag_does_not_connect_unverified():
    """A stored connector without the flag defaults to false, so without a certificate it is refused."""
    client = loader_module._client_from_config({"host": "h", "database": "d", "username": "u", "password": "p"})
    assert client.allow_insecure_connection is False
    with patch("mapepire_python.DaemonServer"), patch("mapepire_python.connect") as connect:
        with pytest.raises(DBTLSConfigError):
            client._get_connection()
    connect.assert_not_called()


# --- A real TLS handshake: the client's own context against a local websocket server


@pytest.fixture
def tls_server(tmp_path):
    """Start a websocket server over TLS on 127.0.0.1; return a function that gives (port, certificate PEM)."""
    import threading

    from cryptography.hazmat.primitives import serialization
    from websockets.sync.server import serve

    servers = []

    def start(san_ip="127.0.0.1"):
        cert_path, key, _ = _make_cert(tmp_path, name=f"server-{san_ip}", san_ip=san_ip)
        key_path = tmp_path / f"server-{san_ip}.key"
        key_path.write_bytes(
            key.private_bytes(
                serialization.Encoding.PEM,
                serialization.PrivateFormat.PKCS8,
                serialization.NoEncryption(),
            )
        )
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cert_path, key_path)
        server = serve(lambda connection: None, "127.0.0.1", 0, ssl=context)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        servers.append(server)
        return server.socket.getsockname()[1], cert_path.read_text()

    yield start
    for server in servers:
        server.shutdown()


def _open_websocket(client, port, monkeypatch):
    """Open the socket through the Mapepire connect that Db2Client patches, with the client's own TLS context."""
    import mapepire_python.client.websocket_client as mapepire_ws

    connection_class = mapepire_ws.WebsocketConnection
    monkeypatch.setattr(connection_class, "connect", connection_class.connect)
    monkeypatch.setattr(connection_class, "_max_size_configured", False, raising=False)
    with patch("mapepire_python.DaemonServer"), patch("mapepire_python.connect"):
        client._get_connection()  # installs the patched connect
    server = SimpleNamespace(ssl_context=client._build_ssl_context(), connect_timeout_s=5)
    ws = connection_class.connect(SimpleNamespace(db2_server=server, uri=f"wss://127.0.0.1:{port}/db/", headers={}))
    with warnings.catch_warnings():
        # websockets 15 and later warn when the result of connect() is used without ``with``,
        # which is how the Mapepire connect that Db2Client patches uses it.
        warnings.simplefilter("ignore", DeprecationWarning)
        ws.close()


def test_the_certificate_is_accepted_by_a_real_handshake(tls_server, monkeypatch):
    """A server whose certificate was sent as the connector's certificate is trusted."""
    port, pem = tls_server()
    _open_websocket(Db2Client("127.0.0.1", "db", "user", "secret", certificate=pem), port, monkeypatch)


def test_a_different_certificate_is_refused_by_a_real_handshake(tls_server, ca_pem, monkeypatch):
    """A certificate other than the server's does not make the server trusted."""
    port, _ = tls_server()
    client = Db2Client("127.0.0.1", "db", "user", "secret", certificate=ca_pem)
    with pytest.raises(ssl.SSLCertVerificationError) as excinfo:
        _open_websocket(client, port, monkeypatch)
    assert "self-signed" in excinfo.value.verify_message or "unable to get" in excinfo.value.verify_message


def test_a_certificate_for_another_host_is_refused_by_a_real_handshake(tls_server, monkeypatch):
    """The certificate is the right one but does not name the host that was connected to."""
    port, pem = tls_server(san_ip="127.0.0.2")
    client = Db2Client("127.0.0.1", "db", "user", "secret", certificate=pem)
    with pytest.raises(ssl.SSLCertVerificationError) as excinfo:
        _open_websocket(client, port, monkeypatch)
    assert "mismatch" in excinfo.value.verify_message.lower()


def test_allow_insecure_connection_accepts_any_certificate_in_a_real_handshake(tls_server, monkeypatch):
    """With the flag the handshake succeeds although the certificate is unknown and names another host."""
    port, _ = tls_server(san_ip="127.0.0.2")
    _open_websocket(Db2Client("127.0.0.1", "db", "user", "secret", allow_insecure_connection=True), port, monkeypatch)


def test_a_tls_configuration_error_makes_the_orchestrator_stop_retrying(encryption_key, tmp_path):
    """Through the real loader: a connector with neither field is a PermanentFailure and never connects."""
    from forecast.errors import PermanentFailure
    from forecast.orchestrator.source import load_training_data

    details = encrypt_secrets({"host": "h", "database": "d", "username": "u", "password": "secret"})
    with patch("mapepire_python.connect") as connect:
        with pytest.raises(PermanentFailure, match="allow_insecure_connection") as excinfo:
            load_training_data(
                table="TEST.ORDERS", columns=["ID"], output_path=tmp_path / "o.parquet",
                connector_id="conn-9", connection_details=details,
            )
    connect.assert_not_called()
    assert isinstance(excinfo.value.__cause__, DBTLSConfigError)
    assert "secret" not in str(excinfo.value)


def test_websocket_always_uses_wss_with_the_client_context(monkeypatch):
    """The socket is opened with the TLS context of the client, not Mapepire's own."""
    import mapepire_python.client.websocket_client as mapepire_ws

    connection_class = mapepire_ws.WebsocketConnection
    monkeypatch.setattr(connection_class, "connect", connection_class.connect)
    monkeypatch.setattr(connection_class, "_max_size_configured", False, raising=False)

    context = ssl.create_default_context()
    with (
        patch("websockets.sync.client.connect") as ws_connect,
        patch("mapepire_python.DaemonServer"),
        patch("mapepire_python.connect"),
        patch.object(Db2Client, "_build_ssl_context"),
    ):
        Db2Client("host", "db", "user", "secret")._get_connection()
        server = SimpleNamespace(ssl_context=context)
        connection_class.connect(MagicMock(db2_server=server, uri="wss://h:8076/db/"))

    assert ws_connect.call_args.args[0] == "wss://h:8076/db/"
    assert ws_connect.call_args.kwargs["ssl"] is context


@pytest.mark.usefixtures("no_certificate_probe")
def test_websocket_frames_are_allowed_up_to_64_mib(monkeypatch):
    """The Mapepire websocket is patched to accept frames larger than the 1 MiB default."""
    import mapepire_python.client.websocket_client as mapepire_ws

    connection_class = mapepire_ws.WebsocketConnection
    monkeypatch.setattr(connection_class, "connect", connection_class.connect)
    monkeypatch.setattr(connection_class, "_max_size_configured", False, raising=False)

    client = Db2Client("host", "db", "user", "secret")
    with (
        patch("websockets.sync.client.connect") as ws_connect,
        patch("mapepire_python.DaemonServer"),
        patch("mapepire_python.connect"),
    ):
        client._get_connection()
        connection_class.connect(MagicMock())
    assert ws_connect.call_args.kwargs["max_size"] == 64 * 1024 * 1024


# --- Timeouts


@pytest.mark.usefixtures("no_certificate_probe")
def test_get_connection_sets_connect_then_query_timeout_on_the_server():
    """Mapepire gets the connect timeout, and reads switch to the query timeout once connected."""
    seen = {}

    def connect(connection_details, opts):
        seen["open"] = connection_details.connect_timeout_s
        seen["reads_while_connecting"] = connection_details.recv_timeout_s
        return MagicMock()

    client = Db2Client("host", "db", "user", "secret", connect_timeout=7, query_timeout=99)
    with patch("mapepire_python.DaemonServer") as daemon, patch("mapepire_python.connect", side_effect=connect):
        client._get_connection()

    assert seen == {"open": 7, "reads_while_connecting": 7}
    assert daemon.return_value.recv_timeout_s == 99


@pytest.mark.usefixtures("no_certificate_probe")
def test_websocket_uses_the_timeouts_of_the_server(monkeypatch):
    """The socket is opened with the connect timeout and each read gets the current read timeout."""
    import mapepire_python.client.websocket_client as mapepire_ws

    connection_class = mapepire_ws.WebsocketConnection
    monkeypatch.setattr(connection_class, "connect", connection_class.connect)
    monkeypatch.setattr(connection_class, "_max_size_configured", False, raising=False)

    server = SimpleNamespace(connect_timeout_s=4, recv_timeout_s=12.5, ssl_context=None)
    ws_self = MagicMock(db2_server=server)
    with (
        patch("websockets.sync.client.connect") as ws_connect,
        patch("mapepire_python.DaemonServer"),
        patch("mapepire_python.connect"),
    ):
        raw_recv = ws_connect.return_value.recv
        Db2Client("host", "db", "user", "secret")._get_connection()
        ws = connection_class.connect(ws_self)
    assert ws_connect.call_args.kwargs["open_timeout"] == 4

    ws.recv()
    assert raw_recv.call_args.kwargs["timeout"] == 12.5
    server.recv_timeout_s = 3
    ws.recv()
    assert raw_recv.call_args.kwargs["timeout"] == 3
    ws.recv(timeout=1)
    assert raw_recv.call_args.kwargs["timeout"] == 1


def test_stream_query_batches_limits_reads_to_the_time_left():
    """Reads wait at most for what is left of the query timeout, and the limit is restored afterwards."""
    client = _client(_FakeConnection(data=_rows(3)), query_timeout=100, connect_timeout=5)
    client._server = SimpleNamespace(recv_timeout_s=None)
    limits = []
    fetchmany = _FakeCursor.fetchmany

    def recording_fetchmany(cursor, size):
        limits.append(client._server.recv_timeout_s)
        return fetchmany(cursor, size)

    with patch.object(_FakeCursor, "fetchmany", recording_fetchmany):
        list(client.stream_query_batches('SELECT "ID" FROM "T"', batch_size=2))

    assert limits and all(0 < limit <= 100 for limit in limits)
    assert client._server.recv_timeout_s == 100


def test_stream_query_batches_fails_when_the_deadline_has_passed():
    """A stream that runs past the query timeout fails with a transient connection error."""
    client = _client(_FakeConnection(data=_rows(10)), query_timeout=60)
    clock = iter([0, 10, 61, 62, 63])
    with patch("forecast.data_loader.db2_client.time", MagicMock(monotonic=lambda: next(clock))):
        with pytest.raises(DBConnectionError, match="60s query timeout") as excinfo:
            list(client.stream_query_batches('SELECT "ID" FROM "T"', batch_size=2))
    assert excinfo.value.is_permanent_failure is False


def test_stream_query_batches_maps_a_read_timeout():
    """A read that times out is a transient connection error, not a column or table error."""
    conn = _FakeConnection()
    cursor = _FakeCursor(conn)
    cursor.fetchmany = MagicMock(side_effect=TimeoutError("timed out waiting for column data"))
    conn.cursor = lambda: cursor
    with pytest.raises(DBConnectionError, match="query timeout") as excinfo:
        list(_client(conn, query_timeout=5).stream_query_batches('SELECT "ID" FROM "T"'))
    assert excinfo.value.is_permanent_failure is False
    assert cursor.closed is True


def test_stream_query_batches_keeps_the_error_when_the_cursor_cannot_be_closed():
    """A failing cursor close after an error does not hide the original error."""
    conn = _FakeConnection()
    cursor = _FakeCursor(conn)
    cursor.fetchmany = MagicMock(side_effect=TimeoutError)
    cursor.close = MagicMock(side_effect=TimeoutError("close timed out"))
    conn.cursor = lambda: cursor
    with pytest.raises(DBConnectionError, match="query timeout"):
        list(_client(conn).stream_query_batches('SELECT "ID" FROM "T"'))


# --- Connector configuration and decryption


def test_client_from_config_converts_string_options():
    """Decrypted numeric options become numbers."""
    client = loader_module._client_from_config(
        {
            "host": "h", "database": "*LOCAL", "username": "u", "password": "p",
            "port": "8076", "connect_timeout": "5", "query_timeout": "60",
        }
    )
    assert (client.port, client.connect_timeout, client.query_timeout) == (8076, 5, 60)


def test_client_from_config_uses_settings_defaults():
    """Missing options fall back to the service settings; there is no certificate and the server is verified."""
    client = loader_module._client_from_config({"host": "h", "database": "d", "username": "u", "password": "p"})
    assert client.port == settings.forecast.mapepire_port
    assert client.connect_timeout == settings.forecast.connector_connect_timeout_s
    assert client.query_timeout == settings.forecast.connector_query_timeout_s
    assert client.certificate is None


def test_client_from_config_takes_the_certificate_from_the_connection_details(ca_pem):
    """The CA content is a connector field, not a service setting."""
    client = loader_module._client_from_config(
        {"host": "h", "database": "d", "username": "u", "password": "p", "certificate": ca_pem}
    )
    assert client.certificate == ca_pem


def test_extract_with_encrypted_details_decrypts_and_closes(encryption_key, tmp_path):
    """Encrypted connector details are decrypted into a client that is closed afterwards."""
    encrypted = encrypt_secrets(
        {"host": "h", "port": 8076, "database": "*LOCAL", "username": "u", "password": "secret"},
    )
    assert encrypted["host"] == "h" and encrypted["username"] == "u"
    assert "secret" not in str(encrypted)

    conn = _FakeConnection()
    captured = {}

    def build_client(config):
        captured.update(config)
        return _client(conn)

    with patch.object(loader_module, "_client_from_config", side_effect=build_client):
        result = Db2DataLoader().extract_to_parquet(
            "TEST.ORDERS", ["ID"], tmp_path / "o.parquet", connector_id="c1",
            encrypted_connection_details=encrypted,
        )

    assert captured["password"] == "secret"
    assert result.rows_written == 10
    assert conn.closed is True


def test_extract_with_wrong_key_names_connector_without_secrets(tmp_path):
    """A key that cannot decrypt the details gives a permanent error that leaks nothing."""
    with patch.object(settings.forecast, "db_encryption_key", SecretStr(ENCRYPTION_KEY)):
        encrypted = encrypt_secrets({"host": "h", "username": "u", "password": "topsecret"})
    with patch.object(settings.forecast, "db_encryption_key", SecretStr("f" * 32)):
        with pytest.raises(ConnectorDecryptionError) as excinfo:
            Db2DataLoader().extract_to_parquet(
                "TEST.ORDERS", ["ID"], tmp_path / "o.parquet", connector_id="conn-42",
                encrypted_connection_details=encrypted,
            )
    message = str(excinfo.value)
    assert "conn-42" in message
    assert "topsecret" not in message and encrypted["password"] not in message
    assert excinfo.value.is_permanent_failure is True
    assert list(tmp_path.iterdir()) == []


def test_extract_with_corrupt_ciphertext_names_the_connector(encryption_key, tmp_path):
    """A secret that is not valid ciphertext is a permanent error that names the connector."""
    details = {"host": "h", "username": "u", "password": "not-valid-ciphertext!"}
    with pytest.raises(ConnectorDecryptionError, match="conn-7") as excinfo:
        Db2DataLoader().extract_to_parquet(
            "TEST.ORDERS", ["ID"], tmp_path / "o.parquet", connector_id="conn-7",
            encrypted_connection_details=details,
        )
    assert excinfo.value.is_permanent_failure is True


def test_extract_with_a_missing_key_is_not_reported_as_a_connector_error(tmp_path):
    """A missing encryption key is a service setup problem, so it is not blamed on the connector."""
    with patch.object(settings.forecast, "db_encryption_key", SecretStr("")):
        with pytest.raises(EncryptionKeyError):
            Db2DataLoader().extract_to_parquet(
                "TEST.ORDERS", ["ID"], tmp_path / "o.parquet", connector_id="conn-1",
                encrypted_connection_details={"host": "h", "password": "x"},
            )


def test_extract_with_encrypted_details_requires_connector_identity(tmp_path):
    """The connector id is required to decrypt connector details, so errors can name it."""
    with pytest.raises(ValueError, match="connector_id"):
        Db2DataLoader().extract_to_parquet(
            "TEST.ORDERS", ["ID"], tmp_path / "o.parquet", encrypted_connection_details={"host": "h"}
        )
