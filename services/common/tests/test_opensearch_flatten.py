"""Unit tests for the flattened-metadata index layout in common.opensearch."""

from unittest.mock import patch

import pytest

from common import opensearch
from common.opensearch import (
    SCHEMA_VERSION,
    OpensearchNotReadyError,
    OpensearchVectorStore,
)

FLAT_METADATA_FIELDS = [
    "filename", "doc_id", "type", "source", "language",
    "page_number", "chunk_index", "total_chunks", "created_at",
]


@pytest.fixture
def store():
    """OpensearchVectorStore with a mocked client (no network access)."""
    with patch.object(opensearch, "OpenSearch") as mock_os, \
         patch.object(OpensearchVectorStore, "_create_pipeline"):
        s = OpensearchVectorStore()
        s.client = mock_os.return_value
        s.client.indices.exists.return_value = True
        yield s


def _chunk(**overrides):
    chunk = {
        "page_content": "hello world",
        "filename": "a.pdf",
        "doc_id": "doc-1",
        "type": "text",
        "source": "Chapter 1",
        "language": "en",
        "page_number": 2,
        "chunk_index": 0,
        "total_chunks": 4,
        "created_at": "2025-01-01T00:00:00",
    }
    chunk.update(overrides)
    return chunk


def _hit(**fields):
    return {"_score": 0.9, "fields": {k: [v] for k, v in fields.items()}}


class TestIndexBody:
    def test_metadata_fields_are_top_level(self, store):
        props = store._index_body(8)["mappings"]["properties"]
        assert "metadata" not in props
        for field in FLAT_METADATA_FIELDS:
            assert field in props

    def test_field_types(self, store):
        props = store._index_body(8)["mappings"]["properties"]
        for field in ("filename", "doc_id", "type", "source", "language"):
            assert props[field] == {"type": "keyword"}
        for field in ("page_number", "chunk_index", "total_chunks"):
            assert props[field] == {"type": "integer"}
        assert props["created_at"] == {"type": "date"}

    def test_text_is_stored_for_stored_fields_retrieval(self, store):
        text = store._index_body(8)["mappings"]["properties"]["text"]
        assert text["type"] == "text"
        assert text["store"] is True

    def test_chunk_id_has_doc_values(self, store):
        chunk_id = store._index_body(8)["mappings"]["properties"]["chunk_id"]
        assert chunk_id == {"type": "long", "doc_values": True}

    def test_embedding_dimension(self, store):
        assert store._index_body(768)["mappings"]["properties"]["embedding"]["dimension"] == 768

    def test_schema_version_marker(self, store):
        # Catalog restore relies on _meta.schema_version to recognise the current layout.
        assert store._index_body(8)["mappings"]["_meta"] == {"schema_version": SCHEMA_VERSION}
        assert SCHEMA_VERSION == 2

    def test_source_not_disabled(self, store):
        # Backup/restore needs _source.
        assert "_source" not in store._index_body(8)["mappings"]


class TestSetupIndex:
    def test_creates_index_with_flat_body(self, store):
        store.client.indices.exists.return_value = False
        store._setup_index(16)
        store.client.indices.create.assert_called_once_with(
            index=store.index_name, body=store._index_body(16)
        )

    def test_skips_existing_index(self, store):
        store._setup_index(16)
        store.client.indices.create.assert_not_called()


class TestInsertChunks:
    def _insert(self, store, chunks, vectors=None):
        vectors = vectors if vectors is not None else [[0.1, 0.2]] * len(chunks)
        with patch.object(opensearch.helpers, "bulk", return_value=(len(chunks), [])) as bulk:
            assert store.insert_chunks(chunks, vectors=vectors) is True
        return [a for call in bulk.call_args_list for a in call.args[1]]

    def test_metadata_written_at_top_level(self, store):
        actions = self._insert(store, [_chunk()])
        source = actions[0]["_source"]
        assert "metadata" not in source
        for field in FLAT_METADATA_FIELDS:
            assert source[field] == _chunk()[field]
        assert source["text"] == "hello world"
        assert source["embedding"] == [0.1, 0.2]
        assert actions[0]["_index"] == store.index_name

    def test_optional_fields_omitted_when_missing(self, store):
        chunk = _chunk()
        for k in ("page_number", "chunk_index", "total_chunks", "created_at"):
            chunk.pop(k)
        source = self._insert(store, [chunk])[0]["_source"]
        for k in ("page_number", "chunk_index", "total_chunks", "created_at"):
            assert k not in source

    def test_doc_id_falls_back_to_filename(self, store):
        chunk = _chunk()
        chunk.pop("doc_id")
        source = self._insert(store, [chunk])[0]["_source"]
        assert source["doc_id"] == "a.pdf"

    def test_chunk_id_matches_document_id(self, store):
        action = self._insert(store, [_chunk()])[0]
        assert action["_id"] == str(action["_source"]["chunk_id"])


class TestSearch:
    def _search(self, store, hits=None, **kwargs):
        store.client.search.return_value = {
            "hits": {"total": {"value": len(hits or [])}, "hits": hits or []}
        }
        kwargs.setdefault("vector", [0.1, 0.2])
        results = store.search("query", **kwargs)
        return results, store.client.search.call_args.kwargs["body"]

    def test_requests_docvalue_and_stored_fields_not_source(self, store):
        _, body = self._search(store, mode="sparse")
        assert body["_source"] is False
        assert body["stored_fields"] == ["text"]
        assert set(body["docvalue_fields"]) == {"chunk_id", *FLAT_METADATA_FIELDS}

    @pytest.mark.parametrize("mode", ["dense", "sparse", "hybrid"])
    def test_language_filter_uses_top_level_field(self, store, mode):
        _, body = self._search(store, mode=mode, language="de")
        assert "metadata.language" not in str(body)
        assert {"term": {"language": "de"}} in _collect_term_filters(body["query"])

    def test_dense_without_language_uses_match_all(self, store):
        _, body = self._search(store, mode="dense", language=None)
        assert body["query"]["knn"]["embedding"]["filter"] == {"match_all": {}}

    def test_sparse_without_language_has_empty_filter(self, store):
        _, body = self._search(store, mode="sparse", language=None)
        assert body["query"]["bool"]["filter"] == []

    def test_defaults_to_hybrid(self, store):
        _, body = self._search(store)
        assert "hybrid" in body["query"]

    def test_invalid_mode_raises(self, store):
        with pytest.raises(ValueError):
            store.search("q", vector=[0.1], mode="bogus")

    def test_missing_index_raises_not_ready(self, store):
        store.client.indices.exists.return_value = False
        with pytest.raises(OpensearchNotReadyError):
            store.search("q", vector=[0.1])

    def test_no_vector_or_embedding_raises(self, store):
        with pytest.raises(ValueError):
            store.search("q")

    def test_results_built_from_fields(self, store):
        hit = _hit(
            chunk_id=123, text="hello", filename="a.pdf", doc_id="doc-1",
            type="text", source="Ch1", language="en", page_number=2,
            chunk_index=0, total_chunks=4, created_at="2025-01-01T00:00:00",
        )
        results, _ = self._search(store, hits=[hit], mode="dense")
        r = results[0]
        assert r["chunk_id"] == 123
        assert r["text"] == "hello"
        assert r["page_content"] == "hello"
        assert r["score"] == 0.9
        assert r["doc_id"] == "doc-1"
        assert r["page_number"] == 2
        assert r["metadata"] == {
            "filename": "a.pdf", "doc_id": "doc-1", "type": "text", "source": "Ch1",
            "language": "en", "page_number": 2, "chunk_index": 0, "total_chunks": 4,
            "created_at": "2025-01-01T00:00:00",
        }

    def test_missing_fields_are_dropped_from_metadata(self, store):
        hit = _hit(chunk_id=1, text="t", doc_id="d", filename="f")
        results, _ = self._search(store, hits=[hit], mode="dense")
        assert results[0]["metadata"] == {"doc_id": "d", "filename": "f"}
        assert "page_number" not in results[0]

    def test_hit_without_fields(self, store):
        results, _ = self._search(store, hits=[{"_score": 0.1}], mode="dense")
        assert results[0]["text"] is None
        assert results[0]["metadata"] == {}


class TestDelete:
    def test_remove_docs_queries_top_level_doc_id(self, store):
        store.client.delete_by_query.return_value = {"deleted": 3}
        assert store.remove_docs_from_index(["a", "b"]) == 3
        body = store.client.delete_by_query.call_args.kwargs["body"]
        assert body == {"query": {"terms": {"doc_id": ["a", "b"]}}}

    def test_remove_docs_missing_index(self, store):
        store.client.indices.exists.return_value = False
        assert store.remove_docs_from_index(["a"]) == 0
        store.client.delete_by_query.assert_not_called()

    def test_delete_document_queries_top_level_doc_id(self, store):
        store.client.delete_by_query.return_value = {"deleted": 2, "total": 2}
        assert store.delete_document_by_id(" doc-1 ") == 2
        body = store.client.delete_by_query.call_args.kwargs["body"]
        assert body == {"query": {"term": {"doc_id": "doc-1"}}}

    def test_delete_document_missing_index(self, store):
        store.client.indices.exists.return_value = False
        assert store.delete_document_by_id("doc-1") == 0
        store.client.delete_by_query.assert_not_called()


def _collect_term_filters(query):
    """Collect every {"term": {...}} dict nested anywhere in a query body."""
    found = []

    def walk(node):
        if isinstance(node, dict):
            if "term" in node:
                found.append({"term": node["term"]})
            for v in node.values():
                walk(v)
        elif isinstance(node, list):
            for v in node:
                walk(v)

    walk(query)
    return found
