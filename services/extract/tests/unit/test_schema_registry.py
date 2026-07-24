"""
Unit tests for the Schema Registry HTTP endpoints.

All external boundaries (database, tokenizer) are mocked so that tests run
without a real PostgreSQL instance or vLLM endpoint.
"""

from unittest.mock import AsyncMock, MagicMock, Mock, patch
from datetime import datetime, timezone

import pytest

from extract.schema_utils import SchemaValidationError


# ---- helpers -----------------------------------------------------------

def _mock_schema_row(
    schema_id="schema-001",
    name="invoice-extraction",
    description="Test schema",
    json_schema=None,
    examples=None,
    custom_prompt=None,
    schema_tokens=50,
    examples_tokens=30,
    custom_prompt_tokens=0,
    created_at=None,
):
    row = Mock()
    row.schema_id = schema_id
    row.name = name
    row.description = description
    row.json_schema = json_schema or {
        "type": "object",
        "properties": {"name": {"type": "string"}},
        "required": ["name"],
    }
    row.examples = examples or []
    row.custom_prompt = custom_prompt
    row.schema_tokens = schema_tokens
    row.examples_tokens = examples_tokens
    row.custom_prompt_tokens = custom_prompt_tokens
    row.created_at = created_at or datetime(2026, 7, 7, 9, 30, 0, tzinfo=timezone.utc)
    return row


_VALID_SCHEMA_BODY = {
    "name": "invoice-extraction",
    "description": "Extracts invoice fields",
    "json_schema": {
        "type": "object",
        "properties": {
            "invoice_number": {"type": "string"},
            "total_amount": {"type": "number"},
        },
        "required": ["invoice_number"],
    },
}

_VALID_SCHEMA_BODY_WITH_EXAMPLE = {
    **_VALID_SCHEMA_BODY,
    "examples": [
        {
            "text": "Invoice #001 Total: 100",
            "output": {"invoice_number": "001", "total_amount": 100.0},
        }
    ],
}


# =========================================================================
# Health & root endpoints
# =========================================================================

@pytest.mark.unit
class TestHealthAndRoot:
    def test_health_returns_ok(self, extract_test_client):
        resp = extract_test_client.get("/health")
        assert resp.status_code == 200
        assert resp.json() == {"status": "ok"}

    def test_root_returns_swagger_html(self, extract_test_client):
        resp = extract_test_client.get("/")
        assert resp.status_code == 200
        assert "Swagger UI" in resp.text


# =========================================================================
# POST /v1/schemas
# =========================================================================

@pytest.mark.unit
class TestRegisterSchema:
    def _patch_all_passing(self):
        """Context-manager stack for the happy path."""
        return [
            patch("extract.app.normalize_schema", return_value=_VALID_SCHEMA_BODY["json_schema"]),
            patch("extract.app.validate_json_schema_structure"),
            patch("extract.app.validate_examples"),
            patch("extract.app.db_repo.schema_name_exists", return_value=False),
            patch(
                "extract.app.compute_token_counts",
                return_value=(50, 30, 0),
            ),
            patch("extract.app.check_schema_share_in_context"),
            patch(
                "extract.app.db_repo.create_schema",
                return_value=_mock_schema_row(),
            ),
        ]

    def test_valid_schema_returns_201(self, extract_test_client, monkeypatch):
        monkeypatch.setattr(
            "extract.app.asyncio.to_thread",
            AsyncMock(return_value=(50, 30, 0)),
        )
        patches = self._patch_all_passing()
        ctx = [p.__enter__() for p in patches]
        try:
            resp = extract_test_client.post("/v1/schemas", json=_VALID_SCHEMA_BODY)
        finally:
            for p in reversed(patches):
                p.__exit__(None, None, None)

        assert resp.status_code == 201
        body = resp.json()
        assert "schema_id" in body
        assert body["name"] == "invoice-extraction"

    def test_duplicate_name_returns_409(self, extract_test_client):
        with patch("extract.app.normalize_schema", return_value=_VALID_SCHEMA_BODY["json_schema"]), \
             patch("extract.app.validate_json_schema_structure"), \
             patch("extract.app.validate_examples"), \
             patch("extract.app.db_repo.schema_name_exists", return_value=True):
            resp = extract_test_client.post("/v1/schemas", json=_VALID_SCHEMA_BODY)

        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "CONFLICT"

    def test_invalid_json_schema_returns_400(self, extract_test_client):
        with patch("extract.app.normalize_schema", return_value=_VALID_SCHEMA_BODY["json_schema"]), \
             patch(
                 "extract.app.validate_json_schema_structure",
                 side_effect=SchemaValidationError("INVALID_SCHEMA", "Root must be type:object", 400),
             ):
            resp = extract_test_client.post(
                "/v1/schemas",
                json={
                    "name": "bad-schema",
                    "json_schema": {"type": "array", "items": {"type": "string"}},
                },
            )

        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "INVALID_SCHEMA"

    def test_invalid_example_returns_400(self, extract_test_client):
        with patch("extract.app.normalize_schema", return_value=_VALID_SCHEMA_BODY["json_schema"]), \
             patch("extract.app.validate_json_schema_structure"), \
             patch(
                 "extract.app.validate_examples",
                 side_effect=SchemaValidationError(
                     "INVALID_EXAMPLE",
                     "examples[0].output does not validate",
                     400,
                     {"example_index": 0},
                 ),
             ):
            resp = extract_test_client.post("/v1/schemas", json=_VALID_SCHEMA_BODY_WITH_EXAMPLE)

        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "INVALID_EXAMPLE"

    def test_budget_exceeded_returns_400(self, extract_test_client, monkeypatch):
        monkeypatch.setattr(
            "extract.app.asyncio.to_thread",
            AsyncMock(return_value=(9000, 8000, 500)),
        )
        with patch("extract.app.normalize_schema", return_value=_VALID_SCHEMA_BODY["json_schema"]), \
             patch("extract.app.validate_json_schema_structure"), \
             patch("extract.app.validate_examples"), \
             patch("extract.app.db_repo.schema_name_exists", return_value=False), \
             patch(
                 "extract.app.check_schema_share_in_context",
                 side_effect=SchemaValidationError(
                     "SCHEMA_BUDGET_EXCEEDED",
                     "Schema overhead exceeds budget",
                     400,
                     {"fixed_tokens": 20000, "budget_tokens": 16384},
                 ),
             ):
            resp = extract_test_client.post("/v1/schemas", json=_VALID_SCHEMA_BODY)

        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "SCHEMA_BUDGET_EXCEEDED"

    def test_missing_name_returns_422(self, extract_test_client):
        """FastAPI validation (not schema_utils) catches missing required fields."""
        resp = extract_test_client.post(
            "/v1/schemas",
            json={"json_schema": {"type": "object", "properties": {}}},
        )
        assert resp.status_code == 422

    def test_name_with_invalid_chars_returns_422(self, extract_test_client):
        resp = extract_test_client.post(
            "/v1/schemas",
            json={"name": "has space!", "json_schema": {"type": "object", "properties": {}}},
        )
        assert resp.status_code == 422



# =========================================================================
# GET /v1/schemas
# =========================================================================

@pytest.mark.unit
class TestListSchemas:
    def test_returns_200_with_pagination(self, extract_test_client):
        rows = [_mock_schema_row(schema_id=f"s-{i}", name=f"schema-{i}") for i in range(3)]
        with patch("extract.app.db_repo.list_schemas", return_value=(rows, 3)):
            resp = extract_test_client.get("/v1/schemas")

        assert resp.status_code == 200
        body = resp.json()
        assert body["pagination"]["total"] == 3
        assert len(body["data"]) == 3

    def test_empty_registry_returns_200(self, extract_test_client):
        with patch("extract.app.db_repo.list_schemas", return_value=([], 0)):
            resp = extract_test_client.get("/v1/schemas")
        assert resp.status_code == 200
        assert resp.json()["pagination"]["total"] == 0

    def test_name_filter_passed_to_db(self, extract_test_client):
        with patch("extract.app.db_repo.list_schemas", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/schemas?name=invoice")
        mock_list.assert_called_once_with(name_filter="invoice", limit=20, offset=0)

    def test_limit_and_offset_passed_to_db(self, extract_test_client):
        with patch("extract.app.db_repo.list_schemas", return_value=([], 0)) as mock_list:
            extract_test_client.get("/v1/schemas?limit=5&offset=10")
        mock_list.assert_called_once_with(name_filter=None, limit=5, offset=10)

    def test_limit_out_of_range_returns_422(self, extract_test_client):
        resp = extract_test_client.get("/v1/schemas?limit=0")
        assert resp.status_code == 422

    def test_schema_body_not_included_in_list(self, extract_test_client):
        row = _mock_schema_row()
        with patch("extract.app.db_repo.list_schemas", return_value=([row], 1)):
            resp = extract_test_client.get("/v1/schemas")
        item = resp.json()["data"][0]
        assert "json_schema" not in item   # body excluded from list endpoint


# =========================================================================
# GET /v1/schemas/{schema_id}
# =========================================================================

@pytest.mark.unit
class TestGetSchema:
    def test_existing_schema_returns_200_with_body(self, extract_test_client):
        row = _mock_schema_row()
        with patch("extract.app.db_repo.get_schema_by_id", return_value=row):
            resp = extract_test_client.get(f"/v1/schemas/{row.schema_id}")

        assert resp.status_code == 200
        body = resp.json()
        assert body["schema_id"] == row.schema_id
        assert "json_schema" in body
        assert "examples" in body

    def test_unknown_id_returns_404(self, extract_test_client):
        with patch("extract.app.db_repo.get_schema_by_id", return_value=None):
            resp = extract_test_client.get("/v1/schemas/nonexistent")

        assert resp.status_code == 404
        assert resp.json()["error"]["code"] == "SCHEMA_NOT_FOUND"

    def test_normalized_schema_returned(self, extract_test_client):
        """The GET endpoint returns whatever is stored in the DB (already normalized)."""
        stored_schema = {
            "type": "object",
            "properties": {"name": {"type": "string"}},
            "required": ["name"],
        }
        row = _mock_schema_row(json_schema=stored_schema)
        with patch("extract.app.db_repo.get_schema_by_id", return_value=row):
            resp = extract_test_client.get(f"/v1/schemas/{row.schema_id}")
        assert resp.json()["json_schema"] == stored_schema


# =========================================================================
# DELETE /v1/schemas/{schema_id}
# =========================================================================

@pytest.mark.unit
class TestDeleteSchema:
    def test_unreferenced_schema_returns_204(self, extract_test_client):
        row = _mock_schema_row()
        with patch("extract.app.db_repo.get_schema_by_id", return_value=row), \
             patch("extract.app.db_repo.get_referencing_job_ids", return_value=[]), \
             patch("extract.app.db_repo.delete_schema", return_value=True):
            resp = extract_test_client.delete(f"/v1/schemas/{row.schema_id}")

        assert resp.status_code == 204

    def test_unknown_schema_returns_404(self, extract_test_client):
        with patch("extract.app.db_repo.get_schema_by_id", return_value=None):
            resp = extract_test_client.delete("/v1/schemas/unknown")

        assert resp.status_code == 404

    def test_schema_with_jobs_returns_409(self, extract_test_client):
        row = _mock_schema_row()
        with patch("extract.app.db_repo.get_schema_by_id", return_value=row), \
             patch("extract.app.db_repo.get_referencing_job_ids", return_value=["job-1", "job-2"]):
            resp = extract_test_client.delete(f"/v1/schemas/{row.schema_id}")

        assert resp.status_code == 409
        body = resp.json()
        assert body["error"]["code"] == "SCHEMA_IN_USE"
        assert "referencing_job_ids" in body["error"]["details"]
        assert "job-1" in body["error"]["details"]["referencing_job_ids"]

    def test_concurrent_job_creation_returns_409(self, extract_test_client):
        """If FK RESTRICT fires in DB, endpoint returns 409."""
        from sqlalchemy.exc import IntegrityError as SAIntegrityError

        row = _mock_schema_row()
        with patch("extract.app.db_repo.get_schema_by_id", return_value=row), \
             patch("extract.app.db_repo.get_referencing_job_ids", return_value=[]), \
             patch("extract.app.db_repo.delete_schema", side_effect=SAIntegrityError(None, None, None)), \
             patch("extract.app.db_repo.get_referencing_job_ids", return_value=["job-1"]):
            resp = extract_test_client.delete(f"/v1/schemas/{row.schema_id}")

        assert resp.status_code == 409


# =========================================================================
# DELETE /v1/schemas (bulk)
# =========================================================================

@pytest.mark.unit
class TestBulkDeleteSchemas:
    def test_confirm_true_no_jobs_returns_204(self, extract_test_client):
        with patch("extract.app.db_repo.any_schema_has_jobs", return_value=False), \
             patch("extract.app.db_repo.delete_all_schemas", return_value=True):
            resp = extract_test_client.delete("/v1/schemas?confirm=true")

        assert resp.status_code == 204

    def test_missing_confirm_returns_400(self, extract_test_client):
        resp = extract_test_client.delete("/v1/schemas")
        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "CONFIRMATION_REQUIRED"

    def test_confirm_false_returns_400(self, extract_test_client):
        resp = extract_test_client.delete("/v1/schemas?confirm=false")
        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "CONFIRMATION_REQUIRED"

    def test_jobs_exist_returns_409(self, extract_test_client):
        with patch("extract.app.db_repo.any_schema_has_jobs", return_value=True):
            resp = extract_test_client.delete("/v1/schemas?confirm=true")

        assert resp.status_code == 409
        assert resp.json()["error"]["code"] == "SCHEMAS_IN_USE"

    def test_confirm_wrong_value_returns_400(self, extract_test_client):
        resp = extract_test_client.delete("/v1/schemas?confirm=yes")
        assert resp.status_code == 400
        assert resp.json()["error"]["code"] == "CONFIRMATION_REQUIRED"


# =========================================================================
# Request-ID middleware
# =========================================================================

@pytest.mark.unit
class TestRequestIdMiddleware:
    def test_provided_request_id_echoed(self, extract_test_client):
        resp = extract_test_client.get("/health", headers={"X-Request-ID": "test-123"})
        assert resp.status_code == 200
        assert resp.headers["X-Request-ID"] == "test-123"

    def test_missing_request_id_generated(self, extract_test_client):
        resp = extract_test_client.get("/health")
        assert "X-Request-ID" in resp.headers
        assert len(resp.headers["X-Request-ID"]) > 0

# Made with Bob
