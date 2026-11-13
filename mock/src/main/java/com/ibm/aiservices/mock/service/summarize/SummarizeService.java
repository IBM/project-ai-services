package com.ibm.aiservices.mock.service.summarize;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.InvalidBodyException;
import com.ibm.aiservices.mock.http.MultipartForm;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.http.SseWriter;
import com.ibm.aiservices.mock.service.JobPhase;
import com.ibm.aiservices.mock.service.common.ErrorCode;
import com.ibm.aiservices.mock.service.common.FastApi;
import com.ibm.aiservices.mock.service.common.FastApiService;
import com.ibm.aiservices.mock.service.common.MockText;
import com.ibm.aiservices.mock.service.common.Pages;
import com.ibm.aiservices.mock.service.common.UploadedText;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;
import com.ibm.aiservices.mock.util.Times;

/**
 * Mock of the summarize service ({@code services/summarize}).
 */
public final class SummarizeService extends FastApiService {

    private static final List<String> LEVELS = Arrays.asList("brief", "standard", "detailed");
    private static final List<String> STATUSES = Arrays.asList("accepted", "in_progress", "completed", "failed");
    private static final int CHUNK_WORDS = 1500;
    private static final int MAX_LENGTH = 2000;

    private final Map<String, SummarizeJob> m_jobs = Collections.synchronizedMap(new LinkedHashMap<String, SummarizeJob>());

    /**
     * State of one submitted summarization job.
     */
    private static final class SummarizeJob {

        private final String m_id = Ids.uuid();
        private final long m_submittedAt = System.currentTimeMillis();
        private final String m_name;
        private final String m_documentName;
        private final String m_text;
        private final String m_level;
        private final boolean m_isFailing;

        /**
         * Creates a job record.
         *
         * @param _name the job name, or {@code null}
         * @param _documentName the uploaded file name
         * @param _text the extracted text
         * @param _level the summary level
         * @param _isFailing whether the simulated job should fail
         */
        SummarizeJob(final String _name, final String _documentName, final String _text, final String _level, final boolean _isFailing) {
            m_name = _name;
            m_documentName = _documentName;
            m_text = _text;
            m_level = _level;
            m_isFailing = _isFailing;
        }

        int chunkCount() {
            return Math.max(1, (MockText.wordCount(m_text) + CHUNK_WORDS - 1) / CHUNK_WORDS);
        }

        String jobType() {
            return 1 < chunkCount() ? "chunked" : "direct";
        }
    }

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public SummarizeService(final MockConfig _config) {
        super(_config);
    }

    @Override
    public String getName() {
        return "summarize";
    }

    @Override
    public String getTitle() {
        return "Summarize API";
    }

    @Override
    public int getDefaultPort() {
        return 6000;
    }

    @Override
    protected void registerApiRoutes(final Router _router) {
        _router.post("/v1/summarize", this::summarize);
        _router.post("/v1/summarize/jobs", this::createJob);
        _router.get("/v1/summarize/jobs", this::listJobs);
        _router.delete("/v1/summarize/jobs", this::deleteAllJobs);
        _router.get("/v1/summarize/jobs/{job_id}", this::getJob);
        _router.get("/v1/summarize/jobs/{job_id}/result", this::getJobResult);
        _router.delete("/v1/summarize/jobs/{job_id}", this::deleteJob);
    }

    /**
     * Handles {@code POST /v1/summarize}: synchronous summarization of JSON text or an uploaded file.
     *
     * @param _request the request
     * @return the summary, or an SSE stream when {@code stream=true}
     */
    private Response summarize(final Request _request) {
        final long started = System.currentTimeMillis();
        final String level = _request.getQuery("level");
        final String lengthText = _request.getQuery("length");
        if (null != level && null != lengthText) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Cannot specify both 'level' and 'length'. Please use only one.");
        }
        if (null != level && !LEVELS.contains(level)) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "level must be one of: brief, standard, detailed");
        }
        final Integer length = parseLength(lengthText);
        final boolean stream = FastApi.queryBool(_request, "stream", false);

        final String text;
        final String inputType;
        if (_request.hasContentType("application/json")) {
            final JsonBody body;
            try {
                body = _request.getJson();
            } catch (final InvalidBodyException e) {
                throw FastApi.error(ErrorCode.INVALID_REQUEST, "Request body is not valid JSON");
            }
            if (!body.has("text")) {
                throw FastApi.error(ErrorCode.MISSING_INPUT, "Either 'text' or 'file' parameter is required");
            }
            text = body.getString("text", "");
            inputType = "text";
        } else if (_request.hasContentType("multipart/form-data")) {
            final MultipartForm.Part file = _request.getMultipart().getFile("file");
            if (null == file) {
                throw FastApi.error(ErrorCode.MISSING_INPUT, "Either 'text' or 'file' parameter is required");
            }
            checkFileType(file.getFilename());
            text = UploadedText.extract(file);
            inputType = "file";
        } else {
            throw FastApi.error(ErrorCode.UNSUPPORTED_CONTENT_TYPE, "Content-Type must be application/json or multipart/form-data");
        }
        if (text.trim().isEmpty()) {
            throw FastApi.error(ErrorCode.EMPTY_INPUT, "The provided input contains no extractable text.");
        }
        final int originalLength = MockText.wordCount(text);
        if (null != length && originalLength < length.intValue()) {
            throw FastApi.error(ErrorCode.INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH, "Input text (" + originalLength + " words) is smaller than the requested summary length (" + length + " words)");
        }
        final String summary = MockText.summarize(text, targetWords(level, length, originalLength));
        if (stream) {
            return streamSummary(summary, text);
        }
        final Map<String, Object> meta = Json.obj("model", MockText.MODEL, "processing_time_ms", Long.valueOf(System.currentTimeMillis() - started), "input_type", inputType);
        return Response.json(200, resultPayload(summary, originalLength, meta, text));
    }

    /**
     * Parses the legacy {@code length} query parameter.
     *
     * @param _lengthText the raw value, or {@code null}
     * @return the length in words, or {@code null} if absent
     */
    private static Integer parseLength(final String _lengthText) {
        if (null == _lengthText) {
            return null;
        }
        final int length;
        try {
            length = Integer.parseInt(_lengthText.trim());
        } catch (final NumberFormatException e) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Length must be an integer");
        }
        if (length < 1 || length > MAX_LENGTH) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Length is out of bounds");
        }
        return Integer.valueOf(length);
    }

    private static void checkFileType(final String _filename) {
        final String extension = UploadedText.extension(_filename);
        if (!".txt".equals(extension) && !".pdf".equals(extension)) {
            throw FastApi.error(ErrorCode.UNSUPPORTED_FILE_TYPE, "Only .txt and .pdf files are allowed. Received: " + (extension.isEmpty() ? "(none)" : extension));
        }
    }

    /**
     * Picks the summary length in words for a level or explicit length.
     *
     * @param _level the requested level, or {@code null}
     * @param _length the requested length, or {@code null}
     * @param _originalLength the input length in words
     * @return the target summary length in words
     */
    private static int targetWords(final String _level, final Integer _length, final int _originalLength) {
        if (null != _length) {
            return _length.intValue();
        }
        final double ratio = "brief".equals(_level) ? 0.1 : "detailed".equals(_level) ? 0.4 : 0.2;
        return Math.max(10, Math.min(600, (int) (_originalLength * ratio)));
    }

    /**
     * Builds the {@code data}, {@code meta} and {@code usage} result envelope.
     *
     * @param _summary the summary
     * @param _originalLength the input length in words
     * @param _meta the metadata block
     * @param _text the input text, for token usage
     * @return the result payload
     */
    private static Map<String, Object> resultPayload(final String _summary, final int _originalLength, final Map<String, Object> _meta, final String _text) {
        final int inputTokens = MockText.tokenCount(_text) + 120;
        final int outputTokens = MockText.tokenCount(_summary);
        return Json.obj("data", Json.obj("summary", _summary, "original_length", Integer.valueOf(_originalLength), "summary_length",
                Integer.valueOf(MockText.wordCount(_summary))), "meta", _meta, "usage", Json.obj("input_tokens", Integer.valueOf(inputTokens), "output_tokens",
                Integer.valueOf(outputTokens), "total_tokens", Integer.valueOf(inputTokens + outputTokens)));
    }

    /**
     * Streams a summary as relayed vLLM chunks, followed by a usage chunk and {@code [DONE]}.
     *
     * @param _summary the summary text
     * @param _text the input text, for usage figures
     * @return the streaming response
     */
    private Response streamSummary(final String _summary, final String _text) {
        final long delay = getConfig().getStreamDelayMillis();
        final String completionId = "chatcmpl-" + Ids.uuid().replace("-", "");
        final long created = System.currentTimeMillis() / 1000L;
        return Response.stream(200, Response.SSE_CONTENT_TYPE, _out -> {
            final SseWriter sse = new SseWriter(_out, delay);
            for (final String chunk : MockText.streamChunks(_summary)) {
                sse.pause();
                final Map<String, Object> choice = Json.obj("index", Integer.valueOf(0), "delta", Json.obj("content", chunk), "logprobs", null, "finish_reason", null);
                sse.dataJson(Json.obj("id", completionId, "object", "chat.completion.chunk", "created", Long.valueOf(created), "model", MockText.MODEL, "choices", Json.list(choice)));
            }
            final Map<String, Object> stop = Json.obj("index", Integer.valueOf(0), "delta", Json.obj("content", ""), "logprobs", null, "finish_reason", "stop");
            sse.dataJson(Json.obj("id", completionId, "object", "chat.completion.chunk", "created", Long.valueOf(created), "model", MockText.MODEL, "choices", Json.list(stop)));
            final int promptTokens = MockText.tokenCount(_text) + 120;
            final int completionTokens = MockText.tokenCount(_summary);
            sse.dataJson(Json.obj("id", completionId, "object", "chat.completion.chunk", "created", Long.valueOf(created), "model", MockText.MODEL, "choices", Json.list(),
                    "usage", Json.obj("prompt_tokens", Integer.valueOf(promptTokens), "total_tokens", Integer.valueOf(promptTokens + completionTokens), "completion_tokens",
                    Integer.valueOf(completionTokens))));
            sse.data("[DONE]");
        }).withHeader("Cache-Control", "no-cache").withHeader("Connection", "keep-alive");
    }

    /**
     * Handles {@code POST /v1/summarize/jobs}.
     *
     * @param _request the multipart request
     * @return {@code 202} with the new job ID
     */
    private Response createJob(final Request _request) {
        if (!_request.hasContentType("multipart/form-data")) {
            throw FastApi.missing("body", "file");
        }
        final MultipartForm form = _request.getMultipart();
        final MultipartForm.Part file = form.getFile("file");
        if (null == file) {
            throw FastApi.missing("body", "file");
        }
        checkFileType(file.getFilename());
        final String levelField = form.getField("level");
        final String level = null == levelField || levelField.trim().isEmpty() ? "standard" : levelField.trim();
        if (!LEVELS.contains(level)) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "level must be one of: brief, standard, detailed");
        }
        final String jobName = form.getField("job_name");
        final SummarizeJob job = new SummarizeJob(jobName, file.getFilename(), UploadedText.extract(file), level, requestsFailure(jobName, file.getFilename()));
        m_jobs.put(job.m_id, job);
        return Response.json(202, Json.obj("job_id", job.m_id));
    }

    /**
     * Derives a job's status from its simulated progress.
     *
     * @param _job the job
     * @return the job status
     */
    private String statusOf(final SummarizeJob _job) {
        final JobPhase phase = phaseOf(_job.m_submittedAt);
        if (JobPhase.QUEUED == phase) {
            return "accepted";
        }
        if (JobPhase.RUNNING == phase) {
            return "in_progress";
        }
        return _job.m_isFailing ? "failed" : "completed";
    }

    private String completedAt(final SummarizeJob _job) {
        return JobPhase.DONE == phaseOf(_job.m_submittedAt) ? Times.isoMicrosOffset(completionTime(_job.m_submittedAt)) : null;
    }

    private String errorOf(final SummarizeJob _job) {
        return "failed".equals(statusOf(_job)) ? "Failed to generate summary. Please try again later" : null;
    }

    /**
     * Handles {@code GET /v1/summarize/jobs}.
     *
     * @param _request the request
     * @return a page of jobs, newest first
     */
    private Response listJobs(final Request _request) {
        final boolean latest = FastApi.queryBool(_request, "latest", false);
        int limit = FastApi.queryInt(_request, "limit", 20, null, null);
        int offset = FastApi.queryInt(_request, "offset", 0, null, null);
        if (limit < 1 || limit > 100) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Limit must be between 1 and 100");
        }
        if (offset < 0) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Offset must be non-negative");
        }
        final String status = null == _request.getQuery("status") ? null : _request.getQuery("status").toLowerCase(Locale.ROOT);
        if (null != status && !STATUSES.contains(status)) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Invalid status value. Must be one of: accepted, in_progress, completed, failed");
        }
        if (latest) {
            limit = 1;
            offset = 0;
        }
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        for (final SummarizeJob job : newestFirst()) {
            final String jobStatus = statusOf(job);
            if (null != status && !status.equals(jobStatus)) {
                continue;
            }
            final String completedAt = completedAt(job);
            items.add(Json.obj("job_id", job.m_id, "job_name", job.m_name, "status", jobStatus, "submitted_at", Times.isoMicrosOffset(job.m_submittedAt), "completed_at",
                    completedAt, "updated_at", null == completedAt ? Times.isoMicrosOffset(job.m_submittedAt) : completedAt, "document_name", job.m_documentName,
                    "document_word_count", Integer.valueOf(MockText.wordCount(job.m_text)), "level", job.m_level, "job_type", job.jobType(), "error", errorOf(job)));
        }
        return Response.json(200, Pages.offsetEnvelope(items.size(), limit, offset, Pages.slice(items, offset, limit)));
    }

    /**
     * Snapshots the jobs, newest first.
     *
     * @return the jobs
     */
    private List<SummarizeJob> newestFirst() {
        final List<SummarizeJob> jobs;
        synchronized (m_jobs) {
            jobs = new ArrayList<SummarizeJob>(m_jobs.values());
        }
        Collections.reverse(jobs);
        return jobs;
    }

    /**
     * Resolves the {@code job_id} job.
     *
     * @param _request the request
     * @return the job
     */
    private SummarizeJob requireJob(final Request _request) {
        final String jobId = _request.getPathParam("job_id");
        final SummarizeJob job = m_jobs.get(jobId);
        if (null == job) {
            throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "Job " + jobId + " not found");
        }
        return job;
    }

    /**
     * Handles {@code GET /v1/summarize/jobs/{job_id}}.
     *
     * @param _request the request
     * @return the job status
     */
    private Response getJob(final Request _request) {
        final SummarizeJob job = requireJob(_request);
        final String status = statusOf(job);
        final int totalChunks = job.chunkCount();
        final int completedChunks;
        final String phase;
        if ("accepted".equals(status)) {
            completedChunks = 0;
            phase = "";
        } else if ("in_progress".equals(status)) {
            final double progress = JobPhase.progress(job.m_submittedAt, jobDuration(), System.currentTimeMillis());
            completedChunks = Math.min(totalChunks, (int) (progress * totalChunks));
            phase = completedChunks >= totalChunks && 1 < totalChunks ? "merging" : "summarizing";
        } else {
            completedChunks = totalChunks;
            phase = "";
        }
        final Map<String, Object> metadata = Json.obj("total_chunks", Integer.valueOf(totalChunks), "completed_chunks", Integer.valueOf(completedChunks), "phase", phase);
        return Response.json(200, Json.obj("job_id", job.m_id, "job_name", job.m_name, "status", status, "submitted_at", Times.isoMicrosOffset(job.m_submittedAt), "completed_at", completedAt(job), "document", Json.obj("name", job.m_documentName, "status", status), "error", errorOf(job), "metadata", metadata));
    }

    /**
     * Handles {@code GET /v1/summarize/jobs/{job_id}/result}.
     *
     * @param _request the request
     * @return the summary, {@code 202} while running, or {@code 422} if the job failed
     */
    private Response getJobResult(final Request _request) {
        final SummarizeJob job = requireJob(_request);
        final String status = statusOf(job);
        if ("accepted".equals(status) || "in_progress".equals(status)) {
            return Response.json(202, Json.obj("message", "Job is still in progress", "job_id", job.m_id, "status", status));
        }
        if ("failed".equals(status)) {
            throw FastApi.error(ErrorCode.JOB_FAILED, "Job failed: " + errorOf(job));
        }
        final int originalLength = MockText.wordCount(job.m_text);
        final String summary = MockText.summarize(job.m_text, targetWords(job.m_level, null, originalLength));
        final Map<String, Object> meta = Json.obj("model", MockText.MODEL, "processing_time_ms", Long.valueOf(jobDuration()), "input_type", "file", "strategy", job.jobType());
        if ("chunked".equals(job.jobType())) {
            meta.put("chunks_processed", Integer.valueOf(job.chunkCount()));
        }
        return Response.json(200, resultPayload(summary, originalLength, meta, job.m_text));
    }

    /**
     * Handles {@code DELETE /v1/summarize/jobs/{job_id}}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteJob(final Request _request) {
        final SummarizeJob job = requireJob(_request);
        final String status = statusOf(job);
        if ("accepted".equals(status) || "in_progress".equals(status)) {
            throw FastApi.error(ErrorCode.RESOURCE_LOCKED, "Cannot delete active job. Current status: " + status);
        }
        m_jobs.remove(job.m_id);
        return Response.empty(204);
    }

    /**
     * Handles {@code DELETE /v1/summarize/jobs?confirm=true}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteAllJobs(final Request _request) {
        if (!FastApi.queryBool(_request, "confirm", false)) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "Bulk delete requires confirm=true query parameter");
        }
        synchronized (m_jobs) {
            int active = 0;
            for (final SummarizeJob job : m_jobs.values()) {
                final String status = statusOf(job);
                if ("accepted".equals(status) || "in_progress".equals(status)) {
                    active++;
                }
            }
            if (0 < active) {
                throw FastApi.error(ErrorCode.RESOURCE_LOCKED, "Cannot delete: " + active + " active job(s) exist");
            }
            m_jobs.clear();
        }
        return Response.empty(204);
    }
}
