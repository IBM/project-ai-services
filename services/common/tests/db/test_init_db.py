"""
Unit tests for common.db.scripts.init_db module.

Tests database initialization script including PostgreSQL connection,
database creation, schema initialization, and table verification.
"""

import pytest
from unittest.mock import Mock, MagicMock, patch, call
from pathlib import Path
import psycopg2

from common.db.scripts.init_db import (
    get_env_var,
    get_connection,
    wait_for_postgres,
    database_exists,
    create_database,
    initialize_schema,
    main,
)


class TestGetEnvVar:
    """Tests for get_env_var function."""
    
    def test_get_env_var_exists(self):
        """Test getting existing environment variable."""
        with patch('os.getenv', return_value='test_value'):
            result = get_env_var('TEST_VAR')
            assert result == 'test_value'
    
    def test_get_env_var_with_default(self):
        """Test getting environment variable with default value."""
        with patch('os.getenv', return_value='default_value'):
            result = get_env_var('TEST_VAR', 'default_value')
            assert result == 'default_value'
    
    def test_get_env_var_missing_required(self):
        """Test that missing required variable exits."""
        with patch('os.getenv', return_value=None):
            with pytest.raises(SystemExit):
                get_env_var('REQUIRED_VAR')


class TestGetConnection:
    """Tests for get_connection context manager."""
    
    @patch('psycopg2.connect')
    def test_get_connection_success(self, mock_connect):
        """Test successful database connection."""
        mock_conn = Mock()
        mock_connect.return_value = mock_conn
        
        with get_connection('localhost', '5432', 'testdb', 'user', 'pass') as conn:
            assert conn == mock_conn
        
        mock_connect.assert_called_once_with(
            host='localhost',
            port='5432',
            database='testdb',
            user='user',
            password='pass',
            connect_timeout=5
        )
        mock_conn.close.assert_called_once()
    
    @patch('psycopg2.connect')
    def test_get_connection_closes_on_error(self, mock_connect):
        """Test connection is closed even on error."""
        mock_conn = Mock()
        mock_connect.return_value = mock_conn
        
        with pytest.raises(ValueError):
            with get_connection('localhost', '5432', 'testdb', 'user', 'pass'):
                raise ValueError("Test error")
        
        mock_conn.close.assert_called_once()


class TestWaitForPostgres:
    """Tests for wait_for_postgres function."""
    
    @patch('common.db.scripts.init_db.get_connection')
    @patch('time.sleep')
    def test_wait_for_postgres_immediate_success(self, mock_sleep, mock_get_conn):
        """Test successful connection on first attempt."""
        mock_conn = MagicMock()
        mock_get_conn.return_value.__enter__ = Mock(return_value=mock_conn)
        mock_get_conn.return_value.__exit__ = Mock(return_value=None)
        
        result = wait_for_postgres('localhost', '5432', 'user', 'pass', max_attempts=5)
        
        assert result is True
        mock_sleep.assert_not_called()
    
    @patch('common.db.scripts.init_db.get_connection')
    @patch('time.sleep')
    def test_wait_for_postgres_retry_then_success(self, mock_sleep, mock_get_conn):
        """Test successful connection after retries."""
        # Fail twice, then succeed
        mock_conn = MagicMock()
        mock_get_conn.side_effect = [
            psycopg2.OperationalError("Not ready"),
            psycopg2.OperationalError("Not ready"),
            MagicMock(__enter__=Mock(return_value=mock_conn), __exit__=Mock(return_value=None))
        ]
        
        result = wait_for_postgres('localhost', '5432', 'user', 'pass', max_attempts=5)
        
        assert result is True
        assert mock_sleep.call_count == 2
    
    @patch('common.db.scripts.init_db.get_connection')
    @patch('time.sleep')
    def test_wait_for_postgres_max_attempts_exceeded(self, mock_sleep, mock_get_conn):
        """Test failure after max attempts."""
        mock_get_conn.side_effect = psycopg2.OperationalError("Connection failed")
        
        result = wait_for_postgres('localhost', '5432', 'user', 'pass', max_attempts=3)
        
        assert result is False
        assert mock_sleep.call_count == 2  # max_attempts - 1


class TestDatabaseExists:
    """Tests for database_exists function."""
    
    def test_database_exists_true(self):
        """Test when database exists."""
        mock_conn = Mock()
        mock_cursor = Mock()
        mock_cursor.fetchone.return_value = (1,)
        mock_conn.cursor.return_value = mock_cursor
        
        result = database_exists(mock_conn, 'testdb')
        
        assert result is True
        mock_cursor.execute.assert_called_once()
        mock_cursor.close.assert_called_once()
    
    def test_database_exists_false(self):
        """Test when database does not exist."""
        mock_conn = Mock()
        mock_cursor = Mock()
        mock_cursor.fetchone.return_value = None
        mock_conn.cursor.return_value = mock_cursor
        
        result = database_exists(mock_conn, 'testdb')
        
        assert result is False


class TestCreateDatabase:
    """Tests for create_database function."""
    
    @patch('common.db.scripts.init_db.get_connection')
    @patch('common.db.scripts.init_db.database_exists')
    def test_create_database_already_exists(self, mock_db_exists, mock_get_conn):
        """Test when database already exists."""
        mock_conn = MagicMock()
        mock_get_conn.return_value.__enter__ = Mock(return_value=mock_conn)
        mock_get_conn.return_value.__exit__ = Mock(return_value=None)
        mock_db_exists.return_value = True
        
        result = create_database('localhost', '5432', 'user', 'pass', 'testdb')
        
        assert result is True
        mock_conn.cursor.assert_not_called()
    
    @patch('common.db.scripts.init_db.get_connection')
    @patch('common.db.scripts.init_db.database_exists')
    def test_create_database_success(self, mock_db_exists, mock_get_conn):
        """Test successful database creation."""
        mock_conn = MagicMock()
        mock_cursor = Mock()
        mock_conn.cursor.return_value = mock_cursor
        mock_get_conn.return_value.__enter__ = Mock(return_value=mock_conn)
        mock_get_conn.return_value.__exit__ = Mock(return_value=None)
        mock_db_exists.return_value = False
        
        result = create_database('localhost', '5432', 'user', 'pass', 'testdb')
        
        assert result is True
        mock_conn.set_isolation_level.assert_called_once()
        mock_cursor.execute.assert_called_once()
        mock_cursor.close.assert_called_once()
    
    @patch('common.db.scripts.init_db.get_connection')
    def test_create_database_error(self, mock_get_conn):
        """Test database creation error handling."""
        mock_get_conn.side_effect = Exception("Connection error")
        
        result = create_database('localhost', '5432', 'user', 'pass', 'testdb')
        
        assert result is False


class TestInitializeSchema:
    """Tests for initialize_schema function."""
    
    @patch('common.db.scripts.init_db.get_connection')
    def test_initialize_schema_success(self, mock_get_conn):
        """Test successful schema initialization."""
        # Create a temporary schema file
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.sql', delete=False) as f:
            f.write('CREATE TABLE test_table (id INT);')
            schema_file = Path(f.name)
        
        try:
            mock_conn = MagicMock()
            mock_cursor = Mock()
            mock_cursor.fetchall.return_value = [('test_table',), ('expected_table',)]
            mock_conn.cursor.return_value = mock_cursor
            mock_get_conn.return_value.__enter__ = Mock(return_value=mock_conn)
            mock_get_conn.return_value.__exit__ = Mock(return_value=None)
            
            expected_tables = {'test_table', 'expected_table'}
            result = initialize_schema(
                'localhost', '5432', 'user', 'pass', 'testdb',
                schema_file, expected_tables
            )
            
            assert result is True
            assert mock_cursor.execute.call_count == 2  # Schema SQL + verification query
            mock_conn.commit.assert_called_once()
        finally:
            schema_file.unlink()
    
    @patch('common.db.scripts.init_db.get_connection')
    def test_initialize_schema_missing_tables(self, mock_get_conn):
        """Test schema initialization with missing tables."""
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.sql', delete=False) as f:
            f.write('CREATE TABLE test_table (id INT);')
            schema_file = Path(f.name)
        
        try:
            mock_conn = MagicMock()
            mock_cursor = Mock()
            mock_cursor.fetchall.return_value = [('test_table',)]
            mock_conn.cursor.return_value = mock_cursor
            mock_get_conn.return_value.__enter__ = Mock(return_value=mock_conn)
            mock_get_conn.return_value.__exit__ = Mock(return_value=None)
            
            expected_tables = {'test_table', 'missing_table'}
            result = initialize_schema(
                'localhost', '5432', 'user', 'pass', 'testdb',
                schema_file, expected_tables
            )
            
            assert result is False
        finally:
            schema_file.unlink()
    
    def test_initialize_schema_file_not_found(self):
        """Test schema initialization with missing file."""
        schema_file = Path('/nonexistent/schema.sql')
        expected_tables = {'test_table'}
        
        result = initialize_schema(
            'localhost', '5432', 'user', 'pass', 'testdb',
            schema_file, expected_tables
        )
        
        assert result is False
    
    @patch('common.db.scripts.init_db.get_connection')
    def test_initialize_schema_error(self, mock_get_conn):
        """Test schema initialization error handling."""
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.sql', delete=False) as f:
            f.write('CREATE TABLE test_table (id INT);')
            schema_file = Path(f.name)
        
        try:
            mock_get_conn.side_effect = Exception("Connection error")
            expected_tables = {'test_table'}
            
            result = initialize_schema(
                'localhost', '5432', 'user', 'pass', 'testdb',
                schema_file, expected_tables
            )
            
            assert result is False
        finally:
            schema_file.unlink()


class TestMain:
    """Tests for main function."""
    
    @patch('common.db.scripts.init_db.initialize_schema')
    @patch('common.db.scripts.init_db.create_database')
    @patch('common.db.scripts.init_db.wait_for_postgres')
    @patch('common.db.scripts.init_db.get_env_var')
    def test_main_success(self, mock_get_env, mock_wait, mock_create_db, mock_init_schema):
        """Test successful main execution."""
        # Mock environment variables
        mock_get_env.side_effect = lambda name, default=None: {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_PORT': '5432',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'user',
            'POSTGRES_PASSWORD': 'pass'
        }.get(name, default)
        
        mock_wait.return_value = True
        mock_create_db.return_value = True
        mock_init_schema.return_value = True
        
        # Create a temporary schema file
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.sql', delete=False) as f:
            f.write('CREATE TABLE test_table (id INT);')
            schema_file = Path(f.name)
        
        try:
            expected_tables = {'test_table'}
            
            with pytest.raises(SystemExit) as exc_info:
                main(schema_file, expected_tables)
            
            assert exc_info.value.code == 0
            mock_wait.assert_called_once()
            mock_create_db.assert_called_once()
            mock_init_schema.assert_called_once()
        finally:
            schema_file.unlink()
    
    @patch('common.db.scripts.init_db.wait_for_postgres')
    @patch('common.db.scripts.init_db.get_env_var')
    def test_main_postgres_not_ready(self, mock_get_env, mock_wait):
        """Test main exits when PostgreSQL is not ready."""
        mock_get_env.side_effect = lambda name, default=None: {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_PORT': '5432',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'user',
            'POSTGRES_PASSWORD': 'pass'
        }.get(name, default)
        
        mock_wait.return_value = False
        
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.sql', delete=False) as f:
            schema_file = Path(f.name)
        
        try:
            expected_tables = {'test_table'}
            
            with pytest.raises(SystemExit) as exc_info:
                main(schema_file, expected_tables)
            
            assert exc_info.value.code == 1
        finally:
            schema_file.unlink()
    
    @patch('common.db.scripts.init_db.create_database')
    @patch('common.db.scripts.init_db.wait_for_postgres')
    @patch('common.db.scripts.init_db.get_env_var')
    def test_main_database_creation_fails(self, mock_get_env, mock_wait, mock_create_db):
        """Test main exits when database creation fails."""
        mock_get_env.side_effect = lambda name, default=None: {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_PORT': '5432',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'user',
            'POSTGRES_PASSWORD': 'pass'
        }.get(name, default)
        
        mock_wait.return_value = True
        mock_create_db.return_value = False
        
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.sql', delete=False) as f:
            schema_file = Path(f.name)
        
        try:
            expected_tables = {'test_table'}
            
            with pytest.raises(SystemExit) as exc_info:
                main(schema_file, expected_tables)
            
            assert exc_info.value.code == 1
        finally:
            schema_file.unlink()
    
    @patch('common.db.scripts.init_db.initialize_schema')
    @patch('common.db.scripts.init_db.create_database')
    @patch('common.db.scripts.init_db.wait_for_postgres')
    @patch('common.db.scripts.init_db.get_env_var')
    def test_main_schema_initialization_fails(self, mock_get_env, mock_wait, mock_create_db, mock_init_schema):
        """Test main exits when schema initialization fails."""
        mock_get_env.side_effect = lambda name, default=None: {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_PORT': '5432',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'user',
            'POSTGRES_PASSWORD': 'pass'
        }.get(name, default)
        
        mock_wait.return_value = True
        mock_create_db.return_value = True
        mock_init_schema.return_value = False
        
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.sql', delete=False) as f:
            schema_file = Path(f.name)
        
        try:
            expected_tables = {'test_table'}
            
            with pytest.raises(SystemExit) as exc_info:
                main(schema_file, expected_tables)
            
            assert exc_info.value.code == 1
        finally:
            schema_file.unlink()


# Made with Bob