"""
Schema utility functions for the Extract Information service.

Responsibilities:
  - Normalization: lift per-property ``"required": true`` into the parent
    object's ``required`` array, recursively for nested objects, array items,
    and combiners (anyOf / oneOf / allOf / $defs).
  - Validation: check that the submitted schema is a valid JSON Schema
    draft 2020-12 with root ``type: object``, and that every example output
    validates against the normalized schema.
  - Size guard: enforce MAX_SCHEMA_BYTES before any normalization work.
  - Budget check: verify that schema fixed-overhead token counts do not
    exceed CONTEXT_SCHEMA_SHARE × MAX_MODEL_LEN at registration time.
  - Custom-prompt safety: block prompt-injection patterns (mirrors chatbot
    validation logic).
"""

import json
import re
import copy
from typing import Any, Dict, List, Optional, Tuple

import jsonschema
from jsonschema import Draft202012Validator

from common.misc_utils import get_logger
from extract.settings import settings

logger = get_logger("schema_utils")


# ---------------------------------------------------------------------------
# Public exception
# ---------------------------------------------------------------------------

class SchemaValidationError(Exception):
    """Raised when a submitted schema fails any validation check."""

    def __init__(self, code: str, message: str, status: int = 400, details: Optional[dict] = None):
        self.code = code
        self.message = message
        self.status = status
        self.details = details or {}
        super().__init__(message)





# ---------------------------------------------------------------------------
# Normalization — per-property "required": true → required array
# ---------------------------------------------------------------------------

def normalize_schema(schema: Dict[str, Any]) -> Dict[str, Any]:
    """
    Return a **deep copy** of *schema* with the non-standard per-property
    ``"required": true`` convention normalized into standard JSON Schema
    ``required`` arrays.

    Algorithm (applied recursively):

    1. For each ``schema`` node that is a JSON object schema
       (i.e. has ``"properties"``), collect the names of all properties
       that carry ``"required": true``.
    2. Strip ``"required": true`` (or ``"required": false``) from every
       property sub-schema.
    3. Merge collected names into the parent's ``"required"`` array:
       - If the parent already has a ``"required"`` array, union the two
         lists, preserving existing order, appending new names in iteration
         order.  Duplicates are silently dropped.
       - If the parent has no ``"required"`` array and collected names is
         non-empty, create one.
    4. Recurse into:
       - Each property sub-schema under ``"properties"``.
       - The ``"items"`` sub-schema (object schemas inside arrays).
       - Each entry in ``"anyOf"``, ``"oneOf"``, ``"allOf"``, ``"if"``,
         ``"then"``, ``"else"``.
       - Each definition in ``"$defs"`` / ``"definitions"``.
    """
    return _normalize_node(copy.deepcopy(schema))


def _normalize_node(node: Any) -> Any:
    """Recursively normalize a single schema node (in-place on a deep copy)."""
    if not isinstance(node, dict):
        return node

    # Step 1 & 2 — lift per-property "required": true from properties.
    if "properties" in node and isinstance(node["properties"], dict):
        collected: List[str] = []
        for prop_name, prop_schema in node["properties"].items():
            if not isinstance(prop_schema, dict):
                continue
            req_val = prop_schema.pop("required", None)
            if req_val is True:
                collected.append(prop_name)
            # "required": false is simply dropped; no addition to the array.

        # Step 3 — merge into parent required array.
        if collected:
            existing: List[str] = node.get("required", [])
            if not isinstance(existing, list):
                existing = []
            existing_set = set(existing)
            for name in collected:
                if name not in existing_set:
                    existing.append(name)
                    existing_set.add(name)
            node["required"] = existing

    # Step 4 — recurse into sub-schemas.

    # properties values
    for prop_schema in node.get("properties", {}).values():
        _normalize_node(prop_schema)

    # items (single schema form)
    if "items" in node and isinstance(node["items"], dict):
        _normalize_node(node["items"])

    # combiners
    for combiner_kw in ("anyOf", "oneOf", "allOf"):
        if combiner_kw in node and isinstance(node[combiner_kw], list):
            for sub in node[combiner_kw]:
                _normalize_node(sub)

    # if / then / else
    for keyword in ("if", "then", "else"):
        if keyword in node and isinstance(node[keyword], dict):
            _normalize_node(node[keyword])

    # $defs / definitions
    for defs_kw in ("$defs", "definitions"):
        if defs_kw in node and isinstance(node[defs_kw], dict):
            for def_schema in node[defs_kw].values():
                _normalize_node(def_schema)

    return node


# ---------------------------------------------------------------------------
# Draft 2020-12 structural validation
# ---------------------------------------------------------------------------

def validate_json_schema_structure(json_schema: Dict[str, Any]) -> None:
    """
    Validate that *json_schema* is a structurally valid JSON Schema
    draft 2020-12 **and** that the root is ``type: object``.

    Raises SchemaValidationError on any violation.
    """
    # Check_schema raises jsonschema.exceptions.SchemaError on invalid meta.
    try:
        Draft202012Validator.check_schema(json_schema)
    except jsonschema.exceptions.SchemaError as exc:
        raise SchemaValidationError(
            "INVALID_SCHEMA",
            f"The submitted json_schema is not a valid JSON Schema draft 2020-12: {exc.message}",
            status=400,
        ) from exc

    root_type = json_schema.get("type")
    if root_type != "object":
        raise SchemaValidationError(
            "INVALID_SCHEMA",
            f"The root of json_schema must be 'type: object'; got {root_type!r}.",
            status=400,
        )


# ---------------------------------------------------------------------------
# Example validation
# ---------------------------------------------------------------------------

def validate_examples(
    examples: Optional[List[Dict[str, Any]]],
    normalized_schema: Dict[str, Any],
) -> None:
    """
    Validate that every example's ``output`` conforms to *normalized_schema*.

    Raises SchemaValidationError identifying the first failing example by
    zero-based index.
    """
    if not examples:
        return

    validator = Draft202012Validator(normalized_schema)

    for idx, example in enumerate(examples):
        output = example.get("output", {})
        errors = list(validator.iter_errors(output))
        if errors:
            error_messages = "; ".join(e.message for e in errors[:5])
            raise SchemaValidationError(
                "INVALID_EXAMPLE",
                f"examples[{idx}].output does not validate against the schema: {error_messages}",
                status=400,
                details={"example_index": idx, "validation_errors": [e.message for e in errors]},
            )


# ---------------------------------------------------------------------------
# Token-count helpers
# ---------------------------------------------------------------------------

def _tokenize(text: str, llm_endpoint: str) -> int:
    """Return token count for *text* via the vLLM /tokenize API."""
    from common.llm_utils import tokenize_with_llm
    tokens = tokenize_with_llm(text, llm_endpoint)
    return len(tokens)


def compute_token_counts(
    normalized_schema: Dict[str, Any],
    examples: Optional[List[Dict[str, Any]]],
    custom_prompt: Optional[str],
    llm_endpoint: str,
) -> Tuple[int, int, int]:
    """
    Return *(schema_tokens, examples_tokens, custom_prompt_tokens)*.

    - *schema_tokens*  : tokens in ``json.dumps(normalized_schema)``
    - *examples_tokens*: tokens in the rendered few-shot block (all examples)
    - *custom_prompt_tokens*: tokens in *custom_prompt* (0 if absent)

    Token counts are computed once at schema-registration time.  Because
    schemas are immutable, these counts never go stale.
    """
    schema_str = json.dumps(normalized_schema, separators=(",", ":"), ensure_ascii=False)
    schema_tokens = _tokenize(schema_str, llm_endpoint)

    examples_tokens = 0
    if examples:
        # Render the few-shot block the same way the extraction prompt does.
        few_shot_parts: List[str] = []
        for ex in examples:
            few_shot_parts.append(
                f"Example text:\n{ex['text']}\n"
                f"Example JSON:\n{json.dumps(ex['output'], ensure_ascii=False)}"
            )
        few_shot_block = "\n\n".join(few_shot_parts)
        examples_tokens = _tokenize(few_shot_block, llm_endpoint)

    custom_prompt_tokens = 0
    if custom_prompt:
        custom_prompt_tokens = _tokenize(custom_prompt, llm_endpoint)

    return schema_tokens, examples_tokens, custom_prompt_tokens


# ---------------------------------------------------------------------------
# Registration budget check (Section 5.1.2 of proposal)
# ---------------------------------------------------------------------------

def check_schema_share_in_context(
    schema_tokens: int,
    examples_tokens: int,
    custom_prompt_tokens: int,
    max_model_len: int,
) -> None:
    """
    Ensure the schema's fixed prompt overhead does not exceed
    ``CONTEXT_SCHEMA_SHARE × MAX_MODEL_LEN``.

    The budget formula :

        schema_tokens + examples_tokens + PROMPT_OVERHEAD_TOKENS
            + custom_prompt_tokens
            <= CONTEXT_SCHEMA_SHARE × MAX_MODEL_LEN

    Additionally verify that the reserved output capacity is feasible:

        schema_tokens × OUTPUT_TOKEN_FACTOR ≤ MAX_OUTPUT_TOKENS

    (This is already guaranteed by the clamp in compute_reserved_output, but
    the explicit check gives a more informative error message.)

    Raises SchemaValidationError with code SCHEMA_BUDGET_EXCEEDED on failure.
    """
    overhead = settings.extract.prompt_overhead_tokens
    share = settings.extract.context_schema_share
    budget = int(share * max_model_len)

    fixed_tokens = schema_tokens + examples_tokens + custom_prompt_tokens + overhead

    if fixed_tokens > budget:
        raise SchemaValidationError(
            "SCHEMA_BUDGET_EXCEEDED",
            (
                f"Schema fixed overhead ({fixed_tokens} tokens) exceeds "
                f"{share * 100:.0f}% of MAX_MODEL_LEN={max_model_len} "
                f"(budget={budget} tokens).  Reduce the schema, shorten or "
                f"remove examples, or trim the custom_prompt."
            ),
            status=400,
            details={
                "schema_tokens": schema_tokens,
                "examples_tokens": examples_tokens,
                "custom_prompt_tokens": custom_prompt_tokens,
                "prompt_overhead_tokens": overhead,
                "fixed_tokens": fixed_tokens,
                "budget_tokens": budget,
                "max_model_len": max_model_len,
                "context_schema_share": share,
            },
        )


# ---------------------------------------------------------------------------
# Per-request reserved-output computation
# ---------------------------------------------------------------------------

def compute_reserved_output(schema_tokens: int) -> int:
    """
    Return the number of output tokens to reserve for the extraction result.

    Formula:
        reserved = clamp(
            schema_tokens × OUTPUT_TOKEN_FACTOR,
            MIN_OUTPUT_TOKENS,
            MAX_OUTPUT_TOKENS,
        )
    """
    raw = schema_tokens * settings.extract.output_token_factor
    return int(
        max(
            settings.extract.min_output_tokens,
            min(settings.extract.max_output_tokens, raw),
        )
    )


def check_extraction_budget(
    input_tokens: int,
    schema_tokens: int,
    examples_tokens: int,
    custom_prompt_tokens: int,
    max_model_len: int,
) -> int:
    """
    Run the hard context-window guard for a single extraction request.

    Returns the reserved_output token count (== max_tokens for the LLM call)
    if the budget is within limits.

    Raises SchemaValidationError with code CONTEXT_LIMIT_EXCEEDED and full
    diagnostics on failure.  The caller is responsible for converting this
    into the appropriate HTTP 413 response.
    """
    overhead = settings.extract.prompt_overhead_tokens
    reserved_output = compute_reserved_output(schema_tokens)

    total = (
        input_tokens
        + schema_tokens
        + examples_tokens
        + custom_prompt_tokens
        + overhead
        + reserved_output
    )

    if total > max_model_len:
        details = {
            "max_model_len": max_model_len,
            "input_tokens": input_tokens,
            "schema_tokens": schema_tokens,
            "examples_tokens": examples_tokens,
            "custom_prompt_tokens": custom_prompt_tokens,
            "prompt_overhead_tokens": overhead,
            "reserved_output_tokens": reserved_output,
            "total_required_tokens": total,
            "excess_tokens": total - max_model_len,
        }
        raise SchemaValidationError(
            "CONTEXT_LIMIT_EXCEEDED",
            (
                "Input does not fit in the model context window. "
                "Reduce input size or use the async job path with a smaller document."
            ),
            status=413,
            details=details,
        )

    return reserved_output

# Made with Bob
