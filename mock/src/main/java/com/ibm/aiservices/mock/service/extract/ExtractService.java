package com.ibm.aiservices.mock.service.extract;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;
import java.util.regex.Pattern;

import com.google.gson.JsonParseException;
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
 * Mock of the extract information service ({@code services/extract}).
 */
public final class ExtractService extends FastApiService {

    private static final Pattern NAME_PATTERN = Pattern.compile("^[a-zA-Z0-9._-]+$");
    private static final List<String> JOB_STATUSES = Arrays.asList("accepted", "completed", "completed_with_errors", "failed", "in_progress");
    private static final int MAX_FILES = 32;
    private static final int MAX_EXAMPLES = 5;
    private static final int MAX_CUSTOM_PROMPT = 2000;

    private final Map<String, Schema> m_schemas = Collections.synchronizedMap(new LinkedHashMap<String, Schema>());
    private final Map<String, Schema> m_ephemeralSchemas = Collections.synchronizedMap(new LinkedHashMap<String, Schema>());
    private final Map<String, ExtractJob> m_jobs = Collections.synchronizedMap(new LinkedHashMap<String, ExtractJob>());

    /**
     * A registered or ephemeral extraction schema.
     */
    private static final class Schema {

        private final String m_id;
        private final String m_name;
        private final String m_description;
        private final Map<String, Object> m_jsonSchema;
        private final boolean m_isInferred;
        private final List<Object> m_examples;
        private final String m_customPrompt;
        private final long m_createdAt = System.currentTimeMillis();

        /**
         * Creates a schema record.
         *
         * @param _id the schema ID
         * @param _name the schema name
         * @param _description the description, or {@code null}
         * @param _jsonSchema the JSON Schema
         * @param _isInferred whether the schema was inferred from examples
         * @param _examples the few-shot examples
         * @param _customPrompt the custom prompt, or {@code null}
         */
        Schema(final String _id, final String _name, final String _description, final Map<String, Object> _jsonSchema, final boolean _isInferred, final List<Object> _examples, final String _customPrompt) {
            m_id = _id;
            m_name = _name;
            m_description = _description;
            m_jsonSchema = _jsonSchema;
            m_isInferred = _isInferred;
            m_examples = _examples;
            m_customPrompt = _customPrompt;
        }

        int schemaTokens() {
            return MockText.tokenCount(Json.toJson(m_jsonSchema).replaceAll("[{}\":,\\[\\]]", " "));
        }

        int examplesTokens() {
            return m_examples.isEmpty() ? 0 : MockText.tokenCount(Json.toJson(m_examples).replaceAll("[{}\":,\\[\\]]", " "));
        }

        int customPromptTokens() {
            return MockText.tokenCount(m_customPrompt);
        }
    }

    /**
     * One file within an extraction job.
     */
    private static final class ExtractDocument {

        private final String m_id = Ids.uuid();
        private final String m_filename;
        private final String m_text;
        private final boolean m_isFailing;

        ExtractDocument(final String _filename, final String _text, final boolean _isFailing) {
            m_filename = _filename;
            m_text = _text;
            m_isFailing = _isFailing;
        }
    }

    /**
     * State of one submitted extraction job.
     */
    private static final class ExtractJob {

        private final String m_id = Ids.uuid();
        private final long m_submittedAt = System.currentTimeMillis();
        private final String m_name;
        private final Schema m_schema;
        private final List<ExtractDocument> m_documents;

        ExtractJob(final String _name, final Schema _schema, final List<ExtractDocument> _documents) {
            m_name = _name;
            m_schema = _schema;
            m_documents = _documents;
        }
    }

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public ExtractService(final MockConfig _config) {
        super(_config);
    }

    @Override
    public String getName() {
        return "extract";
    }

    @Override
    public String getTitle() {
        return "Extract Information API";
    }

    /**
     * Returns the default port. The real service also uses 6000, which collides with summarize, so the mock defaults to 6001.
     *
     * @return the default port
     */
    @Override
    public int getDefaultPort() {
        return 6001;
    }

    @Override
    protected void registerApiRoutes(final Router _router) {
        _router.post("/v1/schemas", this::createSchema);
        _router.get("/v1/schemas", this::listSchemas);
        _router.delete("/v1/schemas", this::deleteAllSchemas);
        _router.get("/v1/schemas/{schema_id}", this::getSchema);
        _router.delete("/v1/schemas/{schema_id}", this::deleteSchema);
        _router.post("/v1/extract", this::extract);
        _router.post("/v1/extract/jobs", this::createJob);
        _router.get("/v1/extract/jobs", this::listJobs);
        _router.delete("/v1/extract/jobs", this::deleteAllJobs);
        _router.get("/v1/extract/jobs/{job_id}", this::getJob);
        _router.delete("/v1/extract/jobs/{job_id}", this::deleteJob);
        _router.get("/v1/extract/jobs/{job_id}/results/{doc_id}", this::getResult);
        _router.get("/v1/extract/jobs/{job_id}/results/{doc_id}/download", this::downloadResult);
    }

    // ---------------------------------------------------------------- schemas

    /**
     * Handles {@code POST /v1/schemas}.
     *
     * @param _request the request
     * @return {@code 201} with the stored schema summary
     */
    private Response createSchema(final Request _request) {
        final JsonBody body = _request.getJson();
        if (!body.has("name")) {
            throw FastApi.missing("body", "name");
        }
        final String name = body.getString("name", "");
        if (name.isEmpty() || 200 < name.length() || !NAME_PATTERN.matcher(name).matches()) {
            throw new ApiException(FastApi.validationResponse("string_pattern_mismatch", Json.list("body", "name"), "String should match pattern '^[a-zA-Z0-9._-]+$'", name, Json.obj("pattern", "^[a-zA-Z0-9._-]+$")));
        }
        final List<Object> examples = null == body.getList("examples") ? new ArrayList<Object>() : body.getList("examples");
        if (MAX_EXAMPLES < examples.size()) {
            throw FastApi.validation("too_long", Json.list("body", "examples"), "List should have at most 5 items after validation, not " + examples.size(), examples);
        }
        final String customPrompt = body.getString("custom_prompt", null);
        if (null != customPrompt && MAX_CUSTOM_PROMPT < customPrompt.length()) {
            throw FastApi.validation("string_too_long", Json.list("body", "custom_prompt"), "String should have at most 2000 characters", customPrompt);
        }
        Map<String, Object> jsonSchema = body.getMap("json_schema");
        final boolean isInferred = null == jsonSchema;
        if (isInferred) {
            jsonSchema = inferFromExamples(examples);
        }
        validateSchema(jsonSchema);
        synchronized (m_schemas) {
            for (final Schema existing : m_schemas.values()) {
                if (existing.m_name.equals(name)) {
                    throw FastApi.error(ErrorCode.RESOURCE_CONFLICT, "A schema named '" + name + "' already exists.");
                }
            }
            final Schema schema = new Schema(Ids.uuid(), name, body.getString("description", null), jsonSchema, isInferred, examples, customPrompt);
            m_schemas.put(schema.m_id, schema);
            return Response.json(201, Json.obj("schema_id", schema.m_id, "name", schema.m_name, "description", schema.m_description, "created_at", Times.isoMicrosZ(schema.m_createdAt)));
        }
    }

    /**
     * Infers a schema from the first example output.
     *
     * @param _examples the examples
     * @return the inferred schema
     */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> inferFromExamples(final List<Object> _examples) {
        if (_examples.isEmpty()) {
            throw FastApi.error(ErrorCode.MISSING_INPUT, "Provide either 'json_schema' or at least one entry in 'examples'.");
        }
        final Object first = _examples.get(0);
        if (!(first instanceof Map) || !(((Map<String, Object>) first).get("output") instanceof Map)) {
            throw FastApi.validation("missing", Json.list("body", "examples", Integer.valueOf(0), "output"), "Field required", first);
        }
        return SchemaSupport.infer((Map<String, Object>) ((Map<String, Object>) first).get("output"));
    }

    /**
     * Rejects schemas whose root is not an object schema.
     *
     * @param _schema the schema
     */
    private static void validateSchema(final Map<String, Object> _schema) {
        if (!"object".equals(_schema.get("type"))) {
            throw FastApi.error(ErrorCode.INVALID_SCHEMA, "The root of 'json_schema' must have \"type\": \"object\".");
        }
        if (null != _schema.get("properties") && !(_schema.get("properties") instanceof Map)) {
            throw FastApi.error(ErrorCode.INVALID_SCHEMA, "'properties' must be an object.");
        }
    }

    /**
     * Handles {@code GET /v1/schemas}.
     *
     * @param _request the request
     * @return a page of schema summaries
     */
    private Response listSchemas(final Request _request) {
        final int limit = FastApi.queryInt(_request, "limit", 20, Integer.valueOf(1), Integer.valueOf(100));
        final int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        final String nameFilter = null == _request.getQuery("name") ? null : _request.getQuery("name").toLowerCase(Locale.ROOT);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_schemas) {
            for (final Schema schema : m_schemas.values()) {
                if (null == nameFilter || schema.m_name.toLowerCase(Locale.ROOT).contains(nameFilter)) {
                    items.add(Json.obj("schema_id", schema.m_id, "name", schema.m_name, "description", schema.m_description, "example_count",
                            Integer.valueOf(schema.m_examples.size()), "schema_tokens", Integer.valueOf(schema.schemaTokens()), "examples_tokens",
                            Integer.valueOf(schema.examplesTokens()), "custom_prompt_tokens", Integer.valueOf(schema.customPromptTokens()), "created_at",
                            Times.isoMicrosZ(schema.m_createdAt)));
                }
            }
        }
        return Response.json(200, Pages.offsetEnvelope(items.size(), limit, offset, Pages.slice(items, offset, limit)));
    }

    /**
     * Resolves a registered schema.
     *
     * @param _schemaId the schema ID
     * @return the schema
     */
    private Schema requireSchema(final String _schemaId) {
        final Schema schema = m_schemas.get(_schemaId);
        if (null == schema) {
            throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "No schema with id '" + _schemaId + "'.");
        }
        return schema;
    }

    /**
     * Handles {@code GET /v1/schemas/{schema_id}}.
     *
     * @param _request the request
     * @return the full schema
     */
    private Response getSchema(final Request _request) {
        final Schema schema = requireSchema(_request.getPathParam("schema_id"));
        return Response.json(200, Json.obj("schema_id", schema.m_id, "name", schema.m_name, "description", schema.m_description, "is_schema_inferred",
                Boolean.valueOf(schema.m_isInferred), "json_schema", schema.m_jsonSchema, "examples", schema.m_examples, "custom_prompt", schema.m_customPrompt, "schema_tokens",
                Integer.valueOf(schema.schemaTokens()), "examples_tokens", Integer.valueOf(schema.examplesTokens()), "custom_prompt_tokens",
                Integer.valueOf(schema.customPromptTokens()), "created_at", Times.isoMicrosZ(schema.m_createdAt)));
    }

    /**
     * Handles {@code DELETE /v1/schemas/{schema_id}}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteSchema(final Request _request) {
        final Schema schema = requireSchema(_request.getPathParam("schema_id"));
        final List<Object> referencing = new ArrayList<Object>();
        synchronized (m_jobs) {
            for (final ExtractJob job : m_jobs.values()) {
                if (job.m_schema == schema && referencing.size() < 10) {
                    referencing.add(job.m_id);
                }
            }
        }
        if (!referencing.isEmpty()) {
            throw new ApiException(FastApi.envelope(409, "RESOURCE_LOCKED", FastApi.message(ErrorCode.RESOURCE_LOCKED, "Schema '" + schema.m_id + "' is referenced by existing jobs."), Json.obj("referencing_job_ids", referencing)));
        }
        m_schemas.remove(schema.m_id);
        return Response.empty(204);
    }

    /**
     * Handles {@code DELETE /v1/schemas?confirm=true}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteAllSchemas(final Request _request) {
        if (!"true".equals(_request.getQuery("confirm"))) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "Bulk delete requires ?confirm=true.");
        }
        if (!m_jobs.isEmpty()) {
            throw FastApi.error(ErrorCode.RESOURCE_LOCKED, "Cannot delete schemas while extraction jobs exist. Delete the jobs first.");
        }
        m_schemas.clear();
        return Response.empty(204);
    }

    // ------------------------------------------------------- schema resolution

    /**
     * Resolves the schema to use from the four alternative inputs, in the real service's priority order.
     *
     * @param _schemaId the registered schema ID, or {@code null}
     * @param _schemaName the registered schema name, or {@code null}
     * @param _jsonSchema an inline schema, or {@code null}
     * @param _jsonExample an inline example output, or {@code null}
     * @return the schema; inline inputs produce an unregistered ephemeral schema
     */
    private Schema resolveSchema(final String _schemaId, final String _schemaName, final Map<String, Object> _jsonSchema, final Map<String, Object> _jsonExample) {
        if (null != _schemaId) {
            final Schema schema = requireSchema(_schemaId);
            if (null != _schemaName && !_schemaName.equals(schema.m_name)) {
                throw FastApi.error(ErrorCode.INVALID_REQUEST, "schema_id '" + _schemaId + "' does not match schema_name '" + _schemaName + "'.");
            }
            return schema;
        }
        if (null != _schemaName) {
            synchronized (m_schemas) {
                for (final Schema schema : m_schemas.values()) {
                    if (schema.m_name.equals(_schemaName)) {
                        return schema;
                    }
                }
            }
            throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "No schema named '" + _schemaName + "'.");
        }
        if (null != _jsonSchema) {
            validateSchema(_jsonSchema);
            return ephemeral(_jsonSchema, false);
        }
        if (null != _jsonExample) {
            if (_jsonExample.isEmpty()) {
                throw FastApi.error(ErrorCode.INVALID_REQUEST, "'json_example' must be a non-empty object.");
            }
            return ephemeral(SchemaSupport.infer(_jsonExample), true);
        }
        throw FastApi.error(ErrorCode.INVALID_REQUEST, "Provide one of 'schema_id', 'schema_name', 'json_schema' or 'json_example'.");
    }

    private Schema ephemeral(final Map<String, Object> _jsonSchema, final boolean _isInferred) {
        final Schema schema = new Schema("ephemeral-" + Ids.uuid(), "ephemeral", null, _jsonSchema, _isInferred, new ArrayList<Object>(), null);
        m_ephemeralSchemas.put(schema.m_id, schema);
        return schema;
    }

    private static String publicSchemaId(final Schema _schema) {
        return _schema.m_id.startsWith("ephemeral-") ? null : _schema.m_id;
    }

    // -------------------------------------------------------- sync extraction

    /**
     * Handles {@code POST /v1/extract}.
     *
     * @param _request the request
     * @return the extraction result
     */
    private Response extract(final Request _request) {
        final long started = System.currentTimeMillis();
        final JsonBody body = _request.getJson();
        if (!body.has("text")) {
            throw FastApi.missing("body", "text");
        }
        final String text = body.getString("text", "");
        if (text.trim().isEmpty()) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "'text' must not be empty.");
        }
        final Schema schema = resolveSchema(body.getString("schema_id", null), body.getString("schema_name", null), body.getMap("json_schema"), body.getMap("json_example"));
        final Map<String, Object> extraction = SchemaSupport.extract(schema.m_jsonSchema, text);
        final int inputTokens = MockText.tokenCount(text);
        final int promptTokens = inputTokens + schema.schemaTokens() + schema.examplesTokens() + 400;
        final int outputTokens = MockText.tokenCount(Json.toJson(extraction).replaceAll("[{}\":,]", " "));
        final Map<String, Object> data = Json.obj("extraction", extraction, "schema_id", publicSchemaId(schema), "source", Json.obj("input_type", "text", "input_tokens", Integer.valueOf(inputTokens)));
        final Map<String, Object> meta = Json.obj("model", MockText.MODEL, "processing_time_ms", Long.valueOf(System.currentTimeMillis() - started), "validation_attempts", Integer.valueOf(1));
        return Response.json(200, Json.obj("data", data, "meta", meta, "usage", Json.obj("input_tokens", Integer.valueOf(promptTokens), "output_tokens", Integer.valueOf(outputTokens), "total_tokens", Integer.valueOf(promptTokens + outputTokens))));
    }

    // ------------------------------------------------------------------- jobs

    /**
     * Handles {@code POST /v1/extract/jobs}.
     *
     * @param _request the multipart request
     * @return {@code 202} with the job ID and file count
     */
    private Response createJob(final Request _request) {
        if (!_request.hasContentType("multipart/form-data")) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "At least one file must be uploaded in the 'files' field.");
        }
        final MultipartForm form = _request.getMultipart();
        final List<MultipartForm.Part> files = form.getFiles("files");
        if (files.isEmpty()) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "At least one file must be uploaded in the 'files' field.");
        }
        if (MAX_FILES < files.size()) {
            throw new ApiException(FastApi.envelope(413, "REQUEST_TOO_LARGE", FastApi.message(ErrorCode.REQUEST_TOO_LARGE, "Too many files: " + files.size() + " submitted, limit is " + MAX_FILES + "."), Json.obj("submitted", Integer.valueOf(files.size()), "limit", Integer.valueOf(MAX_FILES))));
        }
        final Set<String> names = new HashSet<String>();
        final List<ExtractDocument> documents = new ArrayList<ExtractDocument>();
        for (int i = 0; i < files.size(); i++) {
            final MultipartForm.Part file = files.get(i);
            final String extension = UploadedText.extension(file.getFilename());
            if (!".txt".equals(extension) && !".md".equals(extension)) {
                throw FastApi.error(ErrorCode.UNSUPPORTED_FILE_TYPE, "Only .txt and .md files are accepted. File at index " + i + " ('" + file.getFilename() + "') has extension: " + (extension.isEmpty() ? "(none)" : extension));
            }
            if (!names.add(file.getFilename())) {
                throw FastApi.error(ErrorCode.DUPLICATE_FILE, "Duplicate file name '" + file.getFilename() + "' at index " + i + ".");
            }
            final String text = new String(file.getData(), StandardCharsets.UTF_8);
            if (text.trim().isEmpty()) {
                throw new ApiException(FastApi.envelope(415, "INVALID_FILE_CONTENT", FastApi.message(ErrorCode.INVALID_FILE_CONTENT, "1 file(s) have invalid content."), Json.list(Json.obj("index", Integer.valueOf(i), "filename", file.getFilename(), "reason", "File is empty."))));
            }
            documents.add(new ExtractDocument(file.getFilename(), text, requestsFailure(file.getFilename())));
        }
        final Schema schema = resolveSchema(blankToNull(form.getField("schema_id")), blankToNull(form.getField("schema_name")), parseFormJson(form, "json_schema"), parseFormJson(form, "json_example"));
        final ExtractJob job = new ExtractJob(blankToNull(form.getField("job_name")), schema, documents);
        m_jobs.put(job.m_id, job);
        return Response.json(202, Json.obj("job_id", job.m_id, "file_count", Integer.valueOf(documents.size())));
    }

    private static String blankToNull(final String _value) {
        return null == _value || _value.trim().isEmpty() ? null : _value.trim();
    }

    /**
     * Parses an optional JSON object sent as a multipart form field.
     *
     * @param _form the form
     * @param _field the field name
     * @return the object, or {@code null} if the field is blank
     */
    private static Map<String, Object> parseFormJson(final MultipartForm _form, final String _field) {
        final String raw = blankToNull(_form.getField(_field));
        if (null == raw) {
            return null;
        }
        try {
            return Json.parseObject(raw);
        } catch (final JsonParseException e) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "'" + _field + "' is not valid JSON: " + e.getMessage());
        }
    }

    /**
     * Derives a document's status from its job's simulated progress.
     *
     * @param _job the job
     * @param _document the document
     * @return the document status
     */
    private String documentStatus(final ExtractJob _job, final ExtractDocument _document) {
        final JobPhase phase = phaseOf(_job.m_submittedAt);
        if (JobPhase.QUEUED == phase) {
            return "pending";
        }
        if (JobPhase.RUNNING == phase) {
            return "in_progress";
        }
        return _document.m_isFailing ? "failed" : "completed";
    }

    /**
     * Derives the job status from its documents.
     *
     * @param _job the job
     * @return the job status
     */
    private String jobStatus(final ExtractJob _job) {
        final JobPhase phase = phaseOf(_job.m_submittedAt);
        if (JobPhase.QUEUED == phase) {
            return "accepted";
        }
        if (JobPhase.RUNNING == phase) {
            return "in_progress";
        }
        final int failed = failedCount(_job);
        if (0 == failed) {
            return "completed";
        }
        return failed == _job.m_documents.size() ? "failed" : "completed_with_errors";
    }

    /**
     * Counts the documents of a job that will fail.
     *
     * @param _job the job
     * @return the number of failing documents
     */
    private static int failedCount(final ExtractJob _job) {
        int failed = 0;
        for (final ExtractDocument document : _job.m_documents) {
            if (document.m_isFailing) {
                failed++;
            }
        }
        return failed;
    }

    private String completedAt(final ExtractJob _job) {
        // The real service returns an empty string rather than null while a job is unfinished.
        return JobPhase.DONE == phaseOf(_job.m_submittedAt) ? Times.isoMicrosZ(completionTime(_job.m_submittedAt)) : "";
    }

    /**
     * Handles {@code GET /v1/extract/jobs}.
     *
     * @param _request the request
     * @return a page of job summaries, newest first
     */
    private Response listJobs(final Request _request) {
        final boolean latest = FastApi.queryBool(_request, "latest", false);
        int limit = FastApi.queryInt(_request, "limit", 20, Integer.valueOf(1), Integer.valueOf(100));
        int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        final String status = _request.getQuery("status");
        if (null != status && !JOB_STATUSES.contains(status)) {
            throw FastApi.error(ErrorCode.INVALID_PARAMETER, "Invalid status value. Must be one of: accepted, completed, completed_with_errors, failed, in_progress");
        }
        final String schemaId = _request.getQuery("schema_id");
        final String schemaName = _request.getQuery("schema_name");
        if (latest) {
            limit = 1;
            offset = 0;
        }
        final List<ExtractJob> jobs;
        synchronized (m_jobs) {
            jobs = new ArrayList<ExtractJob>(m_jobs.values());
        }
        Collections.reverse(jobs);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        for (final ExtractJob job : jobs) {
            final String jobStatus = jobStatus(job);
            if ((null != status && !status.equals(jobStatus)) || (null != schemaId && !schemaId.equals(job.m_schema.m_id)) || (null != schemaName && !schemaName.equals(job.m_schema.m_name))) {
                continue;
            }
            items.add(Json.obj("job_id", job.m_id, "job_name", job.m_name, "schema_id", job.m_schema.m_id, "status", jobStatus, "file_count", Integer.valueOf(job.m_documents.size()), "submitted_at", Times.isoMicrosZ(job.m_submittedAt), "completed_at", completedAt(job)));
        }
        return Response.json(200, Pages.offsetEnvelope(items.size(), limit, offset, Pages.slice(items, offset, limit)));
    }

    /**
     * Resolves the {@code job_id} job.
     *
     * @param _request the request
     * @return the job
     */
    private ExtractJob requireJob(final Request _request) {
        final String jobId = _request.getPathParam("job_id");
        final ExtractJob job = m_jobs.get(jobId);
        if (null == job) {
            throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "Job '" + jobId + "' not found.");
        }
        return job;
    }

    /**
     * Resolves a document within a job.
     *
     * @param _job the job
     * @param _docId the document ID
     * @return the document
     */
    private static ExtractDocument requireDocument(final ExtractJob _job, final String _docId) {
        for (final ExtractDocument document : _job.m_documents) {
            if (document.m_id.equals(_docId)) {
                return document;
            }
        }
        throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "Document '" + _docId + "' not found in job '" + _job.m_id + "'.");
    }

    /**
     * Handles {@code GET /v1/extract/jobs/{job_id}}.
     *
     * @param _request the request
     * @return the job with per-document status
     */
    private Response getJob(final Request _request) {
        final ExtractJob job = requireJob(_request);
        final List<Map<String, Object>> documents = new ArrayList<Map<String, Object>>();
        int completed = 0;
        int failed = 0;
        for (final ExtractDocument document : job.m_documents) {
            final String status = documentStatus(job, document);
            if ("completed".equals(status)) {
                completed++;
            } else if ("failed".equals(status)) {
                failed++;
            }
            documents.add(Json.obj("doc_id", document.m_id, "filename", document.m_filename, "status", status, "error", "failed".equals(status) ? "LLM_ERROR: Extraction failed for this document." : ""));
        }
        final String status = jobStatus(job);
        String error = null;
        if ("failed".equals(status)) {
            error = "All " + job.m_documents.size() + " file(s) failed extraction.";
        } else if ("completed_with_errors".equals(status)) {
            error = failed + " of " + job.m_documents.size() + " files failed extraction.";
        }
        return Response.json(200, Json.obj("job_id", job.m_id, "job_name", job.m_name, "schema_id", job.m_schema.m_id, "status", status, "document", null, "documents", documents,
                "file_count", Integer.valueOf(job.m_documents.size()), "files_completed", Integer.valueOf(completed), "files_failed", Integer.valueOf(failed), "files_pending",
                Integer.valueOf(job.m_documents.size() - completed - failed), "metadata", null, "submitted_at", Times.isoMicrosZ(job.m_submittedAt), "completed_at",
                completedAt(job), "error", error));
    }

    /**
     * Handles {@code GET /v1/extract/jobs/{job_id}/results/{doc_id}}.
     *
     * @param _request the request
     * @return the extraction result, {@code 202} while processing, or {@code 410} if the document failed
     */
    private Response getResult(final Request _request) {
        final ExtractJob job = requireJob(_request);
        final ExtractDocument document = requireDocument(job, _request.getPathParam("doc_id"));
        final String status = documentStatus(job, document);
        if ("pending".equals(status) || "in_progress".equals(status)) {
            return Response.json(202, Json.obj("message", "Document is still processing.", "job_id", job.m_id, "doc_id", document.m_id, "status", status));
        }
        if ("failed".equals(status)) {
            return Response.json(410, Json.obj("error", Json.obj("code", "DOCUMENT_FAILED", "message", "Document '" + document.m_id + "' failed extraction. Inspect GET /v1/extract/jobs/" + job.m_id + " for details.", "status", Integer.valueOf(410), "job_id", job.m_id, "doc_id", document.m_id)));
        }
        final Map<String, Object> extraction = SchemaSupport.extract(job.m_schema.m_jsonSchema, document.m_text);
        final int inputTokens = MockText.tokenCount(document.m_text);
        final int promptTokens = inputTokens + job.m_schema.schemaTokens() + 400;
        final int outputTokens = MockText.tokenCount(Json.toJson(extraction).replaceAll("[{}\":,]", " "));
        final double seconds = jobDuration() / 1000.0;
        final Map<String, Object> source = Json.obj("input_type", "file", "document_name", document.m_filename, "input_words", Integer.valueOf(MockText.wordCount(document.m_text)), "input_tokens", Integer.valueOf(inputTokens));
        final Map<String, Object> meta = Json.obj("model", MockText.MODEL, "processing_time_ms", Long.valueOf(jobDuration()), "validation_attempts", Integer.valueOf(1), "timing_in_secs", Json.obj("extracting", Double.valueOf(seconds), "validating", Double.valueOf(0.01)));
        return Response.json(200, Json.obj("data", Json.obj("extraction", extraction, "schema_id", job.m_schema.m_id, "source", source), "status", "completed", "meta", meta,
                "usage", Json.obj("input_tokens", Integer.valueOf(promptTokens), "output_tokens", Integer.valueOf(outputTokens), "total_tokens",
                Integer.valueOf(promptTokens + outputTokens))));
    }

    /**
     * Handles {@code GET /v1/extract/jobs/{job_id}/results/{doc_id}/download}.
     *
     * @param _request the request
     * @return the extraction as a downloadable JSON file
     */
    private Response downloadResult(final Request _request) {
        final ExtractJob job = requireJob(_request);
        final ExtractDocument document = requireDocument(job, _request.getPathParam("doc_id"));
        final String status = documentStatus(job, document);
        if ("failed".equals(status)) {
            return Response.json(410, Json.obj("error", Json.obj("code", "DOCUMENT_FAILED", "message", "Document '" + document.m_id + "' failed extraction. Inspect GET /v1/extract/jobs/" + job.m_id + " for details.", "status", Integer.valueOf(410))));
        }
        if (!"completed".equals(status)) {
            throw FastApi.error(ErrorCode.RESOURCE_NOT_FOUND, "No result available for document '" + document.m_id + "' (status='" + status + "').");
        }
        final String body = Json.toPrettyJson(SchemaSupport.extract(job.m_schema.m_jsonSchema, document.m_text));
        return Response.text(200, Response.JSON_CONTENT_TYPE, body).withHeader("Content-Disposition", "attachment; filename=\"" + UploadedText.stem(document.m_filename) + "_result.json\"");
    }

    /**
     * Handles {@code DELETE /v1/extract/jobs/{job_id}}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteJob(final Request _request) {
        final ExtractJob job = requireJob(_request);
        final String status = jobStatus(job);
        if ("accepted".equals(status) || "in_progress".equals(status)) {
            throw FastApi.error(ErrorCode.RESOURCE_LOCKED, "Cannot delete active job '" + job.m_id + "'. Current status: " + status + ".");
        }
        m_jobs.remove(job.m_id);
        m_ephemeralSchemas.remove(job.m_schema.m_id);
        return Response.empty(204);
    }

    /**
     * Handles {@code DELETE /v1/extract/jobs?confirm=true}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response deleteAllJobs(final Request _request) {
        if (!"true".equals(_request.getQuery("confirm"))) {
            throw FastApi.error(ErrorCode.INVALID_REQUEST, "Bulk delete requires ?confirm=true.");
        }
        synchronized (m_jobs) {
            for (final ExtractJob job : m_jobs.values()) {
                final String status = jobStatus(job);
                if ("accepted".equals(status) || "in_progress".equals(status)) {
                    throw FastApi.error(ErrorCode.RESOURCE_LOCKED, "Cannot delete jobs while job '" + job.m_id + "' is " + status + ".");
                }
            }
            m_jobs.clear();
        }
        m_ephemeralSchemas.clear();
        return Response.empty(204);
    }
}
