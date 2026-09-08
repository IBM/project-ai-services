"""
Unit tests for translate/utils/llm.py — LLM helpers.
"""

import pytest
import httpx
from unittest.mock import AsyncMock, MagicMock, patch

from translate.utils.llm import (
    get_chunk_token_budget,
    get_llm_max_model_len,
    query_vllm_translate,
)


@pytest.mark.unit
class TestGetLlmMaxModelLen:
    def test_delegates_to_resolve_model_max_len(self):
        with patch("translate.utils.llm.resolve_model_max_len", return_value=32768) as mock_resolve:
            result = get_llm_max_model_len()
        assert result == 32768
        mock_resolve.assert_called_once()

    def test_passes_correct_args(self):
        with patch("translate.utils.llm.resolve_model_max_len", return_value=16384) as mock_resolve, \
             patch("translate.utils.llm.settings") as mock_settings:
            mock_settings.common.llm.endpoint = "http://vllm:8000"
            mock_settings.common.llm.model = "granite"
            mock_settings.common.llm.max_model_len = 4096
            mock_settings.common.llm.api_key = None
            get_llm_max_model_len()
        mock_resolve.assert_called_once_with("http://vllm:8000", "granite", 4096, None)


@pytest.mark.unit
class TestGetChunkTokenBudget:
    def test_budget_is_ratio_of_max_model_len(self):
        with patch("translate.utils.llm.get_llm_max_model_len", return_value=32768), \
             patch("translate.utils.llm.settings") as mock_settings:
            mock_settings.translate.chunk_budget_ratio = 0.40
            result = get_chunk_token_budget()
        assert result == int(32768 * 0.40)

    def test_result_is_integer(self):
        with patch("translate.utils.llm.get_llm_max_model_len", return_value=10000), \
             patch("translate.utils.llm.settings") as mock_settings:
            mock_settings.translate.chunk_budget_ratio = 0.40
            result = get_chunk_token_budget()
        assert isinstance(result, int)


@pytest.mark.unit
class TestQueryVllmTranslate:
    def _mock_response(self, content="Hallo Welt", prompt_tokens=10, completion_tokens=8):
        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = {
            "choices": [{"message": {"content": content}}],
            "usage": {
                "prompt_tokens": prompt_tokens,
                "completion_tokens": completion_tokens,
            },
        }
        return mock_resp

    @pytest.mark.asyncio
    async def test_successful_call_returns_translation(self):
        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=self._mock_response("Hallo Welt"))

        result = await query_vllm_translate(
            client=mock_client,
            llm_endpoint="http://vllm:8000",
            messages=[{"role": "system", "content": "..."}, {"role": "user", "content": "Hello"}],
            model="granite",
            max_tokens=200,
        )
        text, in_tok, out_tok = result
        assert text == "Hallo Welt"
        assert in_tok == 10
        assert out_tok == 8

    @pytest.mark.asyncio
    async def test_uses_correct_endpoint_path(self):
        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=self._mock_response())

        await query_vllm_translate(
            client=mock_client,
            llm_endpoint="http://vllm:8000",
            messages=[],
            model="m",
            max_tokens=100,
        )
        call_url = mock_client.post.call_args[0][0]
        assert call_url == "http://vllm:8000/v1/chat/completions"

    @pytest.mark.asyncio
    async def test_default_temperature_is_zero(self):
        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=self._mock_response())

        await query_vllm_translate(
            client=mock_client,
            llm_endpoint="http://vllm:8000",
            messages=[],
            model="m",
            max_tokens=100,
        )
        payload = mock_client.post.call_args[1]["json"]
        assert payload["temperature"] == 0.0

    @pytest.mark.asyncio
    async def test_custom_temperature_is_passed(self):
        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=self._mock_response())

        await query_vllm_translate(
            client=mock_client,
            llm_endpoint="http://vllm:8000",
            messages=[],
            model="m",
            max_tokens=100,
            temperature=0.7,
        )
        payload = mock_client.post.call_args[1]["json"]
        assert payload["temperature"] == 0.7

    @pytest.mark.asyncio
    async def test_raises_on_empty_choices(self):
        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = {"choices": [], "usage": {}}

        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)

        with pytest.raises(ValueError, match="empty 'choices'"):
            await query_vllm_translate(
                client=mock_client,
                llm_endpoint="http://vllm:8000",
                messages=[],
                model="m",
                max_tokens=100,
            )

    @pytest.mark.asyncio
    async def test_strips_whitespace_from_content(self):
        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=self._mock_response(content="  Hallo  "))

        text, _, _ = await query_vllm_translate(
            client=mock_client,
            llm_endpoint="http://vllm:8000",
            messages=[],
            model="m",
            max_tokens=100,
        )
        assert text == "Hallo"

    @pytest.mark.asyncio
    async def test_missing_usage_defaults_to_zero(self):
        mock_resp = MagicMock()
        mock_resp.raise_for_status = MagicMock()
        mock_resp.json.return_value = {
            "choices": [{"message": {"content": "result"}}],
        }
        mock_client = AsyncMock()
        mock_client.post = AsyncMock(return_value=mock_resp)

        _, in_tok, out_tok = await query_vllm_translate(
            client=mock_client,
            llm_endpoint="http://vllm:8000",
            messages=[],
            model="m",
            max_tokens=100,
        )
        assert in_tok == 0
        assert out_tok == 0

    @pytest.mark.asyncio
    async def test_http_status_error_propagates(self):
        mock_client = AsyncMock()
        mock_resp = MagicMock()
        mock_resp.raise_for_status.side_effect = httpx.HTTPStatusError(
            "500", request=MagicMock(), response=MagicMock()
        )
        mock_client.post = AsyncMock(return_value=mock_resp)

        with pytest.raises(httpx.HTTPStatusError):
            await query_vllm_translate(
                client=mock_client,
                llm_endpoint="http://vllm:8000",
                messages=[],
                model="m",
                max_tokens=100,
            )

# Made with Bob
