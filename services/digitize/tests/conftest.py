"""
Pytest configuration and fixtures for digitize service tests.

This module provides comprehensive DB mocking to ensure tests run without
requiring an actual PostgreSQL database connection.
"""

import pytest
from unittest.mock import Mock, MagicMock, patch
from datetime import datetime, timezone
from types import SimpleNamespace


@pytest.fixture(autouse=True)
def mock_database_engine():
    """
    Mock the database engine to prevent actual database connections.
    This fixture is automatically used for all tests.
    """
    mock_engine = Mock()
    mock_engine.dispose = Mock()
    
    with patch('digitize.db.connection.engine', mock_engine):
        with patch('digitize.db_operations.engine', mock_engine):
            yield mock_engine


@pytest.fixture(autouse=True)
def mock_db_session():
    """
    Mock database session context manager.
    This fixture is automatically used for all tests.
    """
    mock_session = MagicMock()
    mock_session.commit = Mock()
    mock_session.rollback = Mock()
    mock_session.close = Mock()
    mock_session.add = Mock()
    mock_session.flush = Mock()
    mock_session.scalar = Mock(return_value=None)
    mock_session.scalars = Mock(return_value=Mock(all=Mock(return_value=[])))
    mock_session.execute = Mock(return_value=Mock(rowcount=0))
    mock_session.expunge = Mock()
    
    with patch('digitize.db.connection.get_db_session') as mock_get_session:
        mock_get_session.return_value.__enter__ = Mock(return_value=mock_session)
        mock_get_session.return_value.__exit__ = Mock(return_value=None)
        yield mock_session


@pytest.fixture
def mock_db_manager():
    """
    Mock the DatabaseManager singleton for comprehensive DB operation mocking.
    """
    mock_manager = Mock()
    
    # Mock job operations
    mock_manager.create_job = Mock(return_value=Mock(
        job_id="test-job-123",
        job_name=None,
        operation="digitization",
        status="accepted",
        submitted_at=datetime.now(timezone.utc),
        completed_at=None,
        stats={"total_documents": 0, "completed": 0, "failed": 0, "in_progress": 0},
        error=None,
        updated_at=datetime.now(timezone.utc)
    ))
    
    mock_manager.get_job_by_id = Mock(return_value=None)
    mock_manager.get_all_jobs = Mock(return_value=([], 0))
    mock_manager.update_job = Mock(return_value=True)
    mock_manager.delete_job = Mock(return_value=True)
    mock_manager.get_active_jobs = Mock(return_value=[])
    mock_manager.delete_all_jobs = Mock(return_value={"deleted_count": 0, "success": True})
    
    # Mock document operations
    mock_manager.create_document = Mock(return_value=Mock(
        doc_id="test-doc-123",
        job_id="test-job-123",
        name="test.pdf",
        type="digitization",
        status="accepted",
        output_format="json",
        submitted_at=datetime.now(timezone.utc),
        completed_at=None,
        error=None,
        doc_metadata={},
        updated_at=datetime.now(timezone.utc)
    ))
    
    mock_manager.get_document_by_id = Mock(return_value=None)
    mock_manager.get_all_documents = Mock(return_value=([], 0))
    mock_manager.get_documents_by_job_id = Mock(return_value=[])
    mock_manager.update_document = Mock(return_value=True)
    mock_manager.delete_document = Mock(return_value=True)
    mock_manager.delete_all_documents = Mock(return_value={"deleted_count": 0, "success": True})
    
    with patch('digitize.db.manager.db_manager', mock_manager):
        with patch('digitize.db_operations.db_manager', mock_manager):
            yield mock_manager


@pytest.fixture
def mock_status_manager():
    """
    Mock DatabaseStatusManager for status update operations.
    """
    mock_manager = Mock()
    mock_manager.update_doc_metadata = Mock()
    mock_manager.update_job_progress = Mock()
    mock_manager._update_document = Mock()
    mock_manager._update_job = Mock()
    
    with patch('digitize.db_operations.DatabaseStatusManager', return_value=mock_manager):
        with patch('digitize.db_operations.get_status_manager', return_value=mock_manager):
            yield mock_manager


@pytest.fixture(autouse=True)
def mock_db_operations():
    """
    Mock all db_operations functions to prevent database access.
    This fixture is automatically used for all tests.
    """
    with patch('digitize.db_operations.create_job') as mock_create_job, \
         patch('digitize.db_operations.get_job') as mock_get_job, \
         patch('digitize.db_operations.get_all_jobs') as mock_get_all_jobs, \
         patch('digitize.db_operations.create_document') as mock_create_document, \
         patch('digitize.db_operations.get_document') as mock_get_document, \
         patch('digitize.db_operations.get_all_documents_paginated') as mock_get_all_docs, \
         patch('digitize.db_operations.get_all_document_ids') as mock_get_doc_ids, \
         patch('digitize.db_operations.get_status_manager') as mock_get_status_mgr:
        
        # Set default return values
        mock_create_job.return_value = None
        mock_get_job.return_value = None
        mock_get_all_jobs.return_value = ([], 0)
        mock_create_document.return_value = None
        mock_get_document.return_value = None
        mock_get_all_docs.return_value = ([], 0)
        mock_get_doc_ids.return_value = []
        
        # Mock status manager
        mock_status_mgr = Mock()
        mock_status_mgr.update_doc_metadata = Mock()
        mock_status_mgr.update_job_progress = Mock()
        mock_get_status_mgr.return_value = mock_status_mgr
        
        yield {
            'create_job': mock_create_job,
            'get_job': mock_get_job,
            'get_all_jobs': mock_get_all_jobs,
            'create_document': mock_create_document,
            'get_document': mock_get_document,
            'get_all_documents_paginated': mock_get_all_docs,
            'get_all_document_ids': mock_get_doc_ids,
            'get_status_manager': mock_get_status_mgr
        }


@pytest.fixture
def sample_job_data():
    """
    Provide sample job data for testing.
    """
    return {
        "job_id": "test-job-123",
        "job_name": "Test Job",
        "operation": "digitization",
        "status": "completed",
        "submitted_at": "2024-01-01T00:00:00Z",
        "completed_at": "2024-01-01T01:00:00Z",
        "documents": [
            {
                "id": "doc-1",
                "name": "test.pdf",
                "status": "completed"
            }
        ],
        "stats": {
            "total_documents": 1,
            "completed": 1,
            "failed": 0,
            "in_progress": 0
        },
        "error": None
    }


@pytest.fixture
def sample_document_data():
    """
    Provide sample document data for testing.
    """
    return {
        "id": "doc-1",
        "job_id": "test-job-123",
        "name": "test.pdf",
        "type": "digitization",
        "status": "completed",
        "output_format": "json",
        "submitted_at": "2024-01-01T00:00:00Z",
        "completed_at": "2024-01-01T01:00:00Z",
        "error": None,
        "metadata": {
            "pages": 5,
            "tables": 2,
            "timing_in_secs": {
                "digitizing": 10.5,
                "processing": 5.2,
                "chunking": 3.1,
                "indexing": 2.8
            }
        }
    }


@pytest.fixture
def mock_db_document():
    """
    Create a mock database Document object.
    """
    mock_doc = Mock()
    mock_doc.doc_id = "doc-1"
    mock_doc.job_id = "test-job-123"
    mock_doc.name = "test.pdf"
    mock_doc.type = "digitization"
    mock_doc.status = "completed"
    mock_doc.output_format = "json"
    mock_doc.submitted_at = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    mock_doc.completed_at = datetime(2024, 1, 1, 1, 0, 0, tzinfo=timezone.utc)
    mock_doc.error = None
    mock_doc.doc_metadata = {
        "pages": 5,
        "tables": 2,
        "timing_in_secs": {
            "digitizing": 10.5,
            "processing": 5.2,
            "chunking": 3.1,
            "indexing": 2.8
        }
    }
    mock_doc.updated_at = datetime(2024, 1, 1, 1, 0, 0, tzinfo=timezone.utc)
    return mock_doc


@pytest.fixture
def mock_db_job():
    """
    Create a mock database Job object.
    """
    mock_job = Mock()
    mock_job.job_id = "test-job-123"
    mock_job.job_name = "Test Job"
    mock_job.operation = "digitization"
    mock_job.status = "completed"
    mock_job.submitted_at = datetime(2024, 1, 1, 0, 0, 0, tzinfo=timezone.utc)
    mock_job.completed_at = datetime(2024, 1, 1, 1, 0, 0, tzinfo=timezone.utc)
    mock_job.error = None
    mock_job.stats = {
        "total_documents": 1,
        "completed": 1,
        "failed": 0,
        "in_progress": 0
    }
    mock_job.updated_at = datetime(2024, 1, 1, 1, 0, 0, tzinfo=timezone.utc)
    return mock_job

# Made with Bob
