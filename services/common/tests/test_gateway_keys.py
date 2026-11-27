"""The model gateway key and tokenize endpoint reach the embedding, reranker and tokenize calls."""
from unittest.mock import MagicMock

import pytest
from pydantic import SecretStr

import common.misc_utils as misc_utils
from common import emb_utils, llm_utils, reranker_utils
from common.settings import settings


@pytest.fixture
def session(monkeypatch):
    mock = MagicMock()
    mock.post.return_value.json.return_value = {"tokens": [1, 2], "data": [{"embedding": [0.1]}]}
    monkeypatch.setattr(misc_utils, "SESSION", mock)
    return mock


def _use(monkeypatch, model_config, key="", tokenize_endpoint=""):
    monkeypatch.setattr(model_config, "api_key", SecretStr(key))
    monkeypatch.setattr(model_config, "tokenize_endpoint", tokenize_endpoint)


class TestTokenize:
    def test_defaults_to_the_endpoint_and_always_sends_the_key(self, session, monkeypatch):
        _use(monkeypatch, settings.llm)

        llm_utils.tokenize_with_llm("x", "http://model:8000", "sk-llm")

        assert session.post.call_args.args[0] == "http://model:8000/tokenize"
        assert session.post.call_args.kwargs["headers"]["Authorization"] == "Bearer sk-llm"

    def test_the_key_is_required(self):
        with pytest.raises(TypeError):
            llm_utils.tokenize_with_llm("x", "http://model:8000")

    def test_llm_uses_its_tokenize_endpoint_and_key(self, session, monkeypatch):
        _use(monkeypatch, settings.llm, "sk-llm", "http://gw:4000/tokenize/llm-id")

        llm_utils.tokenize_with_llm("x", "http://model:8000", settings.llm.api_key.get_secret_value())

        assert session.post.call_args.args[0] == "http://gw:4000/tokenize/llm-id"
        assert session.post.call_args.kwargs["headers"]["Authorization"] == "Bearer sk-llm"

    def test_embedding_uses_its_own_tokenize_endpoint_and_key(self, session, monkeypatch):
        _use(monkeypatch, settings.llm, "sk-llm", "http://gw:4000/tokenize/llm-id")
        _use(monkeypatch, settings.embedding, "sk-emb", "http://gw:4000/tokenize/emb-id")

        llm_utils.tokenize_with_llm(
            "x", "http://model:8000", settings.embedding.api_key.get_secret_value(), model_config=settings.embedding
        )

        assert session.post.call_args.args[0] == "http://gw:4000/tokenize/emb-id"
        assert session.post.call_args.kwargs["headers"]["Authorization"] == "Bearer sk-emb"

    def test_embedding_query_validation_tokenizes_with_the_embedding_key(self, session, monkeypatch):
        from common.validation_utils import validate_query_length

        _use(monkeypatch, settings.llm, "sk-llm", "http://gw:4000/tokenize/llm-id")
        _use(monkeypatch, settings.embedding, "sk-emb", "http://gw:4000/tokenize/emb-id")

        validate_query_length("q", "http://model:8000", 100)

        assert session.post.call_args.args[0] == "http://gw:4000/tokenize/emb-id"
        assert session.post.call_args.kwargs["headers"]["Authorization"] == "Bearer sk-emb"


def test_embeddings_send_the_embedding_key(session, monkeypatch):
    _use(monkeypatch, settings.embedding, "sk-emb")

    emb_utils.Embedding("m", "http://gw:4000", 512).embed_query("q")

    assert session.post.call_args.kwargs["headers"]["Authorization"] == "Bearer sk-emb"


def test_embeddings_send_no_authorization_without_a_key(session, monkeypatch):
    _use(monkeypatch, settings.embedding)

    emb_utils.Embedding("m", "http://gw:4000", 512).embed_query("q")

    assert "Authorization" not in session.post.call_args.kwargs["headers"]


def test_model_info_sends_the_key(session):
    session.get.return_value.json.return_value = {}

    llm_utils.query_litellm_model_info("http://gw:4000", "sk-llm")

    assert session.get.call_args.kwargs["headers"]["Authorization"] == "Bearer sk-llm"


def test_key_info_looks_up_the_callers_key(session):
    session.get.return_value.json.return_value = {"info": {"metadata": {"tags": ["id"]}}}

    got = llm_utils.query_gateway_key_info("http://gw:4000", "sk-user")

    assert got == {"info": {"metadata": {"tags": ["id"]}}}
    assert session.get.call_args.args[0] == "http://gw:4000/key/info"
    assert session.get.call_args.kwargs["headers"]["Authorization"] == "Bearer sk-user"


def test_key_info_raises_when_the_gateway_errors(session):
    session.get.return_value.raise_for_status.side_effect = RuntimeError("401")

    with pytest.raises(RuntimeError):
        llm_utils.query_gateway_key_info("http://gw:4000", "sk-bad")


def test_reranker_client_uses_the_reranker_key(monkeypatch):
    _use(monkeypatch, settings.reranker, "sk-rr")
    client = MagicMock()
    monkeypatch.setattr(reranker_utils, "ClientV2", client)

    reranker_utils.rerank_documents("q", [], "m", "http://gw:4000")

    assert client.call_args.kwargs["api_key"] == "sk-rr"
