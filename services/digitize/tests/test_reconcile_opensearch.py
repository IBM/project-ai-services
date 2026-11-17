"""Unit tests for OpenSearch orphan reconciliation logic and endpoints."""

from contextlib import contextmanager
from unittest.mock import MagicMock, Mock, patch
import pytest
from fastapi.testclient import TestClient

from common.opensearch import OpensearchVectorStore
from digitize.app import app
from digitize.db.manager import DatabaseManager
from digitize.utils.recovery import reconcile_opensearch_orphans, _reconcile_lock


class TestOpensearchGetAllIndexedDocIds:
    def test_returns_empty_when_index_not_found(self):
        with patch("common.opensearch.OpenSearch") as mock_os_cls:
            mock_client = MagicMock()
            mock_client.indices.exists.return_value = False
            mock_os_cls.return_value = mock_client

            store = OpensearchVectorStore()
            store.client = mock_client

            doc_ids, after_key = store.get_all_indexed_doc_ids(page_size=100)
            assert doc_ids == []
            assert after_key is None

    def test_returns_doc_ids_and_after_key(self):
        with patch("common.opensearch.OpenSearch") as mock_os_cls:
            mock_client = MagicMock()
            mock_client.indices.exists.return_value = True
            mock_client.search.return_value = {
                "aggregations": {
                    "unique_docs": {
                        "after_key": {"doc_id": "doc-2"},
                        "buckets": [
                            {"key": {"doc_id": "doc-1"}},
                            {"key": {"doc_id": "doc-2"}},
                        ],
                    }
                }
            }
            mock_os_cls.return_value = mock_client

            store = OpensearchVectorStore()
            store.client = mock_client

            doc_ids, after_key = store.get_all_indexed_doc_ids(page_size=2)
            assert doc_ids == ["doc-1", "doc-2"]
            assert after_key == {"doc_id": "doc-2"}


class TestDatabaseManagerGetExistingDocIds:
    def test_empty_list_returns_empty_set(self):
        res = DatabaseManager.get_existing_doc_ids([])
        assert res == set()

    def test_returns_matching_doc_ids(self):
        mock_session = MagicMock()
        doc_scalars = MagicMock()
        doc_scalars.all.return_value = ["doc-exists-1"]
        mock_session.scalars.return_value = doc_scalars

        @contextmanager
        def _fake_session():
            yield mock_session

        with patch("digitize.db.manager.get_db_session", side_effect=lambda: _fake_session()):
            res = DatabaseManager.get_existing_doc_ids(["doc-exists-1", "doc-orphan-2"])
            assert res == {"doc-exists-1"}


class TestReconcileOpensearchOrphans:
    def test_dry_run_identifies_orphans_without_deletion(self, monkeypatch):
        mock_vector_store = MagicMock()
        mock_vector_store.get_all_indexed_doc_ids.side_effect = [
            (["doc-1", "doc-2", "doc-3"], None)
        ]
        monkeypatch.setattr("common.db_utils.get_vector_store", lambda: mock_vector_store)
        monkeypatch.setattr("digitize.db.manager.db_manager.get_existing_doc_ids", lambda ids: {"doc-1"})

        result = reconcile_opensearch_orphans(dry_run=True)
        assert result["status"] == "completed"
        assert result["dry_run"] is True
        assert result["scanned_docs"] == 3
        assert set(result["orphan_doc_ids"]) == {"doc-2", "doc-3"}
        assert result["orphan_count"] == 2
        assert result["deleted_chunks"] == 0
        mock_vector_store.remove_docs_from_index.assert_not_called()

    def test_reconcile_deletes_orphan_chunks(self, monkeypatch):
        mock_vector_store = MagicMock()
        mock_vector_store.get_all_indexed_doc_ids.side_effect = [
            (["doc-1", "doc-orphan"], None)
        ]
        mock_vector_store.remove_docs_from_index.return_value = 12
        monkeypatch.setattr("common.db_utils.get_vector_store", lambda: mock_vector_store)
        monkeypatch.setattr("digitize.db.manager.db_manager.get_existing_doc_ids", lambda ids: {"doc-1"})

        result = reconcile_opensearch_orphans(dry_run=False)
        assert result["status"] == "completed"
        assert result["dry_run"] is False
        assert result["scanned_docs"] == 2
        assert result["orphan_doc_ids"] == ["doc-orphan"]
        assert result["orphan_count"] == 1
        assert result["deleted_chunks"] == 12
        mock_vector_store.remove_docs_from_index.assert_called_once_with(["doc-orphan"])

    def test_concurrency_lock_prevents_simultaneous_runs(self, monkeypatch):
        # Lock is held
        _reconcile_lock.acquire(blocking=False)
        try:
            result = reconcile_opensearch_orphans(dry_run=False)
            assert result["status"] == "already_in_progress"
            assert result["message"] == "Reconciliation is already running"
            assert result["orphan_count"] == 0
        finally:
            _reconcile_lock.release()


class TestReconcileEndpoint:
    def test_endpoint_invokes_reconciliation(self, monkeypatch):
        fake_result = {
            "status": "completed",
            "dry_run": False,
            "scanned_docs": 10,
            "orphan_doc_ids": ["doc-orphan-1"],
            "orphan_count": 1,
            "deleted_chunks": 5,
            "message": None,
        }
        monkeypatch.setattr(
            "digitize.utils.recovery.reconcile_opensearch_orphans",
            lambda dry_run, batch_size=500: fake_result,
        )

        client = TestClient(app)
        resp = client.post("/v1/reconcile-opensearch")
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "completed"
        assert data["dry_run"] is False
        assert data["scanned_docs"] == 10
        assert data["orphan_doc_ids"] == ["doc-orphan-1"]
        assert data["orphan_count"] == 1
        assert data["deleted_chunks"] == 5
