"""
Unit tests for invoice processing health check and basic lifespan.
"""

from unittest.mock import MagicMock, patch
import pytest
import requests

from app import _register_invoice_schema
from settings import settings


def test_health_check(client):
    """Test health check returns 200 and status ok."""
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_stubs_return_501(client):
    """Test API stub endpoints return 501 Not Implemented."""
    res_get = client.get("/v1/invoices/test-job-id")
    assert res_get.status_code == 501

    res_list = client.get("/v1/invoices")
    assert res_list.status_code == 501


def test_register_schema_success():
    """Test schema registration succeeds on 201."""
    with patch.object(settings.invoice, "extract_url", "http://extract-test:6000"):
        mock_resp = MagicMock(status_code=201)
        with patch("requests.post", return_value=mock_resp) as mock_post:
            _register_invoice_schema()
            mock_post.assert_called_once()


def test_register_schema_idempotent_409():
    """Test schema registration succeeds idempotently on 409."""
    with patch.object(settings.invoice, "extract_url", "http://extract-test:6000"):
        mock_resp = MagicMock(status_code=409)
        with patch("requests.post", return_value=mock_resp) as mock_post:
            _register_invoice_schema()
            mock_post.assert_called_once()


def test_register_schema_failure_raises():
    """Test schema registration raises RuntimeError on other HTTP status."""
    with patch.object(settings.invoice, "extract_url", "http://extract-test:6000"):
        mock_resp = MagicMock(status_code=500, text="Internal Server Error")
        with patch("requests.post", return_value=mock_resp):
            with pytest.raises(RuntimeError, match="Failed to register invoice schema"):
                _register_invoice_schema()


def test_register_schema_network_error_raises():
    """Test schema registration raises RuntimeError on connection error."""
    with patch.object(settings.invoice, "extract_url", "http://extract-test:6000"):
        with patch("requests.post", side_effect=requests.RequestException("Connection refused")):
            with pytest.raises(RuntimeError, match="Network error connecting to extract service"):
                _register_invoice_schema()
