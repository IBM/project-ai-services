import pytest
from unittest.mock import Mock, patch

from summarize.summ_utils import (
    MAX_ALLOWED_INPUT_TOKENS,
    MAX_INPUT_WORDS,
    SummarizeException,
    build_messages,
    build_success_response,
    compute_target_and_max_tokens,
    trim_to_last_sentence,
    validate_input_and_get_available_tokens,
    validate_summary_length,
    validate_summary_level,
    word_count,
)
from summarize.settings import settings


@pytest.mark.unit
class TestWordCount:
    def test_word_count_returns_number_of_words(self):
        assert word_count("one two three") == 3

    def test_word_count_ignores_extra_whitespace(self):
        assert word_count("  one   two\nthree  ") == 3

    def test_word_count_empty_string_returns_zero(self):
        assert word_count("") == 0


@pytest.mark.unit
class TestTrimToLastSentence:
    def test_trim_to_last_sentence_keeps_complete_sentence(self):
        text = "This is complete. Another sentence!"
        assert trim_to_last_sentence(text) == text

    def test_trim_to_last_sentence_removes_incomplete_trailing_text(self):
        text = "This is complete. This clause is incomplete"
        assert trim_to_last_sentence(text) == "This is complete."

    def test_trim_to_last_sentence_returns_stripped_text_when_no_punctuation(self):
        assert trim_to_last_sentence("  no closing punctuation here  ") == "no closing punctuation here"


@pytest.mark.unit
class TestBuildSuccessResponse:
    def test_build_success_response_contains_expected_structure(self):
        response = build_success_response(
            summary="This is a short summary.",
            original_length=100,
            input_type="text",
            model="test-model",
            processing_time_ms=123,
            input_tokens=50,
            output_tokens=20,
        )

        assert response["data"]["summary"] == "This is a short summary."
        assert response["data"]["original_length"] == 100
        assert response["data"]["summary_length"] == 5
        assert response["meta"]["model"] == "test-model"
        assert response["meta"]["processing_time_ms"] == 123
        assert response["meta"]["input_type"] == "text"
        assert response["usage"]["input_tokens"] == 50
        assert response["usage"]["output_tokens"] == 20
        assert response["usage"]["total_tokens"] == 70


@pytest.mark.unit
class TestBuildMessages:
    def test_build_messages_with_length_uses_length_prompt(self, summarize_sample_text):
        messages = build_messages(
            text=summarize_sample_text,
            target_words=120,
            min_words=100,
            max_words=140,
            has_length_spec=True,
        )

        assert len(messages) == 2
        assert messages[0]["role"] == "system"
        assert settings.summarize.summarize_system_prompt in messages[0]["content"]
        assert messages[1]["role"] == "user"
        assert "TARGET LENGTH: 120 words" in messages[1]["content"]
        assert "under 100 words is considered incomplete" in messages[1]["content"]
        assert summarize_sample_text in messages[1]["content"]

    def test_build_messages_without_length_uses_default_prompt(self, summarize_sample_text):
        messages = build_messages(
            text=summarize_sample_text,
            target_words=None,
            min_words=None,
            max_words=None,
            has_length_spec=False,
        )

        assert len(messages) == 2
        assert messages[1]["role"] == "user"
        assert "Create a thorough and comprehensive summary" in messages[1]["content"]
        assert "TARGET LENGTH" not in messages[1]["content"]
        assert summarize_sample_text in messages[1]["content"]


@pytest.mark.unit
class TestValidateSummaryLength:
    def test_validate_summary_length_accepts_integer_string(self):
        assert validate_summary_length("25") == 25

    def test_validate_summary_length_accepts_integer(self):
        assert validate_summary_length(30) == 30

    def test_validate_summary_length_none_returns_none(self):
        assert validate_summary_length(None) is None

    def test_validate_summary_length_non_integer_raises_error(self):
        with pytest.raises(SummarizeException) as exc:
            validate_summary_length("abc")

        assert exc.value.code == 400
        assert exc.value.status == "INVALID_PARAMETER"
        assert exc.value.message == "Length must be an integer"

    @pytest.mark.parametrize("value", [-1, MAX_INPUT_WORDS + 1])
    def test_validate_summary_length_out_of_bounds_raises_error(self, value):
        with pytest.raises(SummarizeException) as exc:
            validate_summary_length(value)

        assert exc.value.code == 400
        assert exc.value.status == "INVALID_PARAMETER"
        assert exc.value.message == "Length is out of bounds"

    def test_validate_summary_length_zero_returns_none_per_current_implementation(self):
        assert validate_summary_length(0) is None


@pytest.mark.unit
class TestValidateSummaryLevel:
    @pytest.mark.parametrize("level", ["brief", "standard", "detailed"])
    def test_validate_summary_level_accepts_valid_levels(self, level):
        assert validate_summary_level(level) == level

    def test_validate_summary_level_none_returns_none(self):
        assert validate_summary_level(None) is None

    def test_validate_summary_level_invalid_value_raises_error(self):
        with pytest.raises(SummarizeException) as exc:
            validate_summary_level("invalid")

        assert exc.value.code == 400
        assert exc.value.status == "INVALID_PARAMETER"
        assert "level must be one of" in exc.value.message


@pytest.mark.unit
class TestValidateInputAndGetAvailableTokens:
    def test_returns_available_output_tokens_for_valid_input(self):
        input_tokens = 100
        input_word_count = 80

        available_tokens = validate_input_and_get_available_tokens(
            input_tokens=input_tokens,
            input_word_count=input_word_count,
            summary_level=None,
            summary_length=None,
        )

        expected = (
            settings.common.llm.granite_3_3_8b_instruct_context_length
            - input_tokens
            - settings.summarize.summarization_prompt_token_count
        )
        assert available_tokens == expected

    def test_summary_length_greater_than_input_word_count_raises_error(self):
        with pytest.raises(SummarizeException) as exc:
            validate_input_and_get_available_tokens(
                input_tokens=100,
                input_word_count=20,
                summary_level=None,
                summary_length=25,
            )

        assert exc.value.code == 400
        assert exc.value.status == "INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH"

    def test_input_tokens_over_limit_raises_context_limit_exceeded(self):
        with pytest.raises(SummarizeException) as exc:
            validate_input_and_get_available_tokens(
                input_tokens=MAX_ALLOWED_INPUT_TOKENS + 1,
                input_word_count=5000,
                summary_level=None,
                summary_length=None,
            )

        assert exc.value.code == 413
        assert exc.value.status == "CONTEXT_LIMIT_EXCEEDED"

    def test_level_soft_limit_logs_warning_but_does_not_fail(self):
        input_tokens = MAX_ALLOWED_INPUT_TOKENS
        input_word_count = int(input_tokens * settings.common.llm.token_to_word_ratio_en)

        with patch("summarize.summ_utils.logger") as mock_logger:
            available_tokens = validate_input_and_get_available_tokens(
                input_tokens=input_tokens,
                input_word_count=input_word_count,
                summary_level="detailed",
                summary_length=None,
            )

        assert available_tokens > 0
        mock_logger.warning.assert_called_once()


@pytest.mark.unit
class TestComputeTargetAndMaxTokens:
    @pytest.mark.parametrize("level", ["brief", "standard", "detailed"])
    def test_level_mode_returns_expected_bounds(self, level):
        target_words, min_words, max_words, max_tokens = compute_target_and_max_tokens(
            input_tokens=200,
            available_output_tokens=500,
            summary_level=level,
            summary_length=None,
        )

        assert target_words is not None
        assert min_words is not None
        assert max_words is not None
        assert min_words <= target_words <= max_words
        assert max_tokens <= 500
        assert max_tokens > 0

    def test_level_mode_caps_target_to_available_space(self):
        target_words, min_words, max_words, max_tokens = compute_target_and_max_tokens(
            input_tokens=5000,
            available_output_tokens=50,
            summary_level="detailed",
            summary_length=None,
        )

        max_possible_words = int(50 * settings.common.llm.token_to_word_ratio_en)
        assert target_words == max_possible_words
        assert max_words <= max_possible_words
        assert max_tokens <= 50
        assert min_words <= target_words

    def test_length_mode_uses_requested_length_when_space_allows(self):
        target_words, min_words, max_words, max_tokens = compute_target_and_max_tokens(
            input_tokens=200,
            available_output_tokens=300,
            summary_level=None,
            summary_length=100,
        )

        assert target_words == 100
        assert min_words == 85
        assert max_words == 114
        assert max_tokens <= 300
        assert max_tokens > 0

    def test_length_mode_caps_values_when_space_is_limited(self):
        available_output_tokens = 40
        target_words, min_words, max_words, max_tokens = compute_target_and_max_tokens(
            input_tokens=200,
            available_output_tokens=available_output_tokens,
            summary_level=None,
            summary_length=100,
        )

        max_possible_words = int(available_output_tokens * settings.common.llm.token_to_word_ratio_en)
        assert target_words == max_possible_words
        assert max_words <= max_possible_words
        assert max_tokens <= available_output_tokens

    def test_automatic_mode_returns_none_for_word_targets(self):
        target_words, min_words, max_words, max_tokens = compute_target_and_max_tokens(
            input_tokens=200,
            available_output_tokens=80,
            summary_level=None,
            summary_length=None,
        )

        assert target_words is None
        assert min_words is None
        assert max_words is None
        assert 0 < max_tokens <= 80

# Made with Bob
