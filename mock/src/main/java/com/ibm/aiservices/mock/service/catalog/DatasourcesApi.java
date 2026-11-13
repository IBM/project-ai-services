package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Pattern;

import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.MockService;
import com.ibm.aiservices.mock.service.common.Pages;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;
import com.ibm.aiservices.mock.util.Times;

/**
 * Mock of the catalog server's {@code /api/v1/datasources} routes.
 * <p>
 * Secret parameters are stored but never returned. A datasource whose name contains {@code fail} fails its simulated connection test.
 */
final class DatasourcesApi {

    private static final Pattern NAME_PATTERN = Pattern.compile("^[A-Za-z0-9_-]+$");
    private static final List<String> PROVIDERS = Arrays.asList("object_storage", "file_system");
    private static final List<String> SECRET_PARAMS = Arrays.asList("secret_access_key", "private_key", "api_key", "password");
    private static final List<String> AUTH_PARAMS = Arrays.asList("access_key_id", "secret_access_key", "username", "private_key");

    private final CatalogState m_state;
    private final CatalogAuth m_auth;

    DatasourcesApi(final CatalogState _state, final CatalogAuth _auth) {
        m_state = _state;
        m_auth = _auth;
    }

    /**
     * Registers the datasource routes.
     *
     * @param _router the catalog router
     */
    void register(final Router _router) {
        _router.post("/api/v1/datasources", m_auth.authed(this::create));
        _router.get("/api/v1/datasources", m_auth.authed(this::list));
        _router.get("/api/v1/datasources/{id}", m_auth.authed(this::get));
        _router.get("/api/v1/datasources/{id}/applications", m_auth.authed(this::applications));
        _router.put("/api/v1/datasources/{id}", m_auth.authed(this::update));
        _router.delete("/api/v1/datasources/{id}", m_auth.authed(this::delete));
    }

    private static Map<String, Object> providerRef(final String _providerId) {
        return Json.obj("id", _providerId, "name", "object_storage".equals(_providerId) ? "Object Storage" : "File System");
    }

    /**
     * Handles {@code POST /api/v1/datasources}.
     *
     * @param _request the request
     * @return {@code 201} with the datasource ID
     */
    private Response create(final Request _request) {
        final JsonBody body = _request.getJson();
        final String name = body.getString("name", "");
        if (name.isEmpty()) {
            throw Gin.bindingError("CreateDatasourceRequest", "Name", "required");
        }
        if (3 > name.length() || 100 < name.length()) {
            throw Gin.bindingError("CreateDatasourceRequest", "Name", 3 > name.length() ? "min" : "max");
        }
        final String providerId = body.getString("provider_id", "");
        if (providerId.isEmpty()) {
            throw Gin.bindingError("CreateDatasourceRequest", "ProviderID", "required");
        }
        final Map<String, Object> params = body.getMap("params");
        if (null == params) {
            throw Gin.bindingError("CreateDatasourceRequest", "Params", "required");
        }
        if (!NAME_PATTERN.matcher(name).matches()) {
            throw Gin.error(400, "datasource name may only contain letters, digits, '-' and '_'");
        }
        if (!PROVIDERS.contains(providerId)) {
            throw Gin.error(404, "provider \"" + providerId + "\" not found");
        }
        if (MockService.requestsFailure(name)) {
            throw Gin.error(422, "connection test failed: unable to reach datasource endpoint (simulated)");
        }
        synchronized (m_state) {
            for (final CatalogState.Datasource existing : m_state.datasources().values()) {
                if (existing.getName().equalsIgnoreCase(name)) {
                    throw Gin.error(409, "datasource with name '" + name + "' already exists");
                }
            }
            final CatalogState.Datasource datasource = new CatalogState.Datasource(name, providerId, m_auth.userOf(_request), new LinkedHashMap<String, Object>(params));
            m_state.datasources().put(datasource.getId(), datasource);
            return Response.json(201, Json.obj("id", datasource.getId()));
        }
    }

    /**
     * Handles {@code GET /api/v1/datasources}.
     *
     * @param _request the request
     * @return a page of datasources
     */
    private Response list(final Request _request) {
        final Gin.PageRequest page = Gin.page(_request);
        final String status = _request.getQuery("status");
        if (null != status && !status.isEmpty() && !"connected".equals(status) && !"offline".equals(status)) {
            throw Gin.error(400, "status must be 'connected' or 'offline'");
        }
        final String provider = _request.getQuery("provider");
        final String query = null == _request.getQuery("q") ? null : _request.getQuery("q").toLowerCase(Locale.ROOT);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_state) {
            for (final CatalogState.Datasource datasource : m_state.datasources().values()) {
                if ("offline".equals(status) || (null != provider && !provider.isEmpty() && !provider.equals(datasource.getProviderId())) || (null != query && !datasource.getName().toLowerCase(Locale.ROOT).contains(query))) {
                    continue;
                }
                items.add(Json.obj("id", datasource.getId(), "name", datasource.getName(), "type", "datasource", "provider", providerRef(datasource.getProviderId()), "status",
                        "connected", "connected_services", Integer.valueOf(m_state.applicationsUsing(datasource.getId()).size()), "created_at",
                        Times.rfc3339(datasource.getCreatedAt()), "updated_at", Times.rfc3339(datasource.getUpdatedAt())));
            }
        }
        return Response.json(200, Json.obj("data", Pages.slice(items, page.getOffset(), page.getPageSize()), "pagination", Pages.pageInfo(page.getPage(), page.getPageSize(), items.size())));
    }

    /**
     * Resolves the {@code :id} datasource.
     *
     * @param _request the request
     * @return the datasource
     */
    private CatalogState.Datasource requireDatasource(final Request _request) {
        final String id = _request.getPathParam("id");
        if (!Ids.isUuid(id)) {
            throw Gin.error(400, "Invalid datasource ID format");
        }
        final CatalogState.Datasource datasource = m_state.datasources().get(id);
        if (null == datasource) {
            throw Gin.error(404, "datasource not found");
        }
        return datasource;
    }

    /**
     * Lists the applications connected to a datasource, with their sync status.
     *
     * @param _datasource the datasource
     * @return the connected application items
     */
    private List<Object> connectedApplications(final CatalogState.Datasource _datasource) {
        final List<Object> applications = new ArrayList<Object>();
        for (final CatalogState.Application application : m_state.applicationsUsing(_datasource.getId())) {
            final long connectedAt = application.getDatasources().get(_datasource.getId()).longValue();
            final boolean isSynced = System.currentTimeMillis() - connectedAt >= m_state.getConfig().getJobDurationMillis();
            applications.add(Json.obj("id", application.getId(), "name", application.getName(), "catalog_id", application.getCatalogId(), "type", application.getType(),
                    "sync_status", isSynced ? "completed" : "syncing", "last_sync_at", isSynced ? Times.rfc3339(connectedAt + m_state.getConfig().getJobDurationMillis()) : null));
        }
        return applications;
    }

    /**
     * Handles {@code GET /api/v1/datasources/:id}, with secrets removed from the metadata.
     *
     * @param _request the request
     * @return the datasource details
     */
    private Response get(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Datasource datasource = requireDatasource(_request);
            final Map<String, Object> metadata = new LinkedHashMap<String, Object>();
            for (final Map.Entry<String, Object> param : datasource.getParams().entrySet()) {
                if (!SECRET_PARAMS.contains(param.getKey())) {
                    metadata.put(param.getKey(), param.getValue());
                }
            }
            return Response.json(200, Json.obj("id", datasource.getId(), "name", datasource.getName(), "type", "datasource", "provider", providerRef(datasource.getProviderId()),
                    "status", "connected", "metadata", metadata, "applications", connectedApplications(datasource), "created_at", Times.isoMicrosZ(datasource.getCreatedAt()),
                    "updated_at", Times.isoMicrosZ(datasource.getUpdatedAt())));
        }
    }

    private Response applications(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Datasource datasource = requireDatasource(_request);
            return Response.json(200, Json.obj("datasource_id", datasource.getId(), "applications", connectedApplications(datasource)));
        }
    }

    /**
     * Handles {@code PUT /api/v1/datasources/:id}. Only authentication parameters can be changed.
     *
     * @param _request the request
     * @return the updated datasource
     */
    private Response update(final Request _request) {
        final Map<String, Object> params = _request.getJson().getMap("params");
        if (null == params) {
            throw Gin.bindingError("UpdateDatasourceRequest", "Params", "required");
        }
        if (params.isEmpty()) {
            throw Gin.error(400, "params must not be empty");
        }
        synchronized (m_state) {
            final CatalogState.Datasource datasource = requireDatasource(_request);
            for (final Map.Entry<String, Object> param : params.entrySet()) {
                if (AUTH_PARAMS.contains(param.getKey())) {
                    datasource.getParams().put(param.getKey(), param.getValue());
                }
            }
            datasource.touch();
            return Response.json(200, Json.obj("id", datasource.getId(), "name", datasource.getName(), "type", "datasource", "provider", datasource.getProviderId(), "status",
                    "connected", "message", "Credentials updated", "created_by", datasource.getOwner(), "created_at", Times.isoMicrosZ(datasource.getCreatedAt()), "updated_at",
                    Times.isoMicrosZ(datasource.getUpdatedAt())));
        }
    }

    /**
     * Handles {@code DELETE /api/v1/datasources/:id}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response delete(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Datasource datasource = requireDatasource(_request);
            final int connected = m_state.applicationsUsing(datasource.getId()).size();
            if (0 < connected) {
                throw Gin.error(409, "datasource is still connected to " + connected + " application(s)");
            }
            m_state.datasources().remove(datasource.getId());
        }
        return Response.empty(204);
    }
}
