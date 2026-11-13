package com.ibm.aiservices.mock.service.digitize;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.common.ErrorCode;
import com.ibm.aiservices.mock.service.common.FastApi;
import com.ibm.aiservices.mock.service.common.Pages;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;
import com.ibm.aiservices.mock.util.Times;

/**
 * Mock of the digitize connectors API ({@code /v1/connectors}), which the real service hides from its OpenAPI schema.
 * <p>
 * Syncs are simulated: each one runs for the configured job duration and finds a fixed number of files derived from the connector name.
 */
final class DigitizeConnectors {

    private static final List<String> TYPES = Arrays.asList("file_system", "object_storage");
    private static final List<String> EXTENSIONS = Arrays.asList(".pdf", ".docx", ".html", ".md", ".txt");
    private static final List<String> SSH_FIELDS = Arrays.asList("host", "port", "username", "private_key", "remote_path", "allowed_extensions");
    private static final List<String> S3_FIELDS = Arrays.asList("bucket_name", "access_key_id", "secret_access_key", "endpoint_url", "prefix", "delimiter", "download_concurrency", "verify_ssl", "allowed_extensions");
    private static final List<String> SECRET_FIELDS = Arrays.asList("private_key", "secret_access_key");
    private static final long DEFAULT_SYNC_INTERVAL_SECONDS = 300L;

    private final MockConfig m_config;
    private final Map<String, Connector> m_connectors = new LinkedHashMap<String, Connector>();

    /**
     * One simulated sync run.
     */
    private static final class Sync {

        private final int m_seq;
        private final long m_startedAt = System.currentTimeMillis();
        private final int m_totalFiles;
        private final int m_newFiles;
        private long m_stoppedAt = -1L;

        Sync(final int _seq, final int _totalFiles, final int _newFiles) {
            m_seq = _seq;
            m_totalFiles = _totalFiles;
            m_newFiles = _newFiles;
        }
    }

    /**
     * A registered connector.
     */
    private static final class Connector {

        private final String m_id;
        private final String m_type;
        private final long m_attachedAt = System.currentTimeMillis();
        private final List<Sync> m_syncs = new ArrayList<Sync>();
        private String m_name;
        private List<Object> m_allowedExtensions;
        private Map<String, Object> m_connectionDetails;
        private long m_deleteRequestedAt = -1L;

        /**
         * Creates a connector record.
         *
         * @param _id the connector ID
         * @param _name the connector name
         * @param _type the connector type
         * @param _allowedExtensions the normalized allowed extensions
         * @param _connectionDetails the connection details, including secrets
         */
        Connector(final String _id, final String _name, final String _type, final List<Object> _allowedExtensions, final Map<String, Object> _connectionDetails) {
            m_id = _id;
            m_name = _name;
            m_type = _type;
            m_allowedExtensions = _allowedExtensions;
            m_connectionDetails = _connectionDetails;
        }

        boolean isDeletePending() {
            return 0 <= m_deleteRequestedAt;
        }
    }

    DigitizeConnectors(final MockConfig _config) {
        m_config = _config;
    }

    /**
     * Registers the connector routes.
     *
     * @param _router the digitize router
     */
    void register(final Router _router) {
        _router.post("/v1/connectors", this::create);
        _router.get("/v1/connectors", this::list);
        _router.get("/v1/connectors/{connector_id}", this::get);
        _router.put("/v1/connectors/{connector_id}", this::update);
        _router.delete("/v1/connectors/{connector_id}", this::delete);
        _router.post("/v1/connectors/{connector_id}/syncs", this::startSync);
        _router.get("/v1/connectors/{connector_id}/syncs", this::listSyncs);
        _router.get("/v1/connectors/{connector_id}/syncs/{sync_seq}", this::getSync);
        _router.post("/v1/connectors/{connector_id}/syncs/{sync_seq}/stop", this::stopSync);
    }

    private long syncDuration() {
        return Math.max(1L, m_config.getJobDurationMillis());
    }

    private String syncStatus(final Sync _sync) {
        if (0 <= _sync.m_stoppedAt) {
            return "cancelled";
        }
        return System.currentTimeMillis() - _sync.m_startedAt < syncDuration() ? "started" : "completed";
    }

    private Long syncFinishedAt(final Sync _sync) {
        if (0 <= _sync.m_stoppedAt) {
            return Long.valueOf(_sync.m_stoppedAt);
        }
        return "completed".equals(syncStatus(_sync)) ? Long.valueOf(_sync.m_startedAt + syncDuration()) : null;
    }

    /**
     * Returns the sync currently running, if any.
     *
     * @param _connector the connector
     * @return the running sync, or {@code null}
     */
    private Sync activeSync(final Connector _connector) {
        for (final Sync sync : _connector.m_syncs) {
            if ("started".equals(syncStatus(sync))) {
                return sync;
            }
        }
        return null;
    }

    /**
     * Returns the most recent sync that completed.
     *
     * @param _connector the connector
     * @return the sync, or {@code null}
     */
    private Sync lastCompletedSync(final Connector _connector) {
        for (int i = _connector.m_syncs.size() - 1; i >= 0; i--) {
            if ("completed".equals(syncStatus(_connector.m_syncs.get(i)))) {
                return _connector.m_syncs.get(i);
            }
        }
        return null;
    }

    private String connectorStatus(final Connector _connector) {
        if (_connector.isDeletePending()) {
            return "delete pending";
        }
        return null == activeSync(_connector) ? "up to date" : "syncing";
    }

    /**
     * Removes connectors whose simulated background teardown has finished.
     */
    private void purgeDeleted() {
        final long now = System.currentTimeMillis();
        final Iterator<Connector> iterator = m_connectors.values().iterator();
        while (iterator.hasNext()) {
            final Connector connector = iterator.next();
            if (connector.isDeletePending() && now - connector.m_deleteRequestedAt >= syncDuration()) {
                iterator.remove();
            }
        }
    }

    /**
     * Starts a new sync run, deriving the file counts from the connector name.
     *
     * @param _connector the connector
     * @return the new sync
     */
    private Sync newSync(final Connector _connector) {
        final int totalFiles = 5 + Math.abs(_connector.m_name.hashCode() % 20);
        final Sync previous = lastCompletedSync(_connector);
        final Sync sync = new Sync(_connector.m_syncs.size() + 1, totalFiles, null == previous ? totalFiles : 0);
        _connector.m_syncs.add(sync);
        return sync;
    }

    // ------------------------------------------------------------ validation

    /**
     * Normalizes allowed extensions to lower case with a leading dot, rejecting unsupported ones.
     *
     * @param _raw the raw list, or {@code null} for every supported extension
     * @param _loc the validation error location
     * @return the normalized list
     */
    private static List<Object> normalizeExtensions(final List<Object> _raw, final List<Object> _loc) {
        if (null == _raw) {
            return new ArrayList<Object>(EXTENSIONS);
        }
        final List<Object> normalized = new ArrayList<Object>();
        for (final Object value : _raw) {
            final String text = String.valueOf(value).trim().toLowerCase(Locale.ROOT);
            final String extension = text.startsWith(".") ? text : "." + text;
            if (!EXTENSIONS.contains(extension)) {
                throw FastApi.validation("value_error", _loc, "Value error, Unsupported extension '" + extension + "'. Allowed: " + String.join(", ", EXTENSIONS), _raw);
            }
            if (!normalized.contains(extension)) {
                normalized.add(extension);
            }
        }
        return normalized;
    }

    /**
     * Validates connection details for a connector type and fills in defaults.
     *
     * @param _type the connector type
     * @param _details the raw details
     * @return a validation message, or {@code null} if the details are valid
     */
    private static String validateDetails(final String _type, final Map<String, Object> _details) {
        final boolean isSsh = "file_system".equals(_type);
        final List<String> allowed = isSsh ? SSH_FIELDS : S3_FIELDS;
        for (final String key : _details.keySet()) {
            if (!allowed.contains(key)) {
                return "Extra inputs are not permitted: " + key;
            }
        }
        final List<String> required = isSsh ? Arrays.asList("host", "username", "private_key") : Collections.singletonList("bucket_name");
        for (final String key : required) {
            if (!(_details.get(key) instanceof String) || ((String) _details.get(key)).trim().isEmpty()) {
                return "Field required: " + key;
            }
        }
        if (isSsh) {
            putDefault(_details, "port", Long.valueOf(22L));
            putDefault(_details, "remote_path", "/");
        } else {
            putDefault(_details, "access_key_id", "");
            putDefault(_details, "secret_access_key", "");
            putDefault(_details, "endpoint_url", "");
            putDefault(_details, "prefix", "");
            putDefault(_details, "delimiter", "");
            putDefault(_details, "download_concurrency", Long.valueOf(4L));
            putDefault(_details, "verify_ssl", Boolean.TRUE);
        }
        return null;
    }

    private static void putDefault(final Map<String, Object> _details, final String _key, final Object _value) {
        if (null == _details.get(_key)) {
            _details.put(_key, _value);
        }
    }

    /**
     * Resolves a connector, failing with a prefixed 404.
     *
     * @param _connectorId the connector ID
     * @param _prefix the operation prefix of the error message
     * @return the connector
     */
    private Connector requireConnector(final String _connectorId, final String _prefix) {
        final Connector connector = m_connectors.get(_connectorId);
        if (null == connector) {
            throw DigitizeService.prefixed(ErrorCode.RESOURCE_NOT_FOUND, _prefix, "Connector '" + _connectorId + "' not found");
        }
        return connector;
    }

    /**
     * Returns whether another connector already uses a name.
     *
     * @param _name the name
     * @param _exceptId the connector to ignore, or {@code null}
     * @return {@code true} if the name is in use
     */
    private boolean isNameTaken(final String _name, final String _exceptId) {
        for (final Connector connector : m_connectors.values()) {
            if (connector.m_name.equals(_name) && !connector.m_id.equals(_exceptId)) {
                return true;
            }
        }
        return false;
    }

    // -------------------------------------------------------------- handlers

    /**
     * Handles {@code POST /v1/connectors}: registers a connector and starts its first sync immediately.
     *
     * @param _request the request
     * @return {@code 202} with the connector ID
     */
    private Response create(final Request _request) {
        final JsonBody body = _request.getJson();
        for (final String field : Arrays.asList("name", "type", "connection_details")) {
            if (!body.has(field)) {
                throw FastApi.missing("body", field);
            }
        }
        final String type = body.getString("type", "");
        if (!TYPES.contains(type)) {
            throw FastApi.validation("enum", Json.list("body", "type"), "Input should be 'file_system' or 'object_storage'", body.get("type"));
        }
        final String id = body.getString("id", null);
        if (null != id && !Ids.isUuid(id)) {
            throw FastApi.validation("uuid_parsing", Json.list("body", "id"), "Input should be a valid UUID", id);
        }
        final Map<String, Object> details = body.getMap("connection_details");
        if (null == details) {
            throw FastApi.validation("dict_type", Json.list("body", "connection_details"), "Input should be a valid dictionary", body.get("connection_details"));
        }
        final Map<String, Object> storedDetails = new LinkedHashMap<String, Object>(details);
        final String problem = validateDetails(type, storedDetails);
        if (null != problem) {
            throw FastApi.validation("value_error", Json.list("body", "connection_details"), "Value error, " + problem, details);
        }
        final List<Object> extensions = normalizeExtensions(body.getList("allowed_extensions"), Json.list("body", "allowed_extensions"));
        final String name = body.getString("name", "");
        final String prefix = "Failed to create connector";
        synchronized (m_connectors) {
            purgeDeleted();
            if (null != id && m_connectors.containsKey(id)) {
                throw DigitizeService.prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Connector id '" + id + "' already exists");
            }
            if (isNameTaken(name, null)) {
                throw DigitizeService.prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Connector name '" + name + "' already exists");
            }
            final Connector connector = new Connector(null == id ? Ids.uuid() : id, name, type, extensions, storedDetails);
            m_connectors.put(connector.m_id, connector);
            newSync(connector);
            return Response.json(202, Json.obj("id", connector.m_id));
        }
    }

    /**
     * Handles {@code GET /v1/connectors}.
     *
     * @param _request the request
     * @return a page of connector summaries
     */
    private Response list(final Request _request) {
        final int limit = FastApi.queryInt(_request, "limit", 50, Integer.valueOf(1), Integer.valueOf(200));
        final int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        final String name = null == _request.getQuery("name") ? null : _request.getQuery("name").toLowerCase(Locale.ROOT);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_connectors) {
            purgeDeleted();
            for (final Connector connector : m_connectors.values()) {
                if (null == name || connector.m_name.toLowerCase(Locale.ROOT).contains(name)) {
                    final Sync last = lastCompletedSync(connector);
                    items.add(Json.obj("id", connector.m_id, "name", connector.m_name, "type", connector.m_type, "attached_at", Times.isoMicrosZ(connector.m_attachedAt),
                            "last_sync_at", null == last ? null : Times.isoMicrosZ(last.m_startedAt + syncDuration()), "status", connectorStatus(connector), "total_files",
                            Integer.valueOf(null == last ? 0 : last.m_totalFiles), "message", null));
                }
            }
        }
        return Response.json(200, Json.obj("total", Integer.valueOf(items.size()), "limit", Integer.valueOf(limit), "offset", Integer.valueOf(offset), "items", Pages.slice(items, offset, limit)));
    }

    /**
     * Handles {@code GET /v1/connectors/{connector_id}}. Secret connection fields are never returned.
     *
     * @param _request the request
     * @return the connector details
     */
    private Response get(final Request _request) {
        final String connectorId = _request.getPathParam("connector_id");
        synchronized (m_connectors) {
            purgeDeleted();
            final Connector connector = requireConnector(connectorId, "Failed to get connector '" + connectorId + "'");
            final Map<String, Object> safeDetails = new LinkedHashMap<String, Object>();
            for (final Map.Entry<String, Object> entry : connector.m_connectionDetails.entrySet()) {
                if (!SECRET_FIELDS.contains(entry.getKey())) {
                    safeDetails.put(entry.getKey(), entry.getValue());
                }
            }
            final Sync last = lastCompletedSync(connector);
            return Response.json(200, Json.obj("id", connector.m_id, "name", connector.m_name, "type", connector.m_type, "allowed_extensions", connector.m_allowedExtensions,
                    "sync_interval_seconds", Long.valueOf(DEFAULT_SYNC_INTERVAL_SECONDS), "attached_at", Times.isoMicrosZ(connector.m_attachedAt), "last_sync_at",
                    null == last ? null : Times.isoMicrosZ(last.m_startedAt + syncDuration()), "status", connectorStatus(connector), "connection_details", safeDetails,
                    "total_files", Integer.valueOf(null == last ? 0 : last.m_totalFiles), "message", null));
        }
    }

    /**
     * Handles {@code PUT /v1/connectors/{connector_id}}: renames, changes extensions or merges connection details.
     *
     * @param _request the request
     * @return {@code 200} with no body
     */
    private Response update(final Request _request) {
        final String connectorId = _request.getPathParam("connector_id");
        final JsonBody body = _request.getJson();
        if (body.asMap().isEmpty()) {
            return Response.empty(200);
        }
        final String prefix = "Failed to update connector '" + connectorId + "'";
        final List<Object> extensions = null == body.getList("allowed_extensions") ? null : normalizeExtensions(body.getList("allowed_extensions"), Json.list("body", "allowed_extensions"));
        synchronized (m_connectors) {
            purgeDeleted();
            final Connector connector = requireConnector(connectorId, prefix);
            if (connector.isDeletePending()) {
                throw DigitizeService.prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Connector '" + connectorId + "' is pending deletion");
            }
            final String name = body.getString("name", null);
            if (null != name && isNameTaken(name, connectorId)) {
                throw DigitizeService.prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Connector name '" + name + "' already exists");
            }
            Map<String, Object> details = connector.m_connectionDetails;
            if (null != body.getMap("connection_details")) {
                details = new LinkedHashMap<String, Object>(connector.m_connectionDetails);
                details.putAll(body.getMap("connection_details"));
                final String problem = validateDetails(connector.m_type, details);
                if (null != problem) {
                    throw DigitizeService.prefixed(ErrorCode.INVALID_REQUEST, prefix, "Invalid connection_details: " + problem);
                }
            }
            if (null != name) {
                connector.m_name = name;
            }
            if (null != extensions) {
                connector.m_allowedExtensions = extensions;
            }
            connector.m_connectionDetails = details;
        }
        return Response.empty(200);
    }

    /**
     * Handles {@code DELETE /v1/connectors/{connector_id}}: marks the connector for background teardown.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response delete(final Request _request) {
        final String connectorId = _request.getPathParam("connector_id");
        synchronized (m_connectors) {
            purgeDeleted();
            final Connector connector = requireConnector(connectorId, "Failed to delete connector '" + connectorId + "'");
            if (!connector.isDeletePending()) {
                connector.m_deleteRequestedAt = System.currentTimeMillis();
                final Sync active = activeSync(connector);
                if (null != active) {
                    active.m_stoppedAt = connector.m_deleteRequestedAt;
                }
            }
        }
        return Response.empty(204);
    }

    /**
     * Handles {@code POST /v1/connectors/{connector_id}/syncs}. Idempotent while a sync is running.
     *
     * @param _request the request
     * @return {@code 202} with the sync sequence number
     */
    private Response startSync(final Request _request) {
        final String connectorId = _request.getPathParam("connector_id");
        final String prefix = "Failed to trigger sync for connector '" + connectorId + "'";
        synchronized (m_connectors) {
            purgeDeleted();
            final Connector connector = requireConnector(connectorId, prefix);
            if (connector.isDeletePending()) {
                throw DigitizeService.prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "Connector '" + connectorId + "' is pending deletion");
            }
            final Sync active = activeSync(connector);
            final Sync sync = null == active ? newSync(connector) : active;
            return Response.json(202, Json.obj("sync_seq", Integer.valueOf(sync.m_seq)));
        }
    }

    /**
     * Parses the {@code sync_seq} path parameter.
     *
     * @param _request the request
     * @return the sequence number
     */
    private static int parseSeq(final Request _request) {
        final String raw = _request.getPathParam("sync_seq");
        try {
            return Integer.parseInt(raw);
        } catch (final NumberFormatException e) {
            throw FastApi.validation("int_parsing", Json.list("path", "sync_seq"), "Input should be a valid integer, unable to parse string as an integer", raw);
        }
    }

    /**
     * Handles {@code POST /v1/connectors/{connector_id}/syncs/{sync_seq}/stop}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response stopSync(final Request _request) {
        final String connectorId = _request.getPathParam("connector_id");
        final int seq = parseSeq(_request);
        final String prefix = "Failed to stop sync for connector '" + connectorId + "'";
        synchronized (m_connectors) {
            purgeDeleted();
            final Connector connector = requireConnector(connectorId, prefix);
            final Sync active = activeSync(connector);
            if (null == active) {
                throw DigitizeService.prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "No sync is currently running for this connector.");
            }
            if (active.m_seq != seq) {
                throw DigitizeService.prefixed(ErrorCode.RESOURCE_LOCKED, prefix, "sync_seq " + seq + " is not the active sync for this connector.");
            }
            active.m_stoppedAt = System.currentTimeMillis();
        }
        return Response.empty(204);
    }

    /**
     * Builds the sync log item, with progress derived from elapsed time.
     *
     * @param _sync the sync
     * @return the sync log item
     */
    private Map<String, Object> syncView(final Sync _sync) {
        final String status = syncStatus(_sync);
        final int completed;
        if ("completed".equals(status)) {
            completed = _sync.m_newFiles;
        } else {
            final long end = 0 <= _sync.m_stoppedAt ? _sync.m_stoppedAt : System.currentTimeMillis();
            completed = (int) Math.min(_sync.m_newFiles, _sync.m_newFiles * (end - _sync.m_startedAt) / syncDuration());
        }
        final Long finishedAt = syncFinishedAt(_sync);
        return Json.obj("seq", Integer.valueOf(_sync.m_seq), "started_at", Times.isoMicrosZ(_sync.m_startedAt), "finished_at",
                null == finishedAt ? null : Times.isoMicrosZ(finishedAt.longValue()), "total_files", Integer.valueOf(_sync.m_totalFiles), "new_files",
                Integer.valueOf(_sync.m_newFiles), "completed_files", Integer.valueOf(completed), "removed_files", Integer.valueOf(0), "status", status, "error", "");
    }

    /**
     * Handles {@code GET /v1/connectors/{connector_id}/syncs}.
     *
     * @param _request the request
     * @return a page of sync logs, newest first
     */
    private Response listSyncs(final Request _request) {
        final String connectorId = _request.getPathParam("connector_id");
        final int limit = FastApi.queryInt(_request, "limit", 50, Integer.valueOf(1), Integer.valueOf(200));
        final int offset = FastApi.queryInt(_request, "offset", 0, Integer.valueOf(0), null);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_connectors) {
            purgeDeleted();
            final Connector connector = requireConnector(connectorId, "Failed to list syncs for connector '" + connectorId + "'");
            for (int i = connector.m_syncs.size() - 1; i >= 0; i--) {
                items.add(syncView(connector.m_syncs.get(i)));
            }
        }
        return Response.json(200, Json.obj("total", Integer.valueOf(items.size()), "limit", Integer.valueOf(limit), "offset", Integer.valueOf(offset), "items", Pages.slice(items, offset, limit)));
    }

    /**
     * Handles {@code GET /v1/connectors/{connector_id}/syncs/{sync_seq}}.
     *
     * @param _request the request
     * @return the sync log in a single-item list envelope
     */
    private Response getSync(final Request _request) {
        final String connectorId = _request.getPathParam("connector_id");
        final int seq = parseSeq(_request);
        final String prefix = "Failed to get sync for connector '" + connectorId + "'";
        synchronized (m_connectors) {
            purgeDeleted();
            final Connector connector = requireConnector(connectorId, prefix);
            for (final Sync sync : connector.m_syncs) {
                if (sync.m_seq == seq) {
                    return Response.json(200, Json.obj("total", Integer.valueOf(1), "limit", Integer.valueOf(1), "offset", Integer.valueOf(0), "items", Json.list(syncView(sync))));
                }
            }
            throw DigitizeService.prefixed(ErrorCode.RESOURCE_NOT_FOUND, prefix, "Sync " + seq + " not found for connector '" + connectorId + "'");
        }
    }
}
