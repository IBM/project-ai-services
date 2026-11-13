"""
Unit tests for the Db2 for i connection and schema probe utility.
"""
from unittest.mock import MagicMock, patch
from fastapi import HTTPException
import pytest

from forecast.utils.db2_probe import probe_db2_connector


@pytest.fixture(autouse=True)
def _stub_socket():
    """Stub socket.create_connection and getaddrinfo by default."""
    with patch("socket.create_connection"), patch(
        "socket.getaddrinfo",
        return_value=[(2, 1, 6, "", ("192.0.2.1", 8076))],
    ):
        yield


@pytest.mark.unit
def test_probe_succeeds_when_connection_and_table_valid():
    mock_cursor = MagicMock()
    mock_cursor.fetchall.side_effect = [
        [(1,)],  # SELECT 1 FROM SYSIBM.SYSDUMMY1
        [("COL1",), ("COL2",), ("TARGET",)],  # QSYS2.SYSCOLUMNS
    ]
    mock_conn = MagicMock()
    mock_conn.cursor.return_value = mock_cursor

    mock_mapepire = MagicMock()
    mock_mapepire.connect.return_value = mock_conn

    with patch.dict("sys.modules", {"mapepire_python": mock_mapepire}):
        probe_db2_connector(
            host="ibmi.example.com",
            database="DB",
            username="user",
            password="pwd",
            table="SCHEMA.ORDERS",
            columns=["COL1", "COL2", "TARGET"],
        )

    mock_mapepire.connect.assert_called_once()
    mock_conn.close.assert_called_once()


@pytest.mark.unit
def test_probe_fails_on_authentication_error():
    mock_mapepire = MagicMock()
    mock_mapepire.connect.side_effect = Exception("User ID or password incorrect")

    with patch.dict("sys.modules", {"mapepire_python": mock_mapepire}):
        with pytest.raises(HTTPException) as exc_info:
            probe_db2_connector(
                host="ibmi.example.com",
                database="DB",
                username="user",
                password="wrong-pwd",
            )

    assert exc_info.value.status_code == 400
    assert "Authentication failed for user 'user'" in str(exc_info.value.detail)
    assert "wrong-pwd" not in str(exc_info.value.detail)


@pytest.mark.unit
def test_probe_fails_when_table_not_found():
    mock_cursor = MagicMock()
    mock_cursor.fetchall.side_effect = [
        [(1,)],  # SELECT 1
        [],      # QSYS2.SYSCOLUMNS returns empty -> table not found
    ]
    mock_conn = MagicMock()
    mock_conn.cursor.return_value = mock_cursor

    mock_mapepire = MagicMock()
    mock_mapepire.connect.return_value = mock_conn

    with patch.dict("sys.modules", {"mapepire_python": mock_mapepire}):
        with pytest.raises(HTTPException) as exc_info:
            probe_db2_connector(
                host="ibmi.example.com",
                database="DB",
                username="user",
                password="pwd",
                table="NONEXISTENT_TABLE",
                columns=["COL1"],
            )

    assert exc_info.value.status_code == 400
    assert "Table 'NONEXISTENT_TABLE' not found" in str(exc_info.value.detail)


@pytest.mark.unit
def test_probe_fails_when_column_missing():
    mock_cursor = MagicMock()
    mock_cursor.fetchall.side_effect = [
        [(1,)],  # SELECT 1
        [("SCHEMA1", "COL1")],  # QSYS2.SYSCOLUMNS has only COL1 in SCHEMA1
    ]
    mock_conn = MagicMock()
    mock_conn.cursor.return_value = mock_cursor

    mock_mapepire = MagicMock()
    mock_mapepire.connect.return_value = mock_conn

    with patch.dict("sys.modules", {"mapepire_python": mock_mapepire}):
        with pytest.raises(HTTPException) as exc_info:
            probe_db2_connector(
                host="ibmi.example.com",
                database="DB",
                username="user",
                password="pwd",
                table="ORDERS",
                columns=["COL1", "MISSING_COL"],
            )

    assert exc_info.value.status_code == 400
    assert "Columns 'MISSING_COL' not found in table 'ORDERS'" in str(exc_info.value.detail)


@pytest.mark.unit
def test_probe_fails_when_unqualified_table_matches_multiple_schemas():
    mock_cursor = MagicMock()
    mock_cursor.fetchall.side_effect = [
        [(1,)],  # SELECT 1
        [("SCHEMA1", "COL1"), ("SCHEMA2", "COL1")],  # Multi-schema table match
    ]
    mock_conn = MagicMock()
    mock_conn.cursor.return_value = mock_cursor

    mock_mapepire = MagicMock()
    mock_mapepire.connect.return_value = mock_conn

    with patch.dict("sys.modules", {"mapepire_python": mock_mapepire}):
        with pytest.raises(HTTPException) as exc_info:
            probe_db2_connector(
                host="ibmi.example.com",
                database="DB",
                username="user",
                password="pwd",
                table="ORDERS",
                columns=["COL1"],
            )

    assert exc_info.value.status_code == 400
    assert "exists in multiple schemas" in str(exc_info.value.detail)


@pytest.mark.unit
def test_probe_passes_ignore_unauthorized_false_by_default():
    mock_cursor = MagicMock()
    mock_cursor.fetchall.return_value = [(1,)]
    mock_conn = MagicMock()
    mock_conn.cursor.return_value = mock_cursor

    mock_mapepire = MagicMock()
    mock_mapepire.connect.return_value = mock_conn

    with patch.dict("sys.modules", {"mapepire_python": mock_mapepire}):
        probe_db2_connector(
            host="ibmi.example.com",
            database="DB",
            username="user",
            password="pwd",
        )

    daemon_call = mock_mapepire.DaemonServer.call_args.kwargs
    assert daemon_call["ignoreUnauthorized"] is False


@pytest.mark.unit
@pytest.mark.parametrize(
    "forbidden_host,resolved_ip",
    [
        ("127.0.0.1", "127.0.0.1"),
        ("localhost", "127.0.0.1"),
        ("169.254.169.254", "169.254.169.254"),
        ("0.0.0.0", "0.0.0.0"),
        ("::1", "::1"),
        ("evil.example.com", "127.0.0.1"),
        ("evil.example.com", "169.254.1.1"),
    ],
)
def test_probe_rejects_ssrf_hosts(forbidden_host, resolved_ip):
    with patch("socket.getaddrinfo", return_value=[(2, 1, 6, "", (resolved_ip, 8076))]):
        with pytest.raises(HTTPException) as exc_info:
            probe_db2_connector(
                host=forbidden_host,
                database="DB",
                username="user",
                password="pwd",
            )

    assert exc_info.value.status_code == 400
    assert "not a permitted destination" in str(exc_info.value.detail) or "non-permitted address" in str(exc_info.value.detail)


@pytest.mark.unit
def test_tcp_preflight_failure_raises_400():
    from forecast.utils.db2_probe import _tcp_preflight

    with patch("socket.create_connection", side_effect=OSError("connection refused")):
        with pytest.raises(HTTPException) as exc_info:
            _tcp_preflight("ibmi.example.com", 8471, timeout=5)

    assert exc_info.value.status_code == 400
    assert "Cannot connect to Db2 at ibmi.example.com:8471" in str(exc_info.value.detail)


@pytest.mark.unit
def test_probe_skips_gracefully_when_mapepire_not_installed():
    with patch.dict("sys.modules", {"mapepire_python": None}):
        # Should not raise exception
        probe_db2_connector(
            host="ibmi.example.com",
            database="DB",
            username="user",
            password="pwd",
        )
