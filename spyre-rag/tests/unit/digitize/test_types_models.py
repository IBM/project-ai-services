import json

import pytest
from pydantic import ValidationError

from digitize.document import DocumentMetadata, TimingInfo
from digitize.job import JobDocumentSummary, JobState, JobStats
from digitize.types import (
    DocStatus,
    DocumentContentResponse,
    DocumentDetailResponse,
    DocumentListItem,
    DocumentsListResponse,
    JobCreatedResponse,
    JobStatus,
    JobsListResponse,
    OperationType,
    OutputFormat,
    PaginationInfo,
)


@pytest.mark.unit
class TestEnums:
    def test_output_format_values(self):
        assert OutputFormat.TEXT.value == "txt"
        assert OutputFormat.MD.value == "md"
        assert OutputFormat.JSON.value == "json"

    def test_operation_type_values(self):
        assert OperationType.INGESTION.value == "ingestion"
        assert OperationType.DIGITIZATION.value == "digitization"

    def test_job_status_values(self):
        assert JobStatus.ACCEPTED.value == "accepted"
        assert JobStatus.IN_PROGRESS.value == "in_progress"
        assert JobStatus.COMPLETED.value == "completed"
        assert JobStatus.FAILED.value == "failed"

    def test_doc_status_values(self):
        assert {
            status.value for status in DocStatus
        } == {
            "accepted",
            "in_progress",
            "digitized",
            "processed",
            "chunked",
            "completed",
            "failed",
        }

    def test_enum_string_conversion(self):
        assert str(OutputFormat.JSON) == "OutputFormat.JSON"
        assert JobStatus("completed") == JobStatus.COMPLETED
        assert DocStatus("failed") == DocStatus.FAILED

    def test_invalid_enum_values_raise(self):
        with pytest.raises(ValueError):
            OutputFormat("xml")

        with pytest.raises(ValueError):
            JobStatus("queued")


@pytest.mark.unit
class TestResponseModels:
    def test_pagination_info_model(self):
        model = PaginationInfo(total=10, limit=5, offset=0)

        assert model.total == 10
        assert model.limit == 5
        assert model.offset == 0

    def test_job_created_response_model(self):
        model = JobCreatedResponse(job_id="job-123")

        assert model.model_dump() == {"job_id": "job-123"}

    def test_document_list_item_optional_fields(self):
        model = DocumentListItem(
            id="doc-1",
            name="sample.pdf",
            type="digitization",
            status="accepted",
        )

        assert model.submitted_at is None

    def test_documents_list_response_with_pagination(self):
        payload = DocumentsListResponse(
            pagination=PaginationInfo(total=1, limit=20, offset=0),
            data=[
                DocumentListItem(
                    id="doc-1",
                    name="sample.pdf",
                    type="ingestion",
                    status="completed",
                    submitted_at="2024-01-01T00:00:00Z",
                )
            ],
        )

        assert payload.pagination.total == 1
        assert payload.data[0].id == "doc-1"

    def test_jobs_list_response(self):
        payload = JobsListResponse(
            pagination=PaginationInfo(total=1, limit=20, offset=0),
            data=[{"job_id": "job-1", "status": "completed"}],
        )

        assert payload.data[0]["job_id"] == "job-1"

    def test_document_detail_response_model(self):
        model = DocumentDetailResponse(
            id="doc-1",
            job_id="job-1",
            name="sample.pdf",
            type="digitization",
            status="completed",
            output_format="json",
            metadata={"pages": 2},
        )

        assert model.metadata == {"pages": 2}

    def test_document_content_response_dict_and_str(self):
        json_model = DocumentContentResponse(result={"text": "hello"}, output_format="json")
        text_model = DocumentContentResponse(result="hello", output_format="txt")

        assert json_model.result == {"text": "hello"}
        assert text_model.result == "hello"

    def test_model_validation_error(self):
        with pytest.raises(ValidationError):
            PaginationInfo(total="bad", limit=10, offset=0)

    def test_model_serialization_to_dict(self):
        model = DocumentListItem(
            id="doc-1",
            name="sample.pdf",
            type="ingestion",
            status="accepted",
            submitted_at="2024-01-01T00:00:00Z",
        )

        assert model.model_dump() == {
            "id": "doc-1",
            "name": "sample.pdf",
            "type": "ingestion",
            "status": "accepted",
            "submitted_at": "2024-01-01T00:00:00Z",
        }

    def test_model_json_serialization(self):
        model = JobCreatedResponse(job_id="job-123")

        assert json.loads(model.model_dump_json()) == {"job_id": "job-123"}


@pytest.mark.unit
class TestDocumentAndJobModels:
    def test_document_metadata_defaults_and_conversion(self):
        model = DocumentMetadata(
            id="doc-1",
            name="sample.pdf",
            type="digitization",
            status="completed",
            output_format="md",
        )

        assert model.status == "completed"
        assert model.output_format == "md"
        assert model.metadata == {}

    def test_document_metadata_invalid_values_default(self):
        model = DocumentMetadata(
            id="doc-1",
            name="sample.pdf",
            type="digitization",
            status="bad",
            output_format="bad",
        )

        assert model.status == "accepted"
        assert model.output_format == "json"

    def test_document_metadata_to_dict_and_job_summary(self):
        model = DocumentMetadata(
            id="doc-1",
            name="sample.pdf",
            type="digitization",
            status=DocStatus.COMPLETED,
            output_format=OutputFormat.JSON,
        )

        dumped = model.to_dict()

        assert dumped["id"] == "doc-1"
        assert model.job_summary() == {
            "id": "doc-1",
            "name": "sample.pdf",
            "status": "completed",
        }

    def test_timing_info_model(self):
        timing = TimingInfo(digitizing=1.2, processing=2.5)

        assert timing.model_dump() == {
            "digitizing": 1.2,
            "processing": 2.5,
            "chunking": None,
            "indexing": None,
        }

    def test_job_document_summary_model(self):
        summary = JobDocumentSummary(id="doc-1", name="sample.pdf", status="accepted")

        assert summary.id == "doc-1"

    def test_job_stats_validation_and_constraints(self):
        stats = JobStats(total_documents=2, completed=1, failed=1, in_progress=0)

        assert stats.total_documents == 2

        with pytest.raises(ValidationError):
            JobStats(total_documents=-1)

    def test_job_state_creation_and_filtering(self):
        state = JobState(
            job_id="job-1",
            operation="ingestion",
            status="in_progress",
            submitted_at="2024-01-01T00:00:00Z",
            documents=[
                {"id": "doc-1", "name": "a.pdf", "status": "accepted"},
                {"id": "missing-name"},
                "invalid",
            ],
            stats={"total_documents": 1, "completed": 0, "failed": 0, "in_progress": 1},
        )

        assert state.status == "in_progress"
        assert len(state.documents) == 1
        assert state.documents[0].id == "doc-1"

    def test_job_state_invalid_status_and_stats_default(self):
        state = JobState(
            job_id="job-1",
            operation="ingestion",
            status="bad",
            submitted_at="2024-01-01T00:00:00Z",
            documents=[],
            stats="bad",
        )

        assert state.status == "accepted"
        assert state.stats.model_dump() == {
            "total_documents": 0,
            "completed": 0,
            "failed": 0,
            "in_progress": 0,
        }

    def test_job_state_to_dict(self):
        state = JobState(
            job_id="job-1",
            job_name="My Job",
            operation="ingestion",
            status=JobStatus.COMPLETED,
            submitted_at="2024-01-01T00:00:00Z",
            completed_at="2024-01-01T01:00:00Z",
            documents=[],
            stats=JobStats(total_documents=0, completed=0, failed=0, in_progress=0),
            error=None,
        )

        dumped = state.to_dict()

        assert dumped["job_name"] == "My Job"
        assert dumped["completed_at"] == "2024-01-01T01:00:00Z"

# Made with Bob
