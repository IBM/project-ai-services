"""
Pytest configuration and fixtures for common module tests.

Provides shared fixtures and test utilities for testing common database code.
"""

import pytest
from unittest.mock import Mock, MagicMock, patch
import os


@pytest.fixture(autouse=True)
def mock_env_vars():
    """
    Mock environment variables for database tests.
    This fixture is automatically used for all tests.
    """
    env_vars = {
        'POSTGRES_HOST': 'localhost',
        'POSTGRES_PORT': '5432',
        'POSTGRES_DB': 'testdb',
        'POSTGRES_USER': 'testuser',
        'POSTGRES_PASSWORD': 'testpass'
    }
    
    with patch.dict(os.environ, env_vars, clear=False):
        yield env_vars


@pytest.fixture
def mock_logger():
    """Provide a mock logger for testing."""
    logger = Mock()
    logger.info = Mock()
    logger.debug = Mock()
    logger.warning = Mock()
    logger.error = Mock()
    return logger


@pytest.fixture
def mock_settings():
    """Provide a mock settings object with database configuration."""
    settings = Mock()
    settings.database = Mock()
    settings.database.pool_size = 5
    settings.database.max_overflow = 10
    settings.database.pool_timeout = 30
    settings.database.pool_recycle = 3600
    return settings


# Made with Bob