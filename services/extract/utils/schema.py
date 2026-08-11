"""
Schema utility functions for the Extract Information service.

Responsibilities:
  - Normalization: lift per-property ``"required": true`` into the parent
    object's ``required`` array, recursively for nested objects, array items,
    and combiners (anyOf / oneOf / allOf / $defs).
  - Validation: check that the submitted schema is a valid JSON Schema
    draft 2020-12 with root ``type: object``, and that every example output
    validates against the normalized schema.
  - Budget check: verify that schema fixed-overhead token counts do not
    exceed CONTEXT_SCHEMA_SHARE × MAX_MODEL_LEN at registration time.
"""

import json
import re
import copy
from datetime import timezone
from typing import Any, Dict, List, Optional, Set, Tuple

import jsonschema
from jsonschema import Draft202012Validator

from common.misc_utils import get_logger
from extract.settings import settings
from extract.utils.exceptions import ExtractException

logger = get_logger("schema_utils")

# ---------------------------------------------------------------------------
# Prompt overhead — dynamically computed at service startup
# ---------------------------------------------------------------------------

# Token count of the fixed system + user prompt scaffold (placeholders empty).
# Set to 0 here; overwritten by calculate_prompt_overhead_tokens() during
# the FastAPI lifespan startup once the LLM session is available.
prompt_overhead_tokens: int = 0


def calculate_prompt_overhead_tokens(llm_endpoint: str) -> None:
    """
    Tokenise the fixed prompt scaffold with all variable placeholders replaced
    by empty strings, and store the result in the module-level
    ``prompt_overhead_tokens``.

    Call this once during service startup after ``create_llm_session()`` and
    ``initialize_models()`` have completed.
    """
    global prompt_overhead_tokens
    system_scaffold = settings.extract.extraction_system_prompt.format(custom_prompt="")
    user_scaffold = settings.extract.extraction_user_prompt.format(
        normalized_json_schema="", few_shot_block="", input_text=""
    )
    scaffold_text = system_scaffold + "\n" + user_scaffold
    prompt_overhead_tokens = _tokenize(scaffold_text, llm_endpoint)
    logger.info(f"Computed prompt_overhead_tokens={prompt_overhead_tokens}")


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
# Schema inference from examples
# ---------------------------------------------------------------------------

def _infer_object_schema(value: dict) -> Dict[str, Any]:
    """
    Infer a ``type: object`` sub-schema from a single dict value.

    Every key present in *value* is added to ``required`` (the all-present
    default). When multiple examples are merged, ``_merge_type_nodes``
    intersects the ``required`` arrays so only universally-present keys
    remain required.
    """
    properties = {k: _python_type_to_json_schema(v) for k, v in value.items()}
    schema: Dict[str, Any] = {"type": "object", "properties": properties}
    if properties:
        schema["required"] = list(value.keys())
    return schema


def _infer_array_schema(value: list) -> Dict[str, Any]:
    """
    Infer a ``type: array`` schema from a list value.

    If the list is non-empty, ``items`` is derived by inferring a schema from
    each element and merging them (so ``[1, 2.5]`` yields
    ``items: {type: number}``).  Empty lists produce an unconstrained array.
    """
    if not value:
        return {"type": "array"}

    items_schema = _python_type_to_json_schema(value[0])
    for element in value[1:]:
        items_schema = _merge_type_nodes(
            items_schema, _python_type_to_json_schema(element), "[]"
        )
    return {"type": "array", "items": items_schema}


def _python_type_to_json_schema(value: Any) -> Dict[str, Any]:
    """Return the JSON Schema type node for a single Python *value*."""
    # bool must be checked before int because bool is a subclass of int.
    if isinstance(value, bool):
        return {"type": "boolean"}
    if isinstance(value, int):
        return {"type": "integer"}
    if isinstance(value, float):
        return {"type": "number"}
    if isinstance(value, str):
        return {"type": "string"}
    if isinstance(value, list):
        return _infer_array_schema(value)
    if isinstance(value, dict):
        return _infer_object_schema(value)
    if value is None:
        return {"type": "null"}
    # Fallback for unexpected types.
    return {}


def _merge_type_nodes(
    existing: Dict[str, Any],
    incoming: Dict[str, Any],
    field_path: str,
) -> Dict[str, Any]:
    """
    Merge two JSON Schema type nodes for the same field seen in two
    different examples.

    - Identical nodes are returned as-is.
    - Already-nullable list types (e.g. ``["integer", "null"]``) are
      unwrapped to their base type, merged normally, then re-wrapped
      with null if either side was nullable.
    - ``null`` + any concrete type produces a nullable type
      (e.g. ``{"type": ["string", "null"]}``).
    - ``integer`` + ``number`` widens to ``number``.
    - Two ``type: object`` nodes have their ``properties`` unioned and
      their ``required`` arrays intersected.
    - Two ``type: array`` nodes have their ``items`` merged recursively.
    - All other type mismatches raise SchemaValidationError.
    """
    if existing == incoming:
        return existing

    existing_type = existing.get("type")
    incoming_type = incoming.get("type")

    # --- already-nullable list type on either side → unwrap, merge base, re-wrap ---
    existing_is_list = isinstance(existing_type, list)
    incoming_is_list = isinstance(incoming_type, list)

    if existing_is_list or incoming_is_list:
        existing_has_null = existing_is_list and "null" in existing_type
        incoming_has_null = incoming_is_list and "null" in incoming_type

        if existing_is_list:
            existing_base = [t for t in existing_type if t != "null"]
            existing_base = existing_base[0] if len(existing_base) == 1 else existing_base
        else:
            existing_base = existing_type

        if incoming_is_list:
            incoming_base = [t for t in incoming_type if t != "null"]
            incoming_base = incoming_base[0] if len(incoming_base) == 1 else incoming_base
        else:
            incoming_base = incoming_type

        base_existing = dict(existing)
        base_existing["type"] = existing_base
        base_incoming = dict(incoming)
        base_incoming["type"] = incoming_base

        merged = _merge_type_nodes(base_existing, base_incoming, field_path)

        if existing_has_null or incoming_has_null:
            merged_type = merged.get("type")
            if isinstance(merged_type, list):
                if "null" not in merged_type:
                    merged["type"] = merged_type + ["null"]
            else:
                merged["type"] = [merged_type, "null"]

        return merged

    # --- null + any concrete type → nullable ---
    if existing_type == "null" or incoming_type == "null":
        non_null = incoming if existing_type == "null" else existing
        non_null_type = non_null.get("type")
        if non_null_type is None:
            return non_null
        result = dict(non_null)
        if isinstance(non_null_type, list):
            if "null" not in non_null_type:
                result["type"] = non_null_type + ["null"]
        else:
            result["type"] = [non_null_type, "null"]
        return result

    # --- integer + number → number ---
    if {existing_type, incoming_type} == {"integer", "number"}:
        return {"type": "number"}

    # --- object + object → union properties, intersect required ---
    if existing_type == "object" and incoming_type == "object":
        merged_props: Dict[str, Any] = dict(existing.get("properties", {}))
        for key, inc_prop in incoming.get("properties", {}).items():
            if key in merged_props:
                merged_props[key] = _merge_type_nodes(
                    merged_props[key], inc_prop, f"{field_path}.{key}"
                )
            else:
                merged_props[key] = inc_prop

        existing_req = set(existing.get("required", []))
        incoming_req = set(incoming.get("required", []))
        merged_required = sorted(existing_req & incoming_req)

        result = {"type": "object", "properties": merged_props}
        if merged_required:
            result["required"] = merged_required
        return result

    # --- array + array → merge items ---
    if existing_type == "array" and incoming_type == "array":
        existing_items = existing.get("items")
        incoming_items = incoming.get("items")
        if existing_items and incoming_items:
            merged_items = _merge_type_nodes(
                existing_items, incoming_items, f"{field_path}[]"
            )
            return {"type": "array", "items": merged_items}
        return {"type": "array", "items": existing_items or incoming_items}

    # --- irreconcilable conflict ---
    if existing_type != incoming_type:
        raise SchemaValidationError(
            "SCHEMA_INFERENCE_CONFLICT",
            (
                f"Cannot infer schema: field {field_path!r} has conflicting types "
                f"across examples ({existing_type!r} vs {incoming_type!r})."
            ),
            status=400,
        )

    return existing


def infer_schema_from_examples(examples: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    Infer a JSON Schema draft 2020-12 ``type: object`` from example outputs.

    *examples* is a list of raw example dicts (each having an ``output`` key).

    Algorithm:
    1. For each example's ``output`` dict, derive a type node per field
       recursively (nested dicts become nested ``type: object`` sub-schemas,
       non-empty lists infer ``items`` from their elements).
    2. Merge type nodes across examples: identical nodes pass through,
       ``integer`` + ``number`` widens to ``number``, ``null`` + any type
       produces a nullable type, and two ``object`` nodes have their
       ``properties`` unioned and ``required`` arrays intersected.
       Irreconcilable type conflicts raise SchemaValidationError.
    3. Fields present in **all** examples are added to ``required``.

    Raises SchemaValidationError if no examples are provided, an example
    output is not a non-empty dict, or a type conflict is detected.
    """
    if not examples:
        raise SchemaValidationError(
            "INFERENCE_NO_EXAMPLES",
            "Cannot infer schema: no examples provided. "
            "Supply at least one example or provide json_schema explicitly.",
            status=400,
        )

    field_schemas: Dict[str, Any] = {}
    field_counts: Dict[str, int] = {}
    total = len(examples)

    for idx, example in enumerate(examples):
        output = example.get("output", {})
        if not isinstance(output, dict):
            raise SchemaValidationError(
                "INFERENCE_INVALID_EXAMPLE",
                f"examples[{idx}].output must be an object for schema inference.",
                status=400,
            )
        if not output:
            raise SchemaValidationError(
                "INFERENCE_EMPTY_EXAMPLE",
                f"examples[{idx}].output must be a non-empty object for schema "
                f"inference.",
                status=400,
            )
        for key, value in output.items():
            incoming_node = _python_type_to_json_schema(value)
            if key in field_schemas:
                field_schemas[key] = _merge_type_nodes(
                    field_schemas[key], incoming_node, key
                )
            else:
                field_schemas[key] = incoming_node
            field_counts[key] = field_counts.get(key, 0) + 1

    required = [k for k, count in field_counts.items() if count == total]

    schema: Dict[str, Any] = {
        "type": "object",
        "properties": field_schemas,
    }
    if required:
        schema["required"] = required

    return schema


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
    overhead = prompt_overhead_tokens
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
    overhead = prompt_overhead_tokens
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
        raise ExtractException(413,
            "CONTEXT_LIMIT_EXCEEDED",
            (
                "Input does not fit in the model context window. "
                "Reduce input size or use the async job path with a smaller document."
            ),
            details=details,
        )

    return reserved_output


# ---------------------------------------------------------------------------
# Helper: format datetime for responses
# ---------------------------------------------------------------------------

def fmt_dt(dt) -> Optional[str]:
    if dt is None:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.isoformat().replace("+00:00", "Z")

# Made with Bob
