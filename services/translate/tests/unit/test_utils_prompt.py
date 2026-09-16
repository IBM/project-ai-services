"""
Unit tests for translate/utils/prompt.py — language resolution and message building.
"""

import pytest
from unittest.mock import patch, MagicMock

from translate.utils.prompt import (
    _detect_majority_vote,
    build_messages,
    iso_code_to_language_name,
    resolve_source_language,
)


@pytest.mark.unit
class TestIsoCodeToLanguageName:
    def test_english(self):
        assert iso_code_to_language_name("EN") == "English"

    def test_german(self):
        assert iso_code_to_language_name("DE") == "German"

    def test_french(self):
        assert iso_code_to_language_name("FR") == "French"

    def test_italian(self):
        assert iso_code_to_language_name("IT") == "Italian"

    def test_lowercase_accepted(self):
        assert iso_code_to_language_name("en") == "English"

    def test_unknown_returns_none(self):
        assert iso_code_to_language_name("ZZ") is None

    def test_empty_returns_none(self):
        # Empty string uppercase is still ""
        assert iso_code_to_language_name("") is None


@pytest.mark.unit
class TestDetectMajorityVote:
    def test_empty_text_returns_none(self):
        result = _detect_majority_vote("", min_confidence=0.9)
        assert result is None

    def test_majority_with_all_three_same(self):
        with patch("translate.utils.prompt.detect_language", return_value="DE"):
            result = _detect_majority_vote("Das ist ein langer deutschsprachiger Text " * 40, min_confidence=0.9)
        assert result == "DE"

    def test_majority_two_out_of_three(self):
        # Positions: start=DE, middle=DE, end=EN → DE wins (2/3)
        side_effects = ["DE", "DE", "EN"]
        with patch("translate.utils.prompt.detect_language", side_effect=side_effects):
            result = _detect_majority_vote("x" * 2000, min_confidence=0.9)
        assert result == "DE"

    def test_no_majority_returns_none(self):
        # All three positions return different codes (or None for some)
        side_effects = ["DE", "EN", "FR"]
        with patch("translate.utils.prompt.detect_language", side_effect=side_effects):
            result = _detect_majority_vote("x" * 2000, min_confidence=0.9)
        assert result is None

    def test_all_none_returns_none(self):
        with patch("translate.utils.prompt.detect_language", return_value=None):
            result = _detect_majority_vote("x" * 2000, min_confidence=0.9)
        assert result is None

    def test_short_text_single_sample(self):
        # Text shorter than 3 × 500 chars — samples may overlap but shouldn't crash
        with patch("translate.utils.prompt.detect_language", return_value="EN") as mock_detect:
            result = _detect_majority_vote("Short text.", min_confidence=0.9)
        assert mock_detect.call_count == 3  # always three samples


@pytest.mark.unit
class TestResolveSourceLanguage:
    def _settings(self):
        mock = MagicMock()
        mock.common.language.language_detection_min_confidence = 0.9
        return mock

    # ---- Explicit language supplied ----

    def test_explicit_german(self):
        name, code = resolve_source_language(
            text="Guten Tag",
            requested_source="german",
            settings=self._settings(),
            async_path=False,
        )
        assert name == "German"
        assert code == "DE"

    def test_explicit_english(self):
        name, code = resolve_source_language(
            text="Hello",
            requested_source="english",
            settings=self._settings(),
            async_path=False,
        )
        assert name == "English"
        assert code == "EN"

    def test_explicit_case_insensitive(self):
        name, code = resolve_source_language(
            text="Guten Tag",
            requested_source="GERMAN",
            settings=self._settings(),
            async_path=False,
        )
        assert name == "German"

    def test_explicit_unknown_language_falls_back_to_none(self):
        name, code = resolve_source_language(
            text="xyz",
            requested_source="klingon",
            settings=self._settings(),
            async_path=False,
        )
        assert name is None
        assert code is None

    # ---- Auto-detect sync path ----

    def test_auto_detect_sync_with_high_confidence(self):
        with patch("translate.utils.prompt.detect_language", return_value="DE"):
            name, code = resolve_source_language(
                text="Das ist ein Text",
                requested_source="auto",
                settings=self._settings(),
                async_path=False,
            )
        assert name == "German"
        assert code == "DE"

    def test_auto_detect_sync_low_confidence_returns_none(self):
        with patch("translate.utils.prompt.detect_language", return_value=None):
            name, code = resolve_source_language(
                text="...",
                requested_source="auto",
                settings=self._settings(),
                async_path=False,
            )
        assert name is None
        assert code is None

    def test_auto_detect_sync_unknown_iso_returns_none(self):
        # detect_language returns a code not in _ISO_TO_NAME
        with patch("translate.utils.prompt.detect_language", return_value="ZZ"):
            name, code = resolve_source_language(
                text="...",
                requested_source="auto",
                settings=self._settings(),
                async_path=False,
            )
        assert name is None

    # ---- Async (majority-vote) path ----

    def test_auto_detect_async_with_majority(self):
        with patch("translate.utils.prompt._detect_majority_vote", return_value="EN"):
            name, code = resolve_source_language(
                text="Hello world " * 300,
                requested_source="auto",
                settings=self._settings(),
                async_path=True,
            )
        assert name == "English"
        assert code == "EN"

    def test_auto_detect_async_no_majority(self):
        with patch("translate.utils.prompt._detect_majority_vote", return_value=None):
            name, code = resolve_source_language(
                text="x" * 2000,
                requested_source="auto",
                settings=self._settings(),
                async_path=True,
            )
        assert name is None
        assert code is None

    def test_empty_requested_source_treated_as_auto(self):
        with patch("translate.utils.prompt.detect_language", return_value="EN"):
            name, code = resolve_source_language(
                text="Hello",
                requested_source="",
                settings=self._settings(),
                async_path=False,
            )
        assert name == "English"


@pytest.mark.unit
class TestBuildMessages:
    def test_explicit_source_uses_explicit_template(self):
        messages = build_messages(
            text="Guten Tag",
            resolved_source_language="German",
            target_language="English",
        )
        assert len(messages) == 2
        assert messages[0]["role"] == "system"
        assert messages[1]["role"] == "user"
        assert "German" in messages[1]["content"]
        assert "English" in messages[1]["content"]
        assert "Guten Tag" in messages[1]["content"]

    def test_none_source_uses_auto_detect_template(self):
        messages = build_messages(
            text="Hello",
            resolved_source_language=None,
            target_language="German",
        )
        assert "Detect the language" in messages[1]["content"]
        assert "German" in messages[1]["content"]
        assert "Hello" in messages[1]["content"]

    def test_system_prompt_is_present(self):
        messages = build_messages(
            text="text",
            resolved_source_language="English",
            target_language="German",
        )
        assert "professional translator" in messages[0]["content"].lower()

    def test_both_templates_include_text(self):
        text = "unique_marker_text_xyz"
        for src in ("German", None):
            messages = build_messages(text=text, resolved_source_language=src, target_language="English")
            assert text in messages[1]["content"]

# Made with Bob
