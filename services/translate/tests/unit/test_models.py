"""
Unit tests for translate/models.py — Pydantic models and enums.
"""

import pytest
from pydantic import ValidationError

from translate.models import (
    InputType,
    JobCreatedResponse,
    JobDetailResponse,
    JobResultResponse,
    JobState,
    JobStatus,
    JobsListResponse,
    PaginationInfo,
    SyncTranslateRequest,
    SyncTranslateResponse,
    TranslationChunk,
)


@pytest.mark.unit
class TestJobStatus:
    def test_all_expected_values(self):
        assert JobStatus.ACCEPTED.value == "accepted"
        assert JobStatus.IN_PROGRESS.value == "in_progress"
        assert JobStatus.COMPLETED.value == "completed"
        assert JobStatus.FAILED.value == "failed"

    def test_is_str_enum(self):
        assert isinstance(JobStatus.ACCEPTED, str)

    def test_construction_from_string(self):
        assert JobStatus("completed") == JobStatus.COMPLETED


@pytest.mark.unit
class TestInputType:
    def test_all_expected_values(self):
        assert InputType.TEXT.value == "text"
        assert InputType.TXT.value == "txt"
        assert InputType.MD.value == "md"

    def test_is_str_enum(self):
        assert isinstance(InputType.TXT, str)


@pytest.mark.unit
class TestSyncTranslateRequest:
    def test_valid_request(self):
        req = SyncTranslateRequest(
            text="Hello world",
            source_language="German",
            target_language="English",
        )
        assert req.text == "Hello world"
        assert req.source_language == "German"
        assert req.target_language == "English"

    def test_source_language_defaults_to_auto(self):
        req = SyncTranslateRequest(text="Hi", target_language="English")
        assert req.source_language == "auto"

    def test_missing_text_raises(self):
        with pytest.raises(ValidationError):
            SyncTranslateRequest(target_language="English")

    def test_missing_target_language_raises(self):
        with pytest.raises(ValidationError):
            SyncTranslateRequest(text="Hello")


@pytest.mark.unit
class TestSyncTranslateResponse:
    def test_valid_response(self):
        resp = SyncTranslateResponse(
            data={"translation": "Hallo", "source_language": "english", "target_language": "german"},
            meta={"model": "test-model", "processing_time_ms": 500, "input_type": "text"},
            usage={"input_tokens": 10, "output_tokens": 8, "total_tokens": 18},
        )
        assert resp.data["translation"] == "Hallo"
        assert resp.meta["model"] == "test-model"
        assert resp.usage["total_tokens"] == 18


@pytest.mark.unit
class TestJobCreatedResponse:
    def test_valid(self):
        resp = JobCreatedResponse(job_id="abc-123")
        assert resp.job_id == "abc-123"


@pytest.mark.unit
class TestPaginationInfo:
    def test_valid(self):
        p = PaginationInfo(total=100, limit=20, offset=40)
        assert p.total == 100
        assert p.limit == 20
        assert p.offset == 40


@pytest.mark.unit
class TestJobDetailResponse:
    def test_valid_with_known_status(self):
        resp = JobDetailResponse(
            job_id="job-1",
            status="completed",
            source_language="german",
            target_language="english",
            input_type="txt",
            submitted_at="2024-01-01T00:00:00Z",
        )
        assert resp.status == "completed"
        assert resp.job_name is None
        assert resp.completed_at is None

    def test_status_validator_with_jobstatus_instance(self):
        resp = JobDetailResponse(
            job_id="job-1",
            status=JobStatus.IN_PROGRESS,
            source_language="german",
            target_language="english",
            input_type="txt",
            submitted_at="2024-01-01T00:00:00Z",
        )
        assert resp.status == "in_progress"

    def test_status_validator_falls_back_to_accepted_on_invalid(self):
        resp = JobDetailResponse(
            job_id="job-1",
            status="NOT_A_STATUS",
            source_language="german",
            target_language="english",
            input_type="txt",
            submitted_at="2024-01-01T00:00:00Z",
        )
        assert resp.status == "accepted"

    def test_optional_fields_populated(self):
        resp = JobDetailResponse(
            job_id="job-1",
            job_name="My Job",
            status="completed",
            source_language="german",
            target_language="english",
            input_type="md",
            document_name="doc.md",
            submitted_at="2024-01-01T00:00:00Z",
            completed_at="2024-01-01T01:00:00Z",
            error=None,
            job_metadata={"phase": "completed"},
        )
        assert resp.job_name == "My Job"
        assert resp.document_name == "doc.md"
        assert resp.job_metadata == {"phase": "completed"}


@pytest.mark.unit
class TestJobState:
    def test_valid(self):
        state = JobState(
            job_id="job-2",
            status="in_progress",
            source_language="auto",
            target_language="english",
            input_type="md",
            submitted_at="2024-01-01T00:00:00Z",
        )
        assert state.status == "in_progress"

    def test_status_validator_falls_back(self):
        state = JobState(
            job_id="job-2",
            status="GARBAGE",
            source_language="auto",
            target_language="english",
            input_type="md",
            submitted_at="2024-01-01T00:00:00Z",
        )
        assert state.status == "accepted"


@pytest.mark.unit
class TestJobsListResponse:
    def test_valid(self):
        jobs_resp = JobsListResponse(
            pagination=PaginationInfo(total=1, limit=20, offset=0),
            data=[
                JobState(
                    job_id="j1",
                    status="completed",
                    source_language="german",
                    target_language="english",
                    input_type="txt",
                    submitted_at="2024-01-01T00:00:00Z",
                )
            ],
        )
        assert jobs_resp.pagination.total == 1
        assert len(jobs_resp.data) == 1


@pytest.mark.unit
class TestJobResultResponse:
    def test_valid(self):
        resp = JobResultResponse(
            data={"translation": "Hallo"},
            meta={"model": "m"},
            usage={"input_tokens": 5},
        )
        assert resp.data["translation"] == "Hallo"


@pytest.mark.unit
class TestTranslationChunk:
    def test_defaults(self):
        chunk = TranslationChunk(index=0, text="Hello world")
        assert chunk.join_after == "paragraph"
        assert chunk.token_count == 0

    def test_sentence_join(self):
        chunk = TranslationChunk(index=1, text="Sentence.", join_after="sentence", token_count=5)
        assert chunk.join_after == "sentence"
        assert chunk.token_count == 5

    def test_index_ordering(self):
        chunks = [
            TranslationChunk(index=2, text="C"),
            TranslationChunk(index=0, text="A"),
            TranslationChunk(index=1, text="B"),
        ]
        chunks.sort(key=lambda c: c.index)
        assert [c.text for c in chunks] == ["A", "B", "C"]

# Made with Bob
