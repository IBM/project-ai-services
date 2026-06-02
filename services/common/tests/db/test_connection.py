"""
Unit tests for common.db.connection module.

Tests database connection utilities including URL construction,
engine creation, session management, and connection lifecycle.
"""

import os
import pytest
from unittest.mock import Mock, MagicMock, patch, call
from sqlalchemy import Engine
from sqlalchemy.orm import sessionmaker, scoped_session

from common.db.connection import (
    get_database_url,
    create_db_engine,
    create_session_factory,
    create_scoped_session_factory,
    create_session_context_manager,
    create_connection_checker,
    create_connection_closer,
    get_connection_manager,
)


class TestGetDatabaseUrl:
    """Tests for get_database_url function."""
    
    def test_get_database_url_success(self):
        """Test successful database URL construction."""
        with patch.dict(os.environ, {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_PORT': '5432',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'testuser',
            'POSTGRES_PASSWORD': 'testpass'
        }):
            url = get_database_url()
            assert url == 'postgresql://testuser:testpass@localhost:5432/testdb'
    
    def test_get_database_url_with_special_characters(self):
        """Test URL encoding of special characters in credentials."""
        with patch.dict(os.environ, {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_PORT': '5432',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'test@user',
            'POSTGRES_PASSWORD': 'pass:word@123'
        }):
            url = get_database_url()
            assert 'test%40user' in url  # @ encoded as %40
            assert 'pass%3Aword%40123' in url  # : encoded as %3A, @ as %40
    
    def test_get_database_url_default_port(self):
        """Test default port is used when not specified."""
        with patch.dict(os.environ, {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'testuser',
            'POSTGRES_PASSWORD': 'testpass'
        }, clear=True):
            url = get_database_url()
            assert ':5432/' in url
    
    def test_get_database_url_missing_host(self):
        """Test ValueError raised when POSTGRES_HOST is missing."""
        with patch.dict(os.environ, {
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'testuser',
            'POSTGRES_PASSWORD': 'testpass'
        }, clear=True):
            with pytest.raises(ValueError) as exc_info:
                get_database_url()
            assert 'POSTGRES_HOST' in str(exc_info.value)
    
    def test_get_database_url_missing_database(self):
        """Test ValueError raised when POSTGRES_DB is missing."""
        with patch.dict(os.environ, {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_USER': 'testuser',
            'POSTGRES_PASSWORD': 'testpass'
        }, clear=True):
            with pytest.raises(ValueError) as exc_info:
                get_database_url()
            assert 'POSTGRES_DB' in str(exc_info.value)
    
    def test_get_database_url_missing_user(self):
        """Test ValueError raised when POSTGRES_USER is missing."""
        with patch.dict(os.environ, {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_PASSWORD': 'testpass'
        }, clear=True):
            with pytest.raises(ValueError) as exc_info:
                get_database_url()
            assert 'POSTGRES_USER' in str(exc_info.value)
    
    def test_get_database_url_missing_password(self):
        """Test ValueError raised when POSTGRES_PASSWORD is missing."""
        with patch.dict(os.environ, {
            'POSTGRES_HOST': 'localhost',
            'POSTGRES_DB': 'testdb',
            'POSTGRES_USER': 'testuser'
        }, clear=True):
            with pytest.raises(ValueError) as exc_info:
                get_database_url()
            assert 'POSTGRES_PASSWORD' in str(exc_info.value)
    
    def test_get_database_url_missing_multiple(self):
        """Test ValueError lists all missing variables."""
        with patch.dict(os.environ, {}, clear=True):
            with pytest.raises(ValueError) as exc_info:
                get_database_url()
            error_msg = str(exc_info.value)
            assert 'POSTGRES_HOST' in error_msg
            assert 'POSTGRES_DB' in error_msg
            assert 'POSTGRES_USER' in error_msg
            assert 'POSTGRES_PASSWORD' in error_msg


class TestCreateDbEngine:
    """Tests for create_db_engine function."""
    
    @patch('common.db.connection.get_database_url')
    @patch('common.db.connection.create_engine')
    @patch('common.db.connection.get_logger')
    @patch('common.db.connection.event')
    def test_create_db_engine_success(self, mock_event, mock_get_logger, mock_create_engine, mock_get_url):
        """Test successful engine creation."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_get_url.return_value = 'postgresql://user:pass@localhost:5432/db'
        mock_engine = Mock(spec=Engine)
        mock_engine.pool = Mock()  # Add pool attribute
        mock_create_engine.return_value = mock_engine
        
        engine = create_db_engine(logger_name='test_db')
        
        assert engine == mock_engine
        mock_get_logger.assert_called_once_with('test_db')
        mock_create_engine.assert_called_once()
        mock_logger.info.assert_called()
    
    @patch('common.db.connection.get_database_url')
    @patch('common.db.connection.create_engine')
    @patch('common.db.connection.get_logger')
    @patch('common.db.connection.event')
    def test_create_db_engine_custom_pool_config(self, mock_event, mock_get_logger, mock_create_engine, mock_get_url):
        """Test engine creation with custom pool configuration."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_get_url.return_value = 'postgresql://user:pass@localhost:5432/db'
        mock_engine = Mock(spec=Engine)
        mock_engine.pool = Mock()  # Add pool attribute
        mock_create_engine.return_value = mock_engine
        
        engine = create_db_engine(
            pool_size=10,
            max_overflow=20,
            pool_timeout=60,
            pool_recycle=7200
        )
        
        # Verify create_engine was called with correct pool parameters
        call_kwargs = mock_create_engine.call_args[1]
        assert call_kwargs['pool_size'] == 10
        assert call_kwargs['max_overflow'] == 20
        assert call_kwargs['pool_timeout'] == 60
        assert call_kwargs['pool_recycle'] == 7200
    
    @patch('common.db.connection.get_database_url')
    @patch('common.db.connection.create_engine')
    @patch('common.db.connection.get_logger')
    @patch('common.db.connection.event')
    def test_create_db_engine_masks_password_in_logs(self, mock_event, mock_get_logger, mock_create_engine, mock_get_url):
        """Test that password is masked in log messages."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_get_url.return_value = 'postgresql://user:secretpass@localhost:5432/db'
        mock_engine = Mock(spec=Engine)
        mock_engine.pool = Mock()  # Add pool attribute
        mock_create_engine.return_value = mock_engine
        
        create_db_engine()
        
        # Check that logged URL has masked password
        log_calls = [str(call) for call in mock_logger.info.call_args_list]
        assert any('****' in str(call) for call in log_calls)
        assert not any('secretpass' in str(call) for call in log_calls)


class TestSessionFactories:
    """Tests for session factory creation functions."""
    
    def test_create_session_factory(self):
        """Test session factory creation."""
        mock_engine = Mock(spec=Engine)
        
        factory = create_session_factory(mock_engine)
        
        assert isinstance(factory, sessionmaker)
        assert factory.kw['bind'] == mock_engine
        assert factory.kw['autocommit'] is False
        assert factory.kw['autoflush'] is False
    
    def test_create_scoped_session_factory(self):
        """Test scoped session factory creation."""
        mock_engine = Mock(spec=Engine)
        session_factory = create_session_factory(mock_engine)
        
        scoped_factory = create_scoped_session_factory(session_factory)
        
        assert isinstance(scoped_factory, scoped_session)


class TestSessionContextManager:
    """Tests for session context manager."""
    
    def test_create_session_context_manager_success(self):
        """Test successful session context manager creation and usage."""
        mock_session = MagicMock()
        mock_factory = Mock()
        mock_factory.return_value = mock_session
        
        get_session = create_session_context_manager(mock_factory)
        
        with get_session() as session:
            assert session == mock_session
        
        mock_session.commit.assert_called_once()
        mock_session.close.assert_called_once()
    
    def test_create_session_context_manager_rollback_on_error(self):
        """Test session rollback on exception."""
        mock_session = MagicMock()
        mock_factory = Mock()
        mock_factory.return_value = mock_session
        
        get_session = create_session_context_manager(mock_factory)
        
        with pytest.raises(ValueError):
            with get_session() as session:
                raise ValueError("Test error")
        
        mock_session.rollback.assert_called_once()
        mock_session.close.assert_called_once()
        mock_session.commit.assert_not_called()
    
    def test_create_session_context_manager_no_factory(self):
        """Test RuntimeError when factory is None."""
        get_session = create_session_context_manager(None)
        
        with pytest.raises(RuntimeError) as exc_info:
            with get_session():
                pass
        
        assert 'Database not initialized' in str(exc_info.value)


class TestConnectionChecker:
    """Tests for connection checker function."""
    
    @patch('common.db.connection.get_logger')
    def test_create_connection_checker_success(self, mock_get_logger):
        """Test successful connection check."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_engine = Mock(spec=Engine)
        mock_conn = MagicMock()
        mock_engine.connect.return_value.__enter__ = Mock(return_value=mock_conn)
        mock_engine.connect.return_value.__exit__ = Mock(return_value=None)
        
        check_connection = create_connection_checker(mock_engine, 'test_db')
        result = check_connection()
        
        assert result is True
        mock_conn.execute.assert_called_once()
        mock_logger.info.assert_called()
    
    @patch('common.db.connection.get_logger')
    def test_create_connection_checker_no_engine(self, mock_get_logger):
        """Test connection check with no engine."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        
        check_connection = create_connection_checker(None, 'test_db')
        result = check_connection()
        
        assert result is False
        mock_logger.error.assert_called()
    
    @patch('common.db.connection.get_logger')
    def test_create_connection_checker_connection_error(self, mock_get_logger):
        """Test connection check with connection error."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_engine = Mock(spec=Engine)
        mock_engine.connect.side_effect = Exception("Connection failed")
        
        check_connection = create_connection_checker(mock_engine, 'test_db')
        result = check_connection()
        
        assert result is False
        mock_logger.error.assert_called()


class TestConnectionCloser:
    """Tests for connection closer function."""
    
    @patch('common.db.connection.get_logger')
    def test_create_connection_closer_success(self, mock_get_logger):
        """Test successful connection closure."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_engine = Mock(spec=Engine)
        
        close_connections = create_connection_closer(mock_engine, 'test_db')
        close_connections()
        
        mock_engine.dispose.assert_called_once()
        mock_logger.info.assert_called()
    
    @patch('common.db.connection.get_logger')
    def test_create_connection_closer_no_engine(self, mock_get_logger):
        """Test connection closer with no engine."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        
        close_connections = create_connection_closer(None, 'test_db')
        close_connections()
        
        # Should not raise error, just not call dispose
        mock_logger.info.assert_not_called()


class TestGetConnectionManager:
    """Tests for get_connection_manager factory function."""
    
    @patch('common.db.connection.create_db_engine')
    @patch('common.db.connection.get_logger')
    def test_get_connection_manager_success(self, mock_get_logger, mock_create_engine):
        """Test successful connection manager creation."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_engine = Mock(spec=Engine)
        mock_create_engine.return_value = mock_engine
        
        result = get_connection_manager('test_db', None)
        
        assert len(result) == 6
        engine, session_local, scoped_session, get_session, check_conn, close_conn = result
        
        assert engine == mock_engine
        assert isinstance(session_local, sessionmaker)
        assert isinstance(scoped_session, scoped_session.__class__)
        assert callable(get_session)
        assert callable(check_conn)
        assert callable(close_conn)
    
    @patch('common.db.connection.create_db_engine')
    @patch('common.db.connection.get_logger')
    def test_get_connection_manager_with_settings(self, mock_get_logger, mock_create_engine):
        """Test connection manager with settings object."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_engine = Mock(spec=Engine)
        mock_create_engine.return_value = mock_engine
        
        # Mock settings object
        mock_settings = Mock()
        mock_settings.database.pool_size = 10
        mock_settings.database.max_overflow = 20
        mock_settings.database.pool_timeout = 60
        mock_settings.database.pool_recycle = 7200
        
        result = get_connection_manager('test_db', mock_settings)
        
        # Verify engine was created with settings
        mock_create_engine.assert_called_once()
        call_kwargs = mock_create_engine.call_args[1]
        assert call_kwargs['pool_size'] == 10
        assert call_kwargs['max_overflow'] == 20
    
    @patch('common.db.connection.create_db_engine')
    @patch('common.db.connection.get_logger')
    def test_get_connection_manager_error_handling(self, mock_get_logger, mock_create_engine):
        """Test connection manager handles initialization errors."""
        mock_logger = Mock()
        mock_get_logger.return_value = mock_logger
        mock_create_engine.side_effect = ValueError("Missing env vars")
        
        result = get_connection_manager('test_db', None)
        
        # Should return None values on error
        assert all(item is None for item in result)
        mock_logger.warning.assert_called()


# Made with Bob