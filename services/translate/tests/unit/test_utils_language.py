"""
Unit tests for translate/utils/language.py — language normalisation and validation.
"""

import pytest
from fastapi import HTTPException
from unittest.mock import patch

from translate.utils.language import normalise_language, validate_languages


@pytest.mark.unit
class TestNormaliseLanguage:
    def test_none_returns_auto(self):
        assert normalise_language(None) == "auto"

    def test_empty_string_returns_auto(self):
        assert normalise_language("") == "auto"

    def test_whitespace_only_returns_auto(self):
        assert normalise_language("   ") == "auto"

    def test_strips_and_lowercases(self):
        assert normalise_language("  German  ") == "german"

    def test_already_lowercase_unchanged(self):
        assert normalise_language("english") == "english"

    def test_uppercase_lowercased(self):
        assert normalise_language("GERMAN") == "german"

    def test_mixed_case(self):
        assert normalise_language("English") == "english"


@pytest.mark.unit
class TestValidateLanguages:
    # ---- target must not be 'auto' ----

    def test_target_auto_raises_400(self):
        with pytest.raises(HTTPException) as exc_info:
            validate_languages("german", "auto")
        assert exc_info.value.status_code == 400
        assert exc_info.value.detail["error"]["code"] == "INVALID_LANGUAGE"

    # ---- target must be in allowlist ----

    def test_unsupported_target_raises_400(self):
        with pytest.raises(HTTPException) as exc_info:
            validate_languages("auto", "klingon")
        assert exc_info.value.status_code == 400
        assert "klingon" in exc_info.value.detail["error"]["message"].lower()

    # ---- source (if explicit) must be in allowlist ----

    def test_unsupported_explicit_source_raises_400(self):
        with pytest.raises(HTTPException) as exc_info:
            validate_languages("klingon", "english")
        assert exc_info.value.status_code == 400

    # ---- explicit source == target must differ ----

    def test_same_explicit_source_and_target_raises_400(self):
        with pytest.raises(HTTPException) as exc_info:
            validate_languages("english", "english")
        assert exc_info.value.status_code == 400
        assert exc_info.value.detail["error"]["code"] == "SAME_LANGUAGE"

    def test_german_to_german_raises(self):
        with pytest.raises(HTTPException):
            validate_languages("german", "german")

    # ---- happy path ----

    def test_valid_auto_to_english(self):
        src, tgt = validate_languages("auto", "english")
        assert src == "auto"
        assert tgt == "english"

    def test_valid_german_to_english(self):
        src, tgt = validate_languages("german", "english")
        assert src == "german"
        assert tgt == "english"

    def test_valid_english_to_german(self):
        src, tgt = validate_languages("english", "german")
        assert src == "english"
        assert tgt == "german"

    def test_case_insensitive_source(self):
        src, tgt = validate_languages("German", "English")
        assert src == "german"
        assert tgt == "english"

    def test_case_insensitive_target(self):
        src, tgt = validate_languages("auto", "English")
        assert tgt == "english"

    def test_auto_source_with_whitespace(self):
        src, tgt = validate_languages("  auto  ", "english")
        assert src == "auto"

    def test_empty_source_treated_as_auto(self):
        src, tgt = validate_languages("", "english")
        assert src == "auto"

    def test_none_source_treated_as_auto(self):
        src, tgt = validate_languages(None, "english")
        assert src == "auto"

    def test_returns_normalised_strings(self):
        src, tgt = validate_languages("  GERMAN  ", "  ENGLISH  ")
        assert src == "german"
        assert tgt == "english"

# Made with Bob
