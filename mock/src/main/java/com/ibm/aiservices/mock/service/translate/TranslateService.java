package com.ibm.aiservices.mock.service.translate;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.ApiException;
import com.ibm.aiservices.mock.http.MultipartForm;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
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
 * Mock of the translate service ({@code services/translate}).
 */
public final class TranslateService extends FastApiService {

    private static final List<String> LANGUAGES = Arrays.asList("english", "german");
    private static final List<String> STATUSES = Arrays.asList("accepted", "in_progress", "completed", "failed");
    private static final String SUPPORTED = "Supported: English, German.";
    private static final int MAX_FILE_BYTES = 10 * 1024 * 1024;
    private static final int CHUNK_TOKEN_BUDGET = 2048;

    private final Map<String, TranslateJob> m_jobs = Collections.synchronizedMap(new LinkedHashMap<String, TranslateJob>());

    /**
     * State of one submitted translation job.
     */
    private static final class TranslateJob {

        private final String m_id = Ids.uuid();
        private final long m_submittedAt = System.currentTimeMillis();
        private final String m_name;
        private final String m_documentName;
        private final String m_inputType;
        private final String m_text;
        private final String m_requestedSource;
        private final String m_resolvedSource;
        private final String m_target;
        private final boolean m_isFailing;

        /**
         * Creates a job record, resolving an {@code auto} source language immediately.
         *
         * @param _name the job name, or {@code null}
         * @param _documentName the uploaded file name
         * @param _text the file text
         * @param _requestedSource the requested source language, possibly {@code auto}
         * @param _target the target language
         * @param _isFailing whether the simulated job should fail
         */
        TranslateJob(final String _name, final String _documentName, final String _text, final String _requestedSource, final String _target, final boolean _isFailing) {
            m_name = _name;
            m_documentName = _documentName;
            m_inputType = UploadedText.extension(_documentName).substring(1);
            m_text = _text;
            m_requestedSource = _requestedSource;
            m_resolvedSource = "auto".equals(_requestedSource) ? otherThan(MockText.detectLanguage(_text), _target) : _requestedSource;
            m_target = _target;
            m_isFailing = _isFailing;
        }
    }

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public TranslateService(final MockConfig _config) {
        super(_config);
    }

    @Override
    public String getName() {
        return "translate";
    }

    @Override
    public String getTitle() {
        return "Translate API";
    }

    @Override
    public int getDefaultPort() {
        return 9000;
    }

    @Override
    protected void registerApiRoutes(final Router _router) {
        _router.post("/v1/translate", this::translate);
        _router.post("/v1/translate/jobs", this::createJob);
        _router.get("/v1/translate/jobs", this::listJobs);
        _router.get("/v1/translate/jobs/{job_id}", this::getJob);
        _router.get("/v1/translate/jobs/{job_id}/result", this::getResult);
        _router.get("/v1/translate/jobs/{job_id}/result/download", this::downloadResult);
    }

    /**
     * Returns the detected language, or the other supported language when detection matches the target.
     *
     * @param _detected the detected language
     * @param _target the target language
     * @return a source language different from the target
     */
    private static String otherThan(final String _detected, final String _target) {
        if (!_detected.equals(_target)) {
            return _detected;
        }
        return "english".equals(_target) ? "german" : "english";
    }

    /**
     * Validates and normalizes a source and target language pair.
     *
     * @param _source the requested source language; {@code null} or empty means auto
     * @param _target the requested target language
     * @return {@code {source, target}} in lower case
     */
    private static String[] resolveLanguages(final String _source, final String _target) {
        if (null == _target || _target.trim().isEmpty()) {
            throw FastApi.rawError(400, "INVALID_LANGUAGE", "target_language must not be empty. " + SUPPORTED);
        }
        final String target = _target.trim().toLowerCase(Locale.ROOT);
        if ("auto".equals(target)) {
            throw FastApi.rawError(400, "INVALID_LANGUAGE", "'auto' is not valid for target_language. Please specify an explicit target language. " + SUPPORTED);
        }
        if (!LANGUAGES.contains(target)) {
            throw FastApi.rawError(400, "INVALID_LANGUAGE", "'" + _target.trim() + "' is not a supported language. " + SUPPORTED);
        }
        final String source = null == _source || _source.trim().isEmpty() ? "auto" : _source.trim().toLowerCase(Locale.ROOT);
        if (!"auto".equals(source) && !LANGUAGES.contains(source)) {
            throw FastApi.rawError(400, "INVALID_LANGUAGE", "'" + _source.trim() + "' is not a supported language. " + SUPPORTED);
        }
        if (source.equals(target)) {
            throw FastApi.rawError(400, "SAME_LANGUAGE", "source_language and target_language must differ (both are '" + target + "').");
        }
        return new String[] {source, target};
    }

    /**
     * Handles {@code POST /v1/translate}.
     *
     * @param _request the request
     * @return the translation
     */
    private Response translate(final Request _request) {
        final long started = System.currentTimeMillis();
        final JsonBody body = _request.getJson();
        if (!body.has("text")) {
            throw FastApi.missing("body", "text");
        }
        if (!body.has("target_language")) {
            throw FastApi.missing("body", "target_language");
        }
        final String text = body.getString("text", "");
        if (text.trim().isEmpty()) {
            throw FastApi.error(ErrorCode.EMPTY_INPUT, "The 'text' field must not be empty.");
        }
        final String[] languages = resolveLanguages(body.getString("source_language", "auto"), body.getString("target_language", null));
        final String source = "auto".equals(languages[0]) ? otherThan(MockText.detectLanguage(text), languages[1]) : languages[0];
        final String translation = MockText.translate(text, languages[1]);
        final int inputTokens = MockText.tokenCount(text) + 60;
        final int outputTokens = MockText.tokenCount(translation);
        final Map<String, Object> data = Json.obj("translation", translation, "source_language", source, "target_language", languages[1]);
        final Map<String, Object> meta = Json.obj("model", MockText.MODEL, "processing_time_ms", Long.valueOf(System.currentTimeMillis() - started), "input_type", "text");
        return Response.json(200, Json.obj("data", data, "meta", meta, "usage", usage(inputTokens, outputTokens)));
    }

    private static Map<String, Object> usage(final int _inputTokens, final int _outputTokens) {
        return Json.obj("input_tokens", Integer.valueOf(_inputTokens), "output_tokens", Integer.valueOf(_outputTokens), "total_tokens", Integer.valueOf(_inputTokens + _outputTokens));
    }

    /**
     * Handles {@code POST /v1/translate/jobs}.
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
        if (null == form.getField("target_language")) {
            throw FastApi.missing("body", "target_language");
        }
        final String extension = UploadedText.extension(file.getFilename());
        if (!".txt".equals(extension) && !".md".equals(extension)) {
            throw FastApi.rawError(415, "UNSUPPORTED_FILE_TYPE", "File type '" + extension + "' is not supported. Accepted types: md, txt.");
        }
        if (file.getData().length > MAX_FILE_BYTES) {
            throw FastApi.rawError(413, "FILE_TOO_LARGE", "File exceeds the 10 MB limit.");
        }
        final String text = new String(file.getData(), StandardCharsets.UTF_8);
        if (text.trim().isEmpty()) {
            throw FastApi.rawError(415, "INVALID_FILE_CONTENT", "File '" + file.getFilename() + "' is empty.");
        }
        final String[] languages = resolveLanguages(form.getField("source_language"), form.getField("target_language"));
        final String jobName = form.getField("job_name");
        final TranslateJob job = new TranslateJob(jobName, file.getFilename(), text, languages[0], languages[1], requestsFailure(jobName, file.getFilename()));
        m_jobs.put(job.m_id, job);
        return Response.json(202, Json.obj("job_id", job.m_id));
    }

    /**
     * Derives a job's status from its simulated progress.
     *
     * @param _job the job
     * @return the job status
     */
    private String statusOf(final TranslateJob _job) {
        final JobPhase phase = phaseOf(_job.m_submittedAt);
        if (JobPhase.QUEUED == phase) {
            return "accepted";
        }
        if (JobPhase.RUNNING == phase) {
            return "in_progress";
        }
        return _job.m_isFailing ? "failed" : "completed";
    }

    /**
     * Builds the job summary shared by the list and detail endpoints.
     *
     * @param _job the job
     * @return the summary fields
     */
    private Map<String, Object> summary(final TranslateJob _job) {
        final String status = statusOf(_job);
        final boolean isDone = "completed".equals(status) || "failed".equals(status);
        final String source = "accepted".equals(status) ? _job.m_requestedSource : _job.m_resolvedSource;
        return Json.obj("job_id", _job.m_id, "job_name", _job.m_name, "status", status, "source_language", source, "target_language", _job.m_target, "input_type",
                _job.m_inputType, "document_name", _job.m_documentName, "submitted_at", Times.isoMicrosZ(_job.m_submittedAt), "completed_at",
                isDone ? Times.isoMicrosZ(completionTime(_job.m_submittedAt)) : null, "error", "failed".equals(status) ? "LLM_ERROR: Failed to translate chunk 1" : null);
    }

    /**
     * Handles {@code GET /v1/translate/jobs}.
     *
     * @param _request the request
     * @return a page of jobs, newest first
     */
    private Response listJobs(final Request _request) {
        final int limit = FastApi.queryInt(_request, "limit", 20, Integer.valueOf(1), Integer.valueOf(100));
        final int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        final String filterParam = _request.getQuery("status_filter");
        final String filter = null == filterParam || filterParam.trim().isEmpty() ? null : filterParam.trim().toLowerCase(Locale.ROOT);
        if (null != filter && !STATUSES.contains(filter)) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Invalid status '" + filterParam + "'. Valid values: accepted, in_progress, completed, failed.");
        }
        final List<TranslateJob> jobs;
        synchronized (m_jobs) {
            jobs = new ArrayList<TranslateJob>(m_jobs.values());
        }
        Collections.reverse(jobs);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        for (final TranslateJob job : jobs) {
            if (null == filter || filter.equals(statusOf(job))) {
                items.add(summary(job));
            }
        }
        return Response.json(200, Pages.offsetEnvelope(items.size(), limit, offset, Pages.slice(items, offset, limit)));
    }

    /**
     * Resolves the {@code job_id} job.
     *
     * @param _request the request
     * @return the job
     */
    private TranslateJob requireJob(final Request _request) {
        final String jobId = _request.getPathParam("job_id");
        final TranslateJob job = m_jobs.get(jobId);
        if (null == job) {
            throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "No job found with id '" + jobId + "'.");
        }
        return job;
    }

    private int chunkCount(final TranslateJob _job) {
        return Math.max(1, (MockText.tokenCount(_job.m_text) + CHUNK_TOKEN_BUDGET - 1) / CHUNK_TOKEN_BUDGET);
    }

    /**
     * Handles {@code GET /v1/translate/jobs/{job_id}}, including the phase-dependent {@code job_metadata}.
     *
     * @param _request the request
     * @return the job details
     */
    private Response getJob(final Request _request) {
        final TranslateJob job = requireJob(_request);
        final Map<String, Object> details = summary(job);
        final String status = statusOf(job);
        final Map<String, Object> chunking = Json.obj("chunk_count", Integer.valueOf(chunkCount(job)), "chunk_token_budget", Integer.valueOf(CHUNK_TOKEN_BUDGET));
        final Map<String, Object> metadata;
        if ("accepted".equals(status)) {
            metadata = Json.obj("phase", "chunking");
        } else if ("in_progress".equals(status)) {
            metadata = Json.obj("phase", "translating", "chunking", chunking, "timing_in_secs", Json.obj("chunking", Double.valueOf(0.12)));
        } else if ("failed".equals(status)) {
            metadata = Json.obj("phase", "failed");
        } else {
            final String translation = MockText.translate(job.m_text, job.m_target);
            metadata = Json.obj("phase", "completed", "input_tokens", Integer.valueOf(MockText.tokenCount(job.m_text)), "output_tokens", Integer.valueOf(MockText.tokenCount(translation)), "model", MockText.MODEL, "processing_time_ms", Long.valueOf(jobDuration()), "timing_in_secs", timing(), "chunking", chunking);
        }
        details.put("job_metadata", metadata);
        return Response.json(200, details);
    }

    private Map<String, Object> timing() {
        final double seconds = jobDuration() / 1000.0;
        return Json.obj("chunking", Double.valueOf(0.12), "translating", Double.valueOf(Math.max(0.0, seconds - 0.12)));
    }

    /**
     * Returns the job if it has completed, otherwise throws the 202 or 410 response the result endpoints share.
     *
     * @param _request the request
     * @return the completed job
     */
    private TranslateJob requireCompleted(final Request _request) {
        final TranslateJob job = requireJob(_request);
        final String status = statusOf(job);
        if ("accepted".equals(status) || "in_progress".equals(status)) {
            throw new ApiException(Response.json(202, Json.obj("job_id", job.m_id, "status", status, "message", "Job is still " + status + ". Poll GET /v1/translate/jobs/" + job.m_id + " for status.")));
        }
        if ("failed".equals(status)) {
            throw FastApi.rawError(410, "JOB_FAILED", "Job failed: " + summary(job).get("error"));
        }
        return job;
    }

    /**
     * Handles {@code GET /v1/translate/jobs/{job_id}/result}.
     *
     * @param _request the request
     * @return the translation result
     */
    private Response getResult(final Request _request) {
        final TranslateJob job = requireCompleted(_request);
        final String translation = MockText.translate(job.m_text, job.m_target);
        final Map<String, Object> data = Json.obj("translation", translation, "source_language", job.m_resolvedSource, "target_language", job.m_target, "input_type", job.m_inputType, "document_name", job.m_documentName);
        final Map<String, Object> meta = Json.obj("model", MockText.MODEL, "processing_time_ms", Long.valueOf(jobDuration()), "timing_in_secs", timing());
        return Response.json(200, Json.obj("data", data, "meta", meta, "usage", usage(MockText.tokenCount(job.m_text), MockText.tokenCount(translation))));
    }

    /**
     * Handles {@code GET /v1/translate/jobs/{job_id}/result/download}.
     *
     * @param _request the request
     * @return the translated document as an attachment
     */
    private Response downloadResult(final Request _request) {
        final TranslateJob job = requireCompleted(_request);
        final String contentType = "md".equals(job.m_inputType) ? "text/markdown; charset=utf-8" : "text/plain; charset=utf-8";
        final String filename = UploadedText.stem(job.m_documentName) + "_translated." + job.m_inputType;
        return Response.text(200, contentType, MockText.translate(job.m_text, job.m_target)).withHeader("Content-Disposition", "attachment; filename=\"" + filename + "\"");
    }
}
