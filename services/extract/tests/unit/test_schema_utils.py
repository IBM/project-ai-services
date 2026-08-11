"""
Unit tests for schema_utils: normalization, validation, budget checks.
"""

import copy
import json

import pytest

from extract.utils.schema import (
    SchemaValidationError,
    check_extraction_budget,
    check_schema_share_in_context,
    compute_reserved_output,
    infer_schema_from_examples,
    normalize_schema,
    validate_examples,
    validate_json_schema_structure,
)


# ---------------------------------------------------------------------------
# normalize_schema
# ---------------------------------------------------------------------------

class TestNormalizeSchema:
    def test_flat_required_true_lifted_to_array(self):
        schema = {
            "type": "object",
            "properties": {
                "name": {"type": "string", "required": True},
                "age": {"type": "integer", "required": True},
                "notes": {"type": "string"},
            },
        }
        result = normalize_schema(schema)
        assert set(result["required"]) == {"name", "age"}
        assert "required" not in result["properties"]["name"]
        assert "required" not in result["properties"]["age"]
        assert "required" not in result["properties"]["notes"]

    def test_required_false_stripped_not_added(self):
        schema = {
            "type": "object",
            "properties": {"field": {"type": "string", "required": False}},
        }
        result = normalize_schema(schema)
        assert "required" not in result
        assert "required" not in result["properties"]["field"]

    def test_existing_required_array_merged_no_duplicates(self):
        schema = {
            "type": "object",
            "properties": {
                "a": {"type": "string", "required": True},
                "b": {"type": "string"},
            },
            "required": ["b"],
        }
        result = normalize_schema(schema)
        assert "b" in result["required"]
        assert "a" in result["required"]
        assert result["required"].count("b") == 1

    def test_nested_object_properties_normalized(self):
        schema = {
            "type": "object",
            "properties": {
                "address": {
                    "type": "object",
                    "properties": {
                        "street": {"type": "string", "required": True},
                        "city": {"type": "string"},
                    },
                }
            },
        }
        result = normalize_schema(schema)
        nested = result["properties"]["address"]
        assert nested["required"] == ["street"]
        assert "required" not in nested["properties"]["street"]

    def test_array_items_schema_normalized(self):
        schema = {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "properties": {
                            "description": {"type": "string", "required": True},
                            "qty": {"type": "number"},
                        },
                    },
                }
            },
        }
        result = normalize_schema(schema)
        items_schema = result["properties"]["items"]["items"]
        assert items_schema["required"] == ["description"]
        assert "required" not in items_schema["properties"]["description"]

    def test_anyof_schemas_normalized(self):
        schema = {
            "type": "object",
            "anyOf": [
                {
                    "type": "object",
                    "properties": {
                        "code": {"type": "string", "required": True},
                    },
                },
                {"type": "object", "properties": {"other": {"type": "string"}}},
            ],
        }
        result = normalize_schema(schema)
        assert result["anyOf"][0]["required"] == ["code"]
        assert "required" not in result["anyOf"][0]["properties"]["code"]

    def test_defs_normalized(self):
        schema = {
            "type": "object",
            "$defs": {
                "LineItem": {
                    "type": "object",
                    "properties": {
                        "sku": {"type": "string", "required": True},
                    },
                }
            },
            "properties": {},
        }
        result = normalize_schema(schema)
        assert result["$defs"]["LineItem"]["required"] == ["sku"]

    def test_original_schema_not_mutated(self):
        original = {
            "type": "object",
            "properties": {"name": {"type": "string", "required": True}},
        }
        before = copy.deepcopy(original)
        normalize_schema(original)
        assert original == before

    def test_no_properties_key_returns_schema_unchanged(self):
        schema = {"type": "object"}
        result = normalize_schema(schema)
        assert result == {"type": "object"}

    def test_conflict_existing_required_preserves_order(self):
        schema = {
            "type": "object",
            "properties": {
                "z": {"type": "string", "required": True},
                "a": {"type": "string", "required": True},
            },
            "required": ["existing"],
        }
        result = normalize_schema(schema)
        # "existing" must come first (original order preserved)
        assert result["required"][0] == "existing"
        assert set(result["required"]) == {"existing", "z", "a"}


# ---------------------------------------------------------------------------
# validate_json_schema_structure
# ---------------------------------------------------------------------------

class TestValidateJsonSchemaStructure:
    def test_valid_object_schema_passes(self):
        validate_json_schema_structure({"type": "object", "properties": {}})

    def test_root_array_type_raises(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            validate_json_schema_structure({"type": "array", "items": {"type": "string"}})
        assert exc_info.value.code == "INVALID_SCHEMA"

    def test_root_type_missing_raises(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            validate_json_schema_structure({"properties": {"x": {"type": "string"}}})
        assert exc_info.value.code == "INVALID_SCHEMA"

    def test_invalid_meta_schema_raises(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            validate_json_schema_structure({"type": "object", "properties": {"x": "not-a-schema"}})
        assert exc_info.value.code == "INVALID_SCHEMA"


# ---------------------------------------------------------------------------
# validate_examples
# ---------------------------------------------------------------------------

class TestValidateExamples:
    def _schema(self):
        return {
            "type": "object",
            "properties": {
                "name": {"type": "string"},
                "amount": {"type": "number"},
            },
            "required": ["name"],
        }

    def test_valid_example_passes(self):
        validate_examples(
            [{"text": "...", "output": {"name": "Alice", "amount": 42.0}}],
            self._schema(),
        )

    def test_none_examples_passes(self):
        validate_examples(None, self._schema())

    def test_empty_examples_passes(self):
        validate_examples([], self._schema())

    def test_missing_required_field_raises(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            validate_examples(
                [{"text": "...", "output": {"amount": 10.0}}],  # "name" missing
                self._schema(),
            )
        assert exc_info.value.code == "INVALID_EXAMPLE"
        assert exc_info.value.details["example_index"] == 0

    def test_wrong_type_raises(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            validate_examples(
                [{"text": "...", "output": {"name": 99, "amount": 10.0}}],  # name should be str
                self._schema(),
            )
        assert exc_info.value.code == "INVALID_EXAMPLE"

    def test_second_example_fails_reports_correct_index(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            validate_examples(
                [
                    {"text": "...", "output": {"name": "Alice"}},
                    {"text": "...", "output": {"amount": 5.0}},  # missing name
                ],
                self._schema(),
            )
        assert exc_info.value.details["example_index"] == 1



# ---------------------------------------------------------------------------
# infer_schema_from_examples
# ---------------------------------------------------------------------------

class TestInferSchemaFromExamples:
    """Tests for the schema-inference path (no explicit json_schema supplied)."""

    # ------------------------------------------------------------------
    # Basic happy paths
    # ------------------------------------------------------------------

    def test_single_example_flat_types(self):
        examples = [{"text": "t", "output": {"name": "Alice", "age": 30, "score": 9.5, "active": True}}]
        schema = infer_schema_from_examples(examples)

        assert schema["type"] == "object"
        props = schema["properties"]
        assert props["name"] == {"type": "string"}
        assert props["age"] == {"type": "integer"}
        assert props["score"] == {"type": "number"}
        assert props["active"] == {"type": "boolean"}

    def test_single_example_all_fields_required(self):
        """Every field from a single example must appear in required."""
        examples = [{"text": "t", "output": {"a": "x", "b": 1}}]
        schema = infer_schema_from_examples(examples)
        assert set(schema["required"]) == {"a", "b"}

    def test_field_present_in_all_examples_is_required(self):
        examples = [
            {"text": "t1", "output": {"name": "Alice", "opt": "x"}},
            {"text": "t2", "output": {"name": "Bob"}},
        ]
        schema = infer_schema_from_examples(examples)
        assert "name" in schema["required"]
        assert "opt" not in schema["required"]

    def test_field_absent_from_all_examples_has_no_required(self):
        """If every example omits a field entirely, it never appears at all."""
        examples = [
            {"text": "t1", "output": {"a": 1}},
            {"text": "t2", "output": {"b": 2}},
        ]
        schema = infer_schema_from_examples(examples)
        # Neither field is present in all examples.
        assert "required" not in schema or set(schema.get("required", [])) == set()

    def test_null_value_infers_null_type(self):
        examples = [{"text": "t", "output": {"field": None}}]
        schema = infer_schema_from_examples(examples)
        assert schema["properties"]["field"] == {"type": "null"}

    def test_list_value_infers_array_type(self):
        examples = [{"text": "t", "output": {"tags": ["a", "b"]}}]
        schema = infer_schema_from_examples(examples)
        assert schema["properties"]["tags"] == {'type': 'array', 'items': {'type': 'string'}}

    # ------------------------------------------------------------------
    # Nested dict (recursive)
    # ------------------------------------------------------------------

    def test_nested_dict_becomes_object_sub_schema(self):
        examples = [
            {"text": "t", "output": {"address": {"street": "Main St", "zip": "12345"}}}
        ]
        schema = infer_schema_from_examples(examples)
        addr = schema["properties"]["address"]
        assert addr["type"] == "object"
        assert addr["properties"]["street"] == {"type": "string"}
        assert addr["properties"]["zip"] == {"type": "string"}

    def test_nested_dict_merged_across_examples(self):
        """Same nested field seen in two examples with the same types merges cleanly."""
        examples = [
            {"text": "t1", "output": {"meta": {"a": "x"}}},
            {"text": "t2", "output": {"meta": {"b": 1}}},
        ]
        schema = infer_schema_from_examples(examples)
        meta = schema["properties"]["meta"]
        assert meta["type"] == "object"
        assert meta["properties"]["a"] == {"type": "string"}
        assert meta["properties"]["b"] == {"type": "integer"}

    def test_deeply_nested_dict(self):
        examples = [{"text": "t", "output": {"a": {"b": {"c": 42}}}}]
        schema = infer_schema_from_examples(examples)
        assert schema["properties"]["a"]["properties"]["b"]["properties"]["c"] == {"type": "integer"}

    # ------------------------------------------------------------------
    # bool vs int disambiguation
    # ------------------------------------------------------------------

    def test_bool_not_confused_with_integer(self):
        """Python bool is a subclass of int; must be typed as boolean, not integer."""
        examples = [{"text": "t", "output": {"flag": True}}]
        schema = infer_schema_from_examples(examples)
        assert schema["properties"]["flag"] == {"type": "boolean"}

    # ------------------------------------------------------------------
    # Type-conflict detection
    # ------------------------------------------------------------------

    def test_conflicting_types_raises(self):
        examples = [
            {"text": "t1", "output": {"amount": 100}},
            {"text": "t2", "output": {"amount": "one hundred"}},
        ]
        with pytest.raises(SchemaValidationError) as exc_info:
            infer_schema_from_examples(examples)
        err = exc_info.value
        assert err.code == "SCHEMA_INFERENCE_CONFLICT"
        assert err.status == 400
        assert "amount" in err.message

    def test_conflicting_nested_types_raises(self):
        examples = [
            {"text": "t1", "output": {"meta": {"val": 1}}},
            {"text": "t2", "output": {"meta": {"val": "one"}}},
        ]
        with pytest.raises(SchemaValidationError) as exc_info:
            infer_schema_from_examples(examples)
        assert exc_info.value.code == "SCHEMA_INFERENCE_CONFLICT"
        assert "meta.val" in exc_info.value.message

    def test_conflicting_top_level_vs_nested_type_raises(self):
        """A string in one example and an object in another must raise a conflict."""
        examples = [
            {"text": "t1", "output": {"field": "plain"}},
            {"text": "t2", "output": {"field": {"nested": 1}}},
        ]
        with pytest.raises(SchemaValidationError) as exc_info:
            infer_schema_from_examples(examples)
        assert exc_info.value.code == "SCHEMA_INFERENCE_CONFLICT"

    # ------------------------------------------------------------------
    # Error cases
    # ------------------------------------------------------------------

    def test_no_examples_raises(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            infer_schema_from_examples([])
        assert exc_info.value.code == "INFERENCE_NO_EXAMPLES"
        assert exc_info.value.status == 400

    def test_non_dict_output_raises(self):
        with pytest.raises(SchemaValidationError) as exc_info:
            infer_schema_from_examples([{"text": "t", "output": ["not", "a", "dict"]}])
        assert exc_info.value.code == "INFERENCE_INVALID_EXAMPLE"
        assert exc_info.value.status == 400

    def test_non_dict_output_reports_correct_index(self):
        examples = [
            {"text": "t1", "output": {"ok": 1}},
            {"text": "t2", "output": "not an object"},
        ]
        with pytest.raises(SchemaValidationError) as exc_info:
            infer_schema_from_examples(examples)
        assert "examples[1]" in exc_info.value.message

    # ------------------------------------------------------------------
    # Empty output dict
    # ------------------------------------------------------------------

    def test_empty_output_dict_raises_error(self):
        examples = [{"text": "t", "output": {}}]
        with pytest.raises(SchemaValidationError) as exc_info:
            infer_schema_from_examples(examples)
        assert exc_info.value.code == "INFERENCE_EMPTY_EXAMPLE"

    # ------------------------------------------------------------------
    # Schema structure is valid draft 2020-12
    # ------------------------------------------------------------------

    def test_inferred_schema_passes_structural_validation(self):
        """The schema produced by inference must itself pass validate_json_schema_structure."""
        examples = [
            {"text": "t1", "output": {"name": "Alice", "age": 30}},
            {"text": "t2", "output": {"name": "Bob", "age": 25, "city": "Berlin"}},
        ]
        schema = infer_schema_from_examples(examples)
        # Should not raise.
        validate_json_schema_structure(schema)
        assert schema["required"] == ["name", "age"]


# ---------------------------------------------------------------------------
# compute_reserved_output
# ---------------------------------------------------------------------------

class TestComputeReservedOutput:
    def test_normal_factor(self, monkeypatch):
        monkeypatch.setattr("extract.utils.schema.settings.extract.output_token_factor", 2.0)
        monkeypatch.setattr("extract.utils.schema.settings.extract.min_output_tokens", 512)
        monkeypatch.setattr("extract.utils.schema.settings.extract.max_output_tokens", 4096)
        assert compute_reserved_output(500) == 1000   # 500 * 2.0

    def test_floor_applied(self, monkeypatch):
        monkeypatch.setattr("extract.utils.schema.settings.extract.output_token_factor", 2.0)
        monkeypatch.setattr("extract.utils.schema.settings.extract.min_output_tokens", 512)
        monkeypatch.setattr("extract.utils.schema.settings.extract.max_output_tokens", 4096)
        assert compute_reserved_output(10) == 512   # 10 * 2.0 = 20 < 512

    def test_ceiling_applied(self, monkeypatch):
        monkeypatch.setattr("extract.utils.schema.settings.extract.output_token_factor", 2.0)
        monkeypatch.setattr("extract.utils.schema.settings.extract.min_output_tokens", 512)
        monkeypatch.setattr("extract.utils.schema.settings.extract.max_output_tokens", 4096)
        assert compute_reserved_output(5000) == 4096  # 5000 * 2.0 = 10000 > 4096


# ---------------------------------------------------------------------------
# check_registration_budget
# ---------------------------------------------------------------------------

class TestCheckRegistrationBudget:
    def _patch_settings(self, monkeypatch):
        monkeypatch.setattr("extract.utils.schema.settings.extract.context_schema_share", 0.5)
        monkeypatch.setattr("extract.utils.schema.prompt_overhead_tokens", 150)

    def test_within_budget_passes(self, monkeypatch):
        self._patch_settings(monkeypatch)
        # budget = 0.5 * 32768 = 16384; total = 100 + 50 + 20 + 150 = 320 <= 16384
        check_schema_share_in_context(100, 50, 20, 32768)

    def test_exceeds_budget_raises(self, monkeypatch):
        self._patch_settings(monkeypatch)
        # budget = 0.5 * 100 = 50; total = 40 + 30 + 0 + 150 = 220 > 50
        with pytest.raises(SchemaValidationError) as exc_info:
            check_schema_share_in_context(40, 30, 0, 100)
        assert exc_info.value.code == "SCHEMA_BUDGET_EXCEEDED"
        assert exc_info.value.status == 400
        assert "fixed_tokens" in exc_info.value.details

    def test_exactly_at_budget_passes(self, monkeypatch):
        self._patch_settings(monkeypatch)
        # budget = 0.5 * 32768 = 16384; use exactly 16384
        check_schema_share_in_context(16234, 0, 0, 32768)  # 16234 + 150 = 16384


# ---------------------------------------------------------------------------
# check_extraction_budget
# ---------------------------------------------------------------------------

class TestCheckExtractionBudget:
    def _patch_settings(self, monkeypatch):
        monkeypatch.setattr("extract.utils.schema.settings.extract.output_token_factor", 2.0)
        monkeypatch.setattr("extract.utils.schema.settings.extract.min_output_tokens", 512)
        monkeypatch.setattr("extract.utils.schema.settings.extract.max_output_tokens", 4096)
        monkeypatch.setattr("extract.utils.schema.prompt_overhead_tokens", 150)

    def test_within_budget_returns_reserved_output(self, monkeypatch):
        self._patch_settings(monkeypatch)
        # reserved = clamp(100*2, 512, 4096) = 512
        # total = 1000 + 100 + 50 + 0 + 150 + 512 = 1812 <= 32768
        reserved = check_extraction_budget(1000, 100, 50, 0, 32768)
        assert reserved == 512

    def test_exceeds_budget_raises(self, monkeypatch):
        self._patch_settings(monkeypatch)
        # total = 30000 + 1000 + 500 + 100 + 150 + 2000 = 33750 > 32768
        with pytest.raises(SchemaValidationError) as exc_info:
            check_extraction_budget(30000, 1000, 500, 100, 32768)
        assert exc_info.value.code == "CONTEXT_LIMIT_EXCEEDED"
        assert exc_info.value.status == 413
        assert "excess_tokens" in exc_info.value.details
        assert exc_info.value.details["input_tokens"] == 30000

# Made with Bob
