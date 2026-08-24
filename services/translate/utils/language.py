"""
Shared language validation helpers for the translate service.

Both the sync endpoint (api/v1/translate.py) and the async jobs endpoint
(api/v1/jobs.py) need identical language normalisation and validation logic.
"""

from typing import Optional

from translate.settings import settings
from translate.utils.errors import _raise_invalid_language, _raise_same_language


def normalise_language(value: Optional[str]) -> str:
    """Lowercase + strip; treat empty/None as 'auto'."""
    if not value or not value.strip():
        return "auto"
    return value.strip().lower()


def validate_languages(
    source_language: str,
    target_language: str,
) -> tuple[str, str]:
    """
    Validate and normalise both language parameters.

    Returns ``(normalised_source, normalised_target)`` on success.
    Raises ``HTTPException`` on any validation failure.

    Rules:
    - ``target`` must be an explicit supported language name (not ``"auto"``).
    - ``source`` may be ``"auto"`` (triggers lingua / LLM detection) or an
      explicit supported language name.
    - When both are explicit they must differ.
    """
    supported = settings.translate.supported_languages_list
    supported_display = ", ".join(s.capitalize() for s in sorted(supported))

    norm_source = normalise_language(source_language)
    norm_target = normalise_language(target_language)

    # target must not be "auto"
    if norm_target == "auto":
        _raise_invalid_language(
            f"'auto' is not valid for target_language. "
            f"Please specify an explicit target language. Supported: {supported_display}."
        )

    # target must be in allowlist
    if norm_target not in supported:
        _raise_invalid_language(
            f"'{target_language}' is not a supported language. "
            f"Supported: {supported_display}."
        )

    # source (if explicit) must be in allowlist
    if norm_source != "auto" and norm_source not in supported:
        _raise_invalid_language(
            f"'{source_language}' is not a supported language. "
            f"Supported: {supported_display}. "
            f"Use 'auto' for source_language to auto-detect."
        )

    # explicit source must differ from target
    if norm_source != "auto" and norm_source == norm_target:
        _raise_same_language(
            f"source_language and target_language must differ "
            f"(both are '{norm_source}')."
        )

    return norm_source, norm_target

