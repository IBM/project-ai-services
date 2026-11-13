package com.ibm.aiservices.mock.service.digitize;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.LocalDateTime;
import java.time.OffsetDateTime;
import java.time.ZoneOffset;
import java.time.format.DateTimeParseException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.UUID;

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
import com.ibm.aiservices.mock.service.common.Pages;
import com.ibm.aiservices.mock.service.common.UploadedText;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;
import com.ibm.aiservices.mock.util.Times;

/**
 * Mock of the digitize documents service ({@code services/digitize}): jobs, documents, admin import and export, conversion tasks and connectors.
 */
public final class DigitizeService extends FastApiService {

    private static final List<String> OPERATIONS = Arrays.asList("ingestion", "digitization");
    private static final List<String> OUTPUT_FORMATS = Arrays.asList("json", "md", "txt");
    private static final List<String> JOB_STATUSES = Arrays.asList("accepted", "in_progress", "completed", "completed_with_errors", "failed", "cancel_pending", "cancelled");
    private static final List<String> DOC_STATUSES = Arrays.asList("accepted", "already_exists", "cancelled", "chunked", "completed", "completed_with_errors", "digitized", "failed", "in_progress", "processed", "skipped");
    private static final List<String> TASK_STATUSES = Arrays.asList("cancel_pending", "cancelled", "completed", "failed", "pending", "queued", "running");
    private static final List<String> ACCEPTED_EXTENSIONS = Arrays.asList(".pdf", ".docx", ".html", ".md", ".txt");
    private static final List<String> ACTIVE_DOC_STATUSES = Arrays.asList("accepted", "in_progress", "digitized", "processed", "chunked");
    private static final long CANCEL_SETTLE_MILLIS = 1000L;
    private static final int INGESTION_QUOTA = 10;
    private static final int DIGITIZATION_QUOTA = 5;

    private final Object m_lock = new Object();
    private final Map<String, DigitizeJob> m_jobs = new LinkedHashMap<String, DigitizeJob>();
    private final Map<String, DigitizeDocument> m_documents = new LinkedHashMap<String, DigitizeDocument>();
    private final DigitizeConnectors m_connectors;

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public DigitizeService(final MockConfig _config) {
        super(_config);
        m_connectors = new DigitizeConnectors(_config);
    }

    @Override
    public String getName() {
        return "digitize";
    }

    @Override
    public String getTitle() {
        return "Digitize Documents API";
    }

    @Override
    public int getDefaultPort() {
        return 4000;
    }

    @Override
    protected String[] hiddenFromSchema() {
        return new String[] {"/v1/connectors"};
    }

    @Override
    protected void registerApiRoutes(final Router _router) {
        _router.post("/v1/jobs", this::createJob);
        _router.get("/v1/jobs", this::listJobs);
        _router.get("/v1/jobs/{job_id}", this::getJob);
        _router.delete("/v1/jobs/{job_id}", this::deleteJob);
        _router.post("/v1/jobs/{job_id}/cancel", this::cancelJob);
        _router.post("/v1/import", this::importMetadata);
        _router.get("/v1/export", this::exportMetadata);
        _router.get("/v1/conversion-tasks", this::listConversionTasks);
        _router.get("/v1/documents", this::listDocuments);
        _router.delete("/v1/documents", this::deleteAllDocuments);
        _router.get("/v1/documents/{doc_id}", this::getDocument);
        _router.get("/v1/documents/{doc_id}/content", this::getDocumentContent);
        _router.delete("/v1/documents/{doc_id}", this::deleteDocument);
        m_connectors.register(_router);
    }

    /**
     * Builds an error whose message carries an operation prefix, as {@code build_http_error_detail} does.
     *
     * @param _code the error code
     * @param _prefix the operation prefix, e.g. {@code Failed to create job}
     * @param _detail the detail text
     * @return the exception to throw
     */
    static ApiException prefixed(final ErrorCode _code, final String _prefix, final String _detail) {
        return FastApi.rawError(_code.getStatus(), _code.name(), _prefix + ": " + FastApi.message(_code, _detail));
    }

    // -------------------------------------------------------------- status

    /**
     * Derives a job's status from its simulated progress, cancellation and documents.
     *
     * @param _job the job
     * @return the job status
     */
    private String jobStatus(final DigitizeJob _job) {
        if (null != _job.getImportedStatus()) {
            return _job.getImportedStatus();
        }
        if (_job.isCancelRequested()) {
            return System.currentTimeMillis() - _job.getCancelRequestedAt() < CANCEL_SETTLE_MILLIS ? "cancel_pending" : "cancelled";
        }
        final JobPhase phase = phaseOf(_job.getSubmittedAt());
        if (JobPhase.QUEUED == phase) {
            return "accepted";
        }
        if (JobPhase.RUNNING == phase) {
            return "in_progress";
        }
        int processed = 0;
        int failed = 0;
        for (final DigitizeDocument document : _job.getDocuments()) {
            if (!document.isDuplicate()) {
                processed++;
                if (document.isFailing()) {
                    failed++;
                }
            }
        }
        if (0 < processed && failed == processed) {
            return "failed";
        }
        return 0 < failed ? "completed_with_errors" : "completed";
    }

    private static boolean isActive(final String _jobStatus) {
        return "accepted".equals(_jobStatus) || "in_progress".equals(_jobStatus);
    }

    /**
     * Derives a document's status from its job's progress.
     *
     * @param _document the document
     * @return the document status
     */
    private String documentStatus(final DigitizeDocument _document) {
        if (_document.isFrozen()) {
            return _document.getFrozenStatus();
        }
        if (_document.isDuplicate()) {
            return _document.getDuplicateStatus();
        }
        final DigitizeJob job = m_jobs.get(_document.getJobId());
        final String jobStatus = jobStatus(job);
        if ("cancel_pending".equals(jobStatus)) {
            return "in_progress";
        }
        if ("cancelled".equals(jobStatus)) {
            return "cancelled";
        }
        final JobPhase phase = phaseOf(job.getSubmittedAt());
        if (JobPhase.QUEUED == phase) {
            return "accepted";
        }
        if (JobPhase.RUNNING == phase) {
            if (!"ingestion".equals(_document.getOperation())) {
                return "in_progress";
            }
            final double progress = JobPhase.progress(job.getSubmittedAt(), jobDuration(), System.currentTimeMillis());
            if (progress < 0.45) {
                return "in_progress";
            }
            return progress < 0.65 ? "digitized" : progress < 0.85 ? "processed" : "chunked";
        }
        return _document.isFailing() ? "failed" : "completed";
    }

    /**
     * Returns when a job finished.
     *
     * @param _job the job
     * @return epoch milliseconds, or {@code null} while unfinished
     */
    private Long jobCompletedAt(final DigitizeJob _job) {
        if (null != _job.getImportedStatus()) {
            return _job.getImportedCompletedAt();
        }
        final String status = jobStatus(_job);
        if ("cancelled".equals(status)) {
            return Long.valueOf(_job.getCancelRequestedAt() + CANCEL_SETTLE_MILLIS);
        }
        return isActive(status) || "cancel_pending".equals(status) ? null : Long.valueOf(completionTime(_job.getSubmittedAt()));
    }

    /**
     * Returns when a document finished.
     *
     * @param _document the document
     * @return epoch milliseconds, or {@code null} while unfinished
     */
    private Long documentCompletedAt(final DigitizeDocument _document) {
        if (_document.isFrozen()) {
            return _document.getFrozenCompletedAt();
        }
        final String status = documentStatus(_document);
        if ("completed".equals(status) || "failed".equals(status) || _document.isDuplicate()) {
            return Long.valueOf(_document.isDuplicate() ? _document.getSubmittedAt() : completionTime(_document.getSubmittedAt()));
        }
        return "cancelled".equals(status) ? jobCompletedAt(m_jobs.get(_document.getJobId())) : null;
    }

    private String documentError(final DigitizeDocument _document) {
        if (_document.isFrozen()) {
            return _document.getFrozenError();
        }
        return "failed".equals(documentStatus(_document)) ? "Conversion failed: the document could not be parsed" : null;
    }

    private static String timestamp(final Long _epochMillis) {
        return null == _epochMillis ? null : Times.isoMicrosZ(_epochMillis.longValue());
    }

    /**
     * Builds the processing metadata reported for a document with {@code details=true} and in exports.
     *
     * @param _document the document
     * @return the metadata
     */
    private Map<String, Object> documentMetadata(final DigitizeDocument _document) {
        if (null != _document.getFrozenMetadata()) {
            return _document.getFrozenMetadata();
        }
        if (!"completed".equals(documentStatus(_document))) {
            return Json.obj();
        }
        final double seconds = jobDuration() / 1000.0;
        final Map<String, Object> timing = Json.obj("digitizing", Double.valueOf(round(seconds * 0.5)));
        if ("ingestion".equals(_document.getOperation())) {
            timing.put("processing", Double.valueOf(round(seconds * 0.2)));
            timing.put("chunking", Double.valueOf(round(seconds * 0.1)));
            timing.put("indexing", Double.valueOf(round(seconds * 0.2)));
        }
        return Json.obj("pages", Integer.valueOf(_document.getPages()), "tables", Integer.valueOf(_document.getPages() / 4), "timing_in_secs", timing);
    }

    private static double round(final double _value) {
        return Math.round(_value * 100.0) / 100.0;
    }

    // ---------------------------------------------------------------- jobs

    /**
     * Handles {@code POST /v1/jobs}: stages uploaded files and starts a simulated job.
     *
     * @param _request the multipart request
     * @return {@code 202} with the job ID
     */
    private Response createJob(final Request _request) {
        final String operation = null == _request.getQuery("operation") ? "ingestion" : FastApi.queryEnum(_request, "operation", OPERATIONS);
        final String outputFormat = null == _request.getQuery("output_format") ? "json" : FastApi.queryEnum(_request, "output_format", OUTPUT_FORMATS);
        final String jobName = _request.getQuery("job_name");
        if (!_request.hasContentType("multipart/form-data")) {
            throw FastApi.missing("body", "files");
        }
        final List<MultipartForm.Part> files = _request.getMultipart().getFiles("files");
        if (files.isEmpty()) {
            throw FastApi.missing("body", "files");
        }
        final String prefix = "Failed to create job";
        if ("digitization".equals(operation) && 1 < files.size()) {
            throw prefixed(ErrorCode.INVALID_REQUEST, prefix, "Only 1 file allowed for digitization.");
        }
        for (final MultipartForm.Part file : files) {
            final String extension = UploadedText.extension(file.getFilename());
            if (!ACCEPTED_EXTENSIONS.contains(extension)) {
                throw prefixed(ErrorCode.UNSUPPORTED_MEDIA_TYPE, prefix, "Unsupported file type '" + extension + "' for '" + file.getFilename() + "'. Supported types: PDF, DOCX, HTML, MD, TXT.");
            }
        }

        synchronized (m_lock) {
            final int activeTasks = countActiveTasks(operation);
            final int quota = "ingestion".equals(operation) ? INGESTION_QUOTA : DIGITIZATION_QUOTA;
            if (activeTasks + files.size() > quota) {
                throw prefixed(ErrorCode.RATE_LIMIT_EXCEEDED, prefix, "The " + operation + " conversion queue is full (" + activeTasks + "/" + quota + " slots used). Please retry later.");
            }
            final DigitizeJob job = new DigitizeJob(Ids.uuid(), jobName, operation, outputFormat, System.currentTimeMillis());
            final StringBuilder duplicates = new StringBuilder();
            int fresh = 0;
            for (final MultipartForm.Part file : files) {
                final DigitizeDocument document = stageDocument(job, file);
                if (document.isDuplicate()) {
                    duplicates.append(0 == duplicates.length() ? "" : ", ").append('\'').append(file.getFilename()).append("' (").append(document.getDuplicateMessage()).append(')');
                } else {
                    fresh++;
                }
                job.getDocuments().add(document);
            }
            if (0 == fresh) {
                throw prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "No job was created because all submitted files are duplicates. Already processed: " + duplicates + ". Delete the existing documents first to re-process.");
            }
            for (final DigitizeDocument document : job.getDocuments()) {
                if ("already_exists".equals(document.getDuplicateStatus())) {
                    recordDuplicateName(document);
                }
            }
            m_jobs.put(job.getId(), job);
            for (final DigitizeDocument document : job.getDocuments()) {
                m_documents.put(document.getId(), document);
            }
            return Response.json(202, Json.obj("job_id", job.getId()));
        }
    }

    /**
     * Counts conversion tasks still occupying queue slots for an operation.
     *
     * @param _operation the operation
     * @return the number of active tasks
     */
    private int countActiveTasks(final String _operation) {
        int active = 0;
        for (final DigitizeDocument document : m_documents.values()) {
            if (_operation.equals(document.getOperation()) && !document.isDuplicate() && ACTIVE_DOC_STATUSES.contains(documentStatus(document))) {
                active++;
            }
        }
        return active;
    }

    /**
     * Creates the document record for an uploaded file, detecting duplicates of already ingested content.
     *
     * @param _job the new job
     * @param _file the uploaded file
     * @return the document, marked as a duplicate where applicable
     */
    private DigitizeDocument stageDocument(final DigitizeJob _job, final MultipartForm.Part _file) {
        final String hash = sha256(_file.getData());
        final String name = _file.getFilename();
        final int pages = Math.max(1, _file.getData().length / 3000);
        final boolean isFailing = requestsFailure(name);
        if ("ingestion".equals(_job.getOperation())) {
            for (final DigitizeDocument existing : m_documents.values()) {
                if (existing.isDuplicate() || !hash.equals(existing.getContentHash()) || !"ingestion".equals(existing.getOperation())) {
                    continue;
                }
                final String status = documentStatus(existing);
                if ("completed".equals(status)) {
                    return new DigitizeDocument(Ids.uuid(), _job, name, hash, "", pages, false, "already_exists", "already exists: ingested as " + existing.getName());
                }
                if (ACTIVE_DOC_STATUSES.contains(status)) {
                    return new DigitizeDocument(Ids.uuid(), _job, name, hash, "", pages, false, "skipped", "skipped: duplicate of in progress " + existing.getName());
                }
            }
        }
        return new DigitizeDocument(Ids.uuid(), _job, name, hash, UploadedText.extract(_file), pages, isFailing, null, null);
    }

    /**
     * Adds a duplicate's file name to the completed original it duplicates.
     *
     * @param _duplicate the duplicate document of a newly committed job
     */
    private void recordDuplicateName(final DigitizeDocument _duplicate) {
        for (final DigitizeDocument existing : m_documents.values()) {
            if (!existing.isDuplicate() && _duplicate.getContentHash().equals(existing.getContentHash()) && "completed".equals(documentStatus(existing))) {
                existing.getDuplicateNames().add(_duplicate.getName());
                return;
            }
        }
    }

    /**
     * Hashes content for duplicate detection.
     *
     * @param _data the content
     * @return the lower-case hex digest
     */
    private static String sha256(final byte[] _data) {
        try {
            final byte[] digest = MessageDigest.getInstance("SHA-256").digest(_data);
            final StringBuilder hex = new StringBuilder();
            for (final byte value : digest) {
                hex.append(String.format("%02x", Integer.valueOf(value & 0xff)));
            }
            return hex.toString();
        } catch (final NoSuchAlgorithmException e) {
            // SHA-256 support is mandatory on every Java platform.
            throw new IllegalStateException(e);
        }
    }

    /**
     * Builds the job representation shared by the list and detail endpoints.
     *
     * @param _job the job
     * @return the job object
     */
    private Map<String, Object> jobView(final DigitizeJob _job) {
        final List<Map<String, Object>> documents = new ArrayList<Map<String, Object>>();
        int completed = 0;
        int failed = 0;
        int inProgress = 0;
        for (final DigitizeDocument document : _job.getDocuments()) {
            final String status = documentStatus(document);
            if ("completed".equals(status)) {
                completed++;
            } else if ("failed".equals(status)) {
                failed++;
            } else if (ACTIVE_DOC_STATUSES.contains(status)) {
                inProgress++;
            }
            documents.add(Json.obj("id", document.getId(), "name", document.getName(), "status", status, "message", document.getDuplicateMessage()));
        }
        final String status = jobStatus(_job);
        String error = _job.getImportedError();
        if (null == error && "failed".equals(status)) {
            error = "All documents failed processing";
        } else if (null == error && "completed_with_errors".equals(status)) {
            error = failed + " of " + (completed + failed) + " documents failed processing";
        }
        final Map<String, Object> stats = Json.obj("total_documents", Integer.valueOf(_job.getDocuments().size()), "completed", Integer.valueOf(completed), "failed", Integer.valueOf(failed), "in_progress", Integer.valueOf(inProgress));
        return Json.obj("job_id", _job.getId(), "job_name", _job.getName(), "operation", _job.getOperation(), "source", "user", "status", status, "submitted_at", Times.isoMicrosZ(_job.getSubmittedAt()), "completed_at", timestamp(jobCompletedAt(_job)), "documents", documents, "stats", stats, "error", error);
    }

    /**
     * Handles {@code GET /v1/jobs}.
     *
     * @param _request the request
     * @return a page of jobs, newest first
     */
    private Response listJobs(final Request _request) {
        final boolean latest = FastApi.queryBool(_request, "latest", false);
        final int limit = FastApi.queryInt(_request, "limit", 20, Integer.valueOf(1), Integer.valueOf(100));
        final int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        final String status = FastApi.queryEnum(_request, "status", JOB_STATUSES);
        final String operation = FastApi.queryEnum(_request, "operation", OPERATIONS);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_lock) {
            final List<DigitizeJob> jobs = new ArrayList<DigitizeJob>(m_jobs.values());
            jobs.sort((_a, _b) -> Long.compare(_b.getSubmittedAt(), _a.getSubmittedAt()));
            for (final DigitizeJob job : jobs) {
                if ((null == status || status.equals(jobStatus(job))) && (null == operation || operation.equals(job.getOperation()))) {
                    items.add(jobView(job));
                }
            }
        }
        if (latest) {
            final List<Map<String, Object>> newest = items.isEmpty() ? items : Pages.slice(items, 0, 1);
            return Response.json(200, Pages.offsetEnvelope(newest.size(), limit, offset, newest));
        }
        return Response.json(200, Pages.offsetEnvelope(items.size(), limit, offset, Pages.slice(items, offset, limit)));
    }

    /**
     * Resolves a job, failing with a 404 whose message carries the operation prefix.
     *
     * @param _jobId the job ID
     * @param _prefix the operation prefix, or {@code null} for an unprefixed message
     * @return the job
     */
    private DigitizeJob requireJob(final String _jobId, final String _prefix) {
        final DigitizeJob job = m_jobs.get(_jobId);
        if (null == job) {
            if (null == _prefix) {
                throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "No job found with id '" + _jobId + "'");
            }
            throw prefixed(ErrorCode.RESOURCE_NOT_FOUND, _prefix, "No job found with id '" + _jobId + "'");
        }
        return job;
    }

    private Response getJob(final Request _request) {
        final String jobId = _request.getPathParam("job_id");
        synchronized (m_lock) {
            return Response.json(200, jobView(requireJob(jobId, "Failed to get job '" + jobId + "'")));
        }
    }

    /**
     * Handles {@code DELETE /v1/jobs/{job_id}}. The job's documents are kept, with their state frozen.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteJob(final Request _request) {
        final String jobId = _request.getPathParam("job_id");
        final String prefix = "Failed to delete job '" + jobId + "'";
        synchronized (m_lock) {
            final DigitizeJob job = requireJob(jobId, prefix);
            if (isActive(jobStatus(job))) {
                throw prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Job '" + jobId + "' is still active and cannot be deleted");
            }
            for (final DigitizeDocument document : job.getDocuments()) {
                if (!document.isFrozen()) {
                    document.freeze(documentStatus(document), documentCompletedAt(document), documentError(document), documentMetadata(document));
                }
            }
            m_jobs.remove(jobId);
        }
        return Response.empty(204);
    }

    /**
     * Handles {@code POST /v1/jobs/{job_id}/cancel}.
     *
     * @param _request the request
     * @return {@code 202} with no body
     */
    private Response cancelJob(final Request _request) {
        FastApi.queryBool(_request, "clean_files", false);
        final String jobId = _request.getPathParam("job_id");
        synchronized (m_lock) {
            final DigitizeJob job = requireJob(jobId, null);
            final String status = jobStatus(job);
            if (!isActive(status)) {
                throw FastApi.error(ErrorCode.RESOURCE_LOCKED, "Job '" + jobId + "' is already in terminal state '" + status + "' and cannot be cancelled");
            }
            job.requestCancel(System.currentTimeMillis());
        }
        return Response.empty(202);
    }

    // ---------------------------------------------------------------- admin

    /**
     * Lists jobs that are accepted or in progress, which block admin operations.
     *
     * @return the active job IDs
     */
    private List<String> activeJobIds() {
        final List<String> active = new ArrayList<String>();
        for (final DigitizeJob job : m_jobs.values()) {
            if (isActive(jobStatus(job))) {
                active.add(job.getId());
            }
        }
        return active;
    }

    /**
     * Handles {@code GET /v1/export}.
     *
     * @param _request the request
     * @return the exported job and document records
     */
    private Response exportMetadata(final Request _request) {
        final long started = System.nanoTime();
        final String prefix = "Failed to export metadata";
        final int limit = FastApi.queryInt(_request, "limit", 10000, null, null);
        final int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        if (0 == limit || -1 > limit) {
            throw prefixed(ErrorCode.INVALID_REQUEST, prefix, "limit must be -1 or a positive integer");
        }
        final List<Map<String, Object>> jobs = new ArrayList<Map<String, Object>>();
        final List<Map<String, Object>> documents = new ArrayList<Map<String, Object>>();
        final int totalRecords;
        synchronized (m_lock) {
            final List<String> active = activeJobIds();
            if (!active.isEmpty()) {
                throw prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Cannot export while jobs are active. Active jobs: " + String.join(", ", active));
            }
            final List<Object> records = new ArrayList<Object>();
            records.addAll(m_jobs.values());
            records.addAll(m_documents.values());
            totalRecords = records.size();
            final List<Object> window = -1 == limit ? records : Pages.slice(records, offset, limit);
            for (final Object record : window) {
                if (record instanceof DigitizeJob) {
                    jobs.add(exportJob((DigitizeJob) record));
                } else {
                    documents.add(exportDocument((DigitizeDocument) record));
                }
            }
        }
        final int returned = jobs.size() + documents.size();
        final int effectiveOffset = -1 == limit ? 0 : offset;
        final Map<String, Object> summary = Json.obj("jobs", exportSummary(jobs), "documents", exportSummary(documents));
        final Map<String, Object> pagination = Json.obj("limit", Integer.valueOf(-1 == limit ? returned : limit), "offset", Integer.valueOf(effectiveOffset), "has_more",
                Boolean.valueOf(effectiveOffset + returned < totalRecords), "total_records", Integer.valueOf(totalRecords), "returned_records", Integer.valueOf(returned));
        return Response.json(200, Json.obj("status", "completed", "data", Json.obj("jobs", jobs, "documents", documents), "summary", summary, "export_timestamp", Times.isoMicrosZ(System.currentTimeMillis()), "duration_seconds", Double.valueOf((System.nanoTime() - started) / 1.0e9), "pagination", pagination));
    }

    /**
     * Counts exported records by status.
     *
     * @param _records the exported records
     * @return the summary block
     */
    private static Map<String, Object> exportSummary(final List<Map<String, Object>> _records) {
        int completed = 0;
        int failed = 0;
        for (final Map<String, Object> record : _records) {
            if ("completed".equals(record.get("status"))) {
                completed++;
            } else if ("failed".equals(record.get("status"))) {
                failed++;
            }
        }
        return Json.obj("total_exported", Integer.valueOf(_records.size()), "completed", Integer.valueOf(completed), "failed", Integer.valueOf(failed));
    }

    private Map<String, Object> exportJob(final DigitizeJob _job) {
        final Map<String, Object> view = jobView(_job);
        return Json.obj("job_id", _job.getId(), "operation", _job.getOperation(), "status", view.get("status"), "source", "user", "job_name", _job.getName(), "submitted_at", view.get("submitted_at"), "completed_at", view.get("completed_at"), "stats", view.get("stats"), "error", view.get("error"));
    }

    private Map<String, Object> exportDocument(final DigitizeDocument _document) {
        return Json.obj("id", _document.getId(), "job_id", _document.getJobId(), "name", _document.getName(), "type", _document.getOperation(), "status",
                documentStatus(_document), "source", "user", "output_format", _document.getOutputFormat(), "submitted_at", Times.isoMicrosZ(_document.getSubmittedAt()),
                "completed_at", timestamp(documentCompletedAt(_document)), "error", documentError(_document), "metadata", documentMetadata(_document));
    }

    /**
     * Handles {@code POST /v1/import}: restores exported job and document records.
     *
     * @param _request the request
     * @return the import report
     */
    @SuppressWarnings("unchecked")
    private Response importMetadata(final Request _request) {
        final long started = System.nanoTime();
        final JsonBody body = _request.getJson();
        final Map<String, Object> data = body.getMap("data");
        if (null == data) {
            throw FastApi.missing("body", "data");
        }
        final List<Object> jobRecords = data.get("jobs") instanceof List ? (List<Object>) data.get("jobs") : new ArrayList<Object>();
        final List<Object> documentRecords = data.get("documents") instanceof List ? (List<Object>) data.get("documents") : new ArrayList<Object>();
        if (jobRecords.isEmpty() && documentRecords.isEmpty()) {
            throw FastApi.validation("value_error", Json.list("body", "data"), "Value error, At least one job or document record must be provided", data);
        }
        final boolean validateOnly = body.getBoolean("validate_only", false);
        final List<Object> errors = new ArrayList<Object>();
        final List<Object> warnings = new ArrayList<Object>();
        final int[] jobCounts = new int[3];
        final int[] documentCounts = new int[3];
        synchronized (m_lock) {
            final List<String> active = activeJobIds();
            if (!active.isEmpty()) {
                throw prefixed(ErrorCode.RESOURCE_LOCKED, "Failed to import metadata", "Cannot import while jobs are active. Active jobs: " + String.join(", ", active));
            }
            final Map<String, DigitizeJob> imported = new LinkedHashMap<String, DigitizeJob>();
            for (final Object record : jobRecords) {
                importJob(record, imported, errors, jobCounts);
            }
            for (final Object record : documentRecords) {
                importDocument(record, imported, errors, warnings, documentCounts);
            }
            if (!validateOnly) {
                for (final DigitizeJob job : imported.values()) {
                    m_jobs.put(job.getId(), job);
                    for (final DigitizeDocument document : job.getDocuments()) {
                        m_documents.put(document.getId(), document);
                    }
                }
            }
        }
        final Map<String, Object> summary = Json.obj("jobs", importSummary(jobRecords.size(), jobCounts), "documents", importSummary(documentRecords.size(), documentCounts));
        return Response.json(200, Json.obj("status", "completed", "summary", summary, "duration_seconds", Double.valueOf((System.nanoTime() - started) / 1.0e9), "errors", errors, "warnings", warnings));
    }

    private static Map<String, Object> importSummary(final int _received, final int[] _counts) {
        return Json.obj("total_received", Integer.valueOf(_received), "imported", Integer.valueOf(_counts[0]), "skipped", Integer.valueOf(_counts[1]), "failed", Integer.valueOf(_counts[2]));
    }

    private static Map<String, Object> issue(final String _recordType, final Object _recordId, final String _type, final String _message) {
        return Json.obj("record_type", _recordType, "record_id", null == _recordId ? null : String.valueOf(_recordId), "type", _type, "message", _message);
    }

    /**
     * Validates and stages one imported job record.
     *
     * @param _record the raw record
     * @param _imported staged jobs by ID
     * @param _errors collects validation errors
     * @param _counts imported, skipped and failed counters
     */
    @SuppressWarnings("unchecked")
    private void importJob(final Object _record, final Map<String, DigitizeJob> _imported, final List<Object> _errors, final int[] _counts) {
        final JsonBody record = new JsonBody(_record instanceof Map ? (Map<String, Object>) _record : null);
        final String id = record.getString("job_id", null);
        final String operation = record.getString("operation", null);
        final String status = record.getString("status", null);
        if (null == id || !OPERATIONS.contains(operation) || !JOB_STATUSES.contains(status)) {
            _errors.add(issue("job", id, "validation_error", "job_id, a valid operation and a valid status are required"));
            _counts[2]++;
            return;
        }
        if (m_jobs.containsKey(id) || _imported.containsKey(id)) {
            _counts[1]++;
            return;
        }
        try {
            final DigitizeJob job = new DigitizeJob(id, record.getString("job_name", null), operation, "json", parseTime(record.getString("submitted_at", null)).longValue());
            final String completedAt = record.getString("completed_at", null);
            job.setImported(status, null == completedAt ? null : parseTime(completedAt), record.getString("error", null));
            _imported.put(id, job);
            _counts[0]++;
        } catch (final IllegalArgumentException e) {
            _errors.add(issue("job", id, "validation_error", e.getMessage()));
            _counts[2]++;
        }
    }

    /**
     * Validates and stages one imported document record, attaching it to its job.
     *
     * @param _record the raw record
     * @param _imported staged jobs by ID
     * @param _errors collects validation errors
     * @param _warnings collects warnings such as orphaned documents
     * @param _counts imported, skipped and failed counters
     */
    @SuppressWarnings("unchecked")
    private void importDocument(final Object _record, final Map<String, DigitizeJob> _imported, final List<Object> _errors, final List<Object> _warnings, final int[] _counts) {
        final JsonBody record = new JsonBody(_record instanceof Map ? (Map<String, Object>) _record : null);
        final String id = record.getString("id", null);
        final String name = record.getString("name", null);
        final String status = record.getString("status", null);
        if (null == id || null == name || !DOC_STATUSES.contains(status)) {
            _errors.add(issue("document", id, "validation_error", "id, name and a valid status are required"));
            _counts[2]++;
            return;
        }
        if (m_documents.containsKey(id)) {
            _counts[1]++;
            return;
        }
        final String jobId = record.getString("job_id", null);
        final DigitizeJob job = null == jobId ? null : _imported.get(jobId);
        if (null == job) {
            _warnings.add(issue("document", id, "orphaned_document", "Document references unknown job '" + jobId + "'"));
            _counts[2]++;
            return;
        }
        try {
            final String type = OPERATIONS.contains(record.getString("type", null)) ? record.getString("type", null) : job.getOperation();
            final String format = OUTPUT_FORMATS.contains(record.getString("output_format", null)) ? record.getString("output_format", null) : "json";
            final DigitizeJob template = new DigitizeJob(job.getId(), null, type, format, parseTime(record.getString("submitted_at", null)).longValue());
            final DigitizeDocument document = new DigitizeDocument(id, template, name, "", UploadedText.placeholder(name, 6000), 1, false, null, null);
            final String completedAt = record.getString("completed_at", null);
            document.freeze(status, null == completedAt ? null : parseTime(completedAt), record.getString("error", null), record.getMap("metadata"));
            job.getDocuments().add(document);
            _counts[0]++;
        } catch (final IllegalArgumentException e) {
            _errors.add(issue("document", id, "validation_error", e.getMessage()));
            _counts[2]++;
        }
    }

    /**
     * Parses an ISO 8601 timestamp, with or without an offset (naive values are UTC).
     *
     * @param _text the timestamp
     * @return epoch milliseconds
     * @throws IllegalArgumentException if the timestamp is missing or malformed
     */
    private static Long parseTime(final String _text) {
        if (null == _text) {
            throw new IllegalArgumentException("submitted_at is required");
        }
        try {
            return Long.valueOf(OffsetDateTime.parse(_text).toInstant().toEpochMilli());
        } catch (final DateTimeParseException e) {
            try {
                return Long.valueOf(LocalDateTime.parse(_text).toInstant(ZoneOffset.UTC).toEpochMilli());
            } catch (final DateTimeParseException e2) {
                final IllegalArgumentException invalid = new IllegalArgumentException("Invalid timestamp '" + _text + "'");
                invalid.addSuppressed(e);
                invalid.initCause(e2);
                throw invalid;
            }
        }
    }

    /**
     * Handles {@code GET /v1/conversion-tasks}.
     *
     * @param _request the request
     * @return the conversion tasks, optionally filtered by status
     */
    private Response listConversionTasks(final Request _request) {
        final List<String> filter = _request.getQueryValues("status");
        final List<String> invalid = new ArrayList<String>();
        for (final String value : filter) {
            if (!TASK_STATUSES.contains(value)) {
                invalid.add(value);
            }
        }
        if (!invalid.isEmpty()) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "Invalid status value(s): " + String.join(", ", invalid) + ". Valid values: " + String.join(", ", TASK_STATUSES));
        }
        final List<Map<String, Object>> tasks = new ArrayList<Map<String, Object>>();
        synchronized (m_lock) {
            for (final DigitizeJob job : m_jobs.values()) {
                if (null != job.getImportedStatus()) {
                    continue;
                }
                for (final DigitizeDocument document : job.getDocuments()) {
                    if (document.isDuplicate()) {
                        continue;
                    }
                    final Map<String, Object> task = conversionTask(job, document);
                    if (filter.isEmpty() || filter.contains(task.get("status"))) {
                        tasks.add(task);
                    }
                }
            }
        }
        return Response.json(200, tasks);
    }

    /**
     * Builds the conversion task view of one document.
     *
     * @param _job the job
     * @param _document the document
     * @return the task
     */
    private Map<String, Object> conversionTask(final DigitizeJob _job, final DigitizeDocument _document) {
        final String jobStatus = jobStatus(_job);
        final String documentStatus = documentStatus(_document);
        final String status;
        if ("cancel_pending".equals(jobStatus) || "cancelled".equals(jobStatus)) {
            status = jobStatus;
        } else if ("accepted".equals(documentStatus)) {
            status = "queued";
        } else if ("completed".equals(documentStatus) || "failed".equals(documentStatus)) {
            status = documentStatus;
        } else {
            status = "running";
        }
        final boolean isStarted = !"queued".equals(status);
        final boolean isFinished = "completed".equals(status) || "failed".equals(status) || "cancelled".equals(status);
        final long startedAt = _job.getSubmittedAt() + (long) (jobDuration() * 0.15);
        final Long completedAt = documentCompletedAt(_document);
        final String taskId = UUID.nameUUIDFromBytes(("task:" + _document.getId()).getBytes(StandardCharsets.UTF_8)).toString();
        return Json.obj("task_id", taskId, "job_id", _job.getId(), "doc_id", _document.getId(), "connector_id", null, "operation", _job.getOperation(), "cached_file",
                "/var/cache/staging/" + _job.getId() + "/" + _document.getName(), "output_format", _job.getOutputFormat(), "page_count", Integer.valueOf(_document.getPages()),
                "is_large", Boolean.valueOf(100 < _document.getPages()), "status", status, "result_path",
                "completed".equals(status) ? "/var/cache/digitized/" + _document.getId() + "." + _job.getOutputFormat() : null, "error", documentError(_document), "queued_at",
                Times.isoMicrosOffset(_job.getSubmittedAt()), "started_at", isStarted ? Times.isoMicrosOffset(startedAt) : null, "completed_at",
                isFinished && null != completedAt ? Times.isoMicrosOffset(completedAt.longValue()) : null);
    }

    // ------------------------------------------------------------ documents

    /**
     * Handles {@code GET /v1/documents}.
     *
     * @param _request the request
     * @return a page of document summaries, newest first
     */
    private Response listDocuments(final Request _request) {
        final int limit = FastApi.queryInt(_request, "limit", 20, Integer.valueOf(1), Integer.valueOf(100));
        final int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        final String status = null == _request.getQuery("status") ? null : _request.getQuery("status").toLowerCase(Locale.ROOT);
        if (null != status && !DOC_STATUSES.contains(status)) {
            throw prefixed(ErrorCode.INVALID_REQUEST, "Failed to list documents", "Invalid status '" + _request.getQuery("status") + "'. Must be one of: " + String.join(", ", DOC_STATUSES));
        }
        final String name = null == _request.getQuery("name") ? null : _request.getQuery("name").toLowerCase(Locale.ROOT);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_lock) {
            final List<DigitizeDocument> documents = new ArrayList<DigitizeDocument>(m_documents.values());
            Collections.reverse(documents);
            documents.sort((_a, _b) -> Long.compare(_b.getSubmittedAt(), _a.getSubmittedAt()));
            for (final DigitizeDocument document : documents) {
                final String documentStatus = documentStatus(document);
                if ((null == status || status.equals(documentStatus)) && (null == name || document.getName().toLowerCase(Locale.ROOT).contains(name))) {
                    items.add(Json.obj("id", document.getId(), "name", document.getName(), "type", document.getOperation(), "status", documentStatus, "submitted_at", Times.isoMicrosZ(document.getSubmittedAt())));
                }
            }
        }
        return Response.json(200, Pages.offsetEnvelope(items.size(), limit, offset, Pages.slice(items, offset, limit)));
    }

    /**
     * Resolves a document, failing with the unprefixed 404 the real service returns.
     *
     * @param _docId the document ID
     * @return the document
     */
    private DigitizeDocument requireDocument(final String _docId) {
        final DigitizeDocument document = m_documents.get(_docId);
        if (null == document) {
            throw FastApi.rawError(404, "RESOURCE_NOT_FOUND", "Document with ID '" + _docId + "' not found");
        }
        return document;
    }

    /**
     * Handles {@code GET /v1/documents/{doc_id}}.
     *
     * @param _request the request
     * @return the document details
     */
    private Response getDocument(final Request _request) {
        final boolean details = FastApi.queryBool(_request, "details", false);
        synchronized (m_lock) {
            final DigitizeDocument document = requireDocument(_request.getPathParam("doc_id"));
            return Response.json(200, Json.obj("id", document.getId(), "job_id", document.getJobId(), "name", document.getName(), "type", document.getOperation(), "status",
                    documentStatus(document), "output_format", document.getOutputFormat(), "submitted_at", Times.isoMicrosZ(document.getSubmittedAt()), "completed_at",
                    timestamp(documentCompletedAt(document)), "error", documentError(document), "metadata", details ? documentMetadata(document) : null, "duplicate_names",
                    new ArrayList<String>(document.getDuplicateNames())));
        }
    }

    /**
     * Handles {@code GET /v1/documents/{doc_id}/content}.
     *
     * @param _request the request
     * @return the converted content in the job's output format
     */
    private Response getDocumentContent(final Request _request) {
        synchronized (m_lock) {
            final DigitizeDocument document = requireDocument(_request.getPathParam("doc_id"));
            if (document.isDuplicate() || !"completed".equals(documentStatus(document))) {
                throw FastApi.rawError(404, "RESOURCE_NOT_FOUND", "Content file for document '" + document.getId() + "' not found");
            }
            final String format = document.getOutputFormat();
            final Object result;
            if ("md".equals(format)) {
                result = "# " + UploadedText.stem(document.getName()) + "\n\n" + document.getText() + "\n";
            } else if ("txt".equals(format)) {
                result = document.getText();
            } else {
                result = doclingJson(document);
            }
            return Response.json(200, Json.obj("result", result, "output_format", format));
        }
    }

    /**
     * Builds a small document in the shape of Docling's JSON export.
     *
     * @param _document the document
     * @return the JSON document
     */
    private static Map<String, Object> doclingJson(final DigitizeDocument _document) {
        final List<Object> texts = new ArrayList<Object>();
        final List<Object> bodyChildren = new ArrayList<Object>();
        final String[] paragraphs = _document.getText().split("(?<=\\.)\\s+");
        for (int i = 0; i < paragraphs.length && i < 50; i++) {
            final String reference = "#/texts/" + i;
            bodyChildren.add(Json.obj("$ref", reference));
            final Map<String, Object> provenance = Json.obj("page_no", Integer.valueOf(1 + i * _document.getPages() / Math.max(1, paragraphs.length)), "bbox", Json.obj("l",
                    Double.valueOf(72.0), "t", Double.valueOf(720.0 - (i % 20) * 30.0), "r", Double.valueOf(540.0), "b", Double.valueOf(700.0 - (i % 20) * 30.0), "coord_origin",
                    "BOTTOMLEFT"), "charspan", Json.list(Integer.valueOf(0), Integer.valueOf(paragraphs[i].length())));
            texts.add(Json.obj("self_ref", reference, "parent", Json.obj("$ref", "#/body"), "children", Json.list(), "content_layer", "body", "label", 0 == i ? "title" : "text", "prov", Json.list(provenance), "orig", paragraphs[i], "text", paragraphs[i]));
        }
        final Map<String, Object> pages = new LinkedHashMap<String, Object>();
        for (int page = 1; page <= _document.getPages(); page++) {
            pages.put(String.valueOf(page), Json.obj("size", Json.obj("width", Double.valueOf(612.0), "height", Double.valueOf(792.0)), "page_no", Integer.valueOf(page)));
        }
        return Json.obj("schema_name", "DoclingDocument", "version", "1.5.0", "name", UploadedText.stem(_document.getName()), "origin", Json.obj("mimetype",
                mimeType(_document.getName()), "binary_hash", Long.valueOf(Math.abs((long) _document.getContentHash().hashCode())), "filename", _document.getName()), "body",
                Json.obj("self_ref", "#/body", "children", bodyChildren, "content_layer", "body", "name", "_root_", "label", "unspecified"), "texts", texts, "tables", Json.list(),
                "pictures", Json.list(), "pages", pages);
    }

    /**
     * Maps a file name to the MIME type Docling records.
     *
     * @param _filename the file name
     * @return the MIME type
     */
    private static String mimeType(final String _filename) {
        final String extension = UploadedText.extension(_filename);
        if (".pdf".equals(extension)) {
            return "application/pdf";
        }
        if (".docx".equals(extension)) {
            return "application/vnd.openxmlformats-officedocument.wordprocessingml.document";
        }
        if (".html".equals(extension)) {
            return "text/html";
        }
        return ".md".equals(extension) ? "text/markdown" : "text/plain";
    }

    /**
     * Handles {@code DELETE /v1/documents/{doc_id}}. Like the real service, an unknown ID still returns 204.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteDocument(final Request _request) {
        final String docId = _request.getPathParam("doc_id");
        synchronized (m_lock) {
            final DigitizeDocument document = m_documents.get(docId);
            if (null == document) {
                return Response.empty(204);
            }
            final DigitizeJob job = m_jobs.get(document.getJobId());
            if (null != job && isActive(jobStatus(job))) {
                throw prefixed(ErrorCode.RESOURCE_LOCKED, "Failed to delete document '" + docId + "'", "Document part of active job '" + job.getId() + "' and cannot be deleted");
            }
            if (null != job) {
                job.getDocuments().remove(document);
            }
            m_documents.remove(docId);
        }
        return Response.empty(204);
    }

    /**
     * Handles {@code DELETE /v1/documents?confirm=true}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteAllDocuments(final Request _request) {
        if (!_request.hasQuery("confirm")) {
            throw FastApi.missing("query", "confirm");
        }
        final String prefix = "Failed to delete documents";
        if (!FastApi.queryBool(_request, "confirm", false)) {
            throw prefixed(ErrorCode.INVALID_REQUEST, prefix, "Bulk deletion requires explicit confirmation. Set 'confirm=true' to proceed.");
        }
        synchronized (m_lock) {
            final List<String> active = activeJobIds();
            if (!active.isEmpty()) {
                throw prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Cannot delete documents while jobs are active. Active jobs: " + String.join(", ", active));
            }
            for (final DigitizeDocument document : m_documents.values()) {
                final DigitizeJob job = m_jobs.get(document.getJobId());
                if (null != job) {
                    job.getDocuments().remove(document);
                }
            }
            m_documents.clear();
        }
        return Response.empty(204);
    }
}
