package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

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
 * Mock of the catalog server's {@code /api/v1/applications} routes, including application datasources.
 * <p>
 * Endpoints of running services point at the corresponding mock services, so a client can follow an application's endpoint URL straight to a working mock API.
 */
final class ApplicationsApi {

    private static final String CREATE_STRUCT = "CreateApplicationRequest";

    /** Catalog service ID to {mock service name, default mock port}. */
    private static final Map<String, Object[]> SERVICE_ENDPOINTS = new LinkedHashMap<String, Object[]>();

    static {
        SERVICE_ENDPOINTS.put("chat", new Object[] {"chatbot", Integer.valueOf(5000)});
        SERVICE_ENDPOINTS.put("digitize", new Object[] {"digitize", Integer.valueOf(4000)});
        SERVICE_ENDPOINTS.put("extract", new Object[] {"extract", Integer.valueOf(6001)});
        SERVICE_ENDPOINTS.put("similarity", new Object[] {"similarity", Integer.valueOf(7000)});
        SERVICE_ENDPOINTS.put("summarize", new Object[] {"summarize", Integer.valueOf(6000)});
        SERVICE_ENDPOINTS.put("translate", new Object[] {"translate", Integer.valueOf(9000)});
    }

    private final CatalogState m_state;
    private final CatalogAuth m_auth;

    ApplicationsApi(final CatalogState _state, final CatalogAuth _auth) {
        m_state = _state;
        m_auth = _auth;
    }

    /**
     * Registers the application routes. List and create are registered without a trailing slash; the router ignores trailing slashes, so both {@code /applications} and {@code /applications/} work.
     *
     * @param _router the catalog router
     */
    void register(final Router _router) {
        _router.get("/api/v1/applications", m_auth.authed(this::list));
        _router.post("/api/v1/applications", m_auth.authed(this::create));
        _router.get("/api/v1/applications/{id}", m_auth.authed(this::get));
        _router.put("/api/v1/applications/{id}", m_auth.authed(this::update));
        _router.delete("/api/v1/applications/{id}", m_auth.authed(this::delete));
        _router.get("/api/v1/applications/{id}/resources", m_auth.authed(this::resources));
        _router.get("/api/v1/applications/{id}/ps", m_auth.authed(this::ps));
        _router.get("/api/v1/applications/{id}/datasources", m_auth.authed(this::listDatasources));
        _router.put("/api/v1/applications/{id}/datasources", m_auth.authed(this::connectDatasources));
        _router.get("/api/v1/applications/{id}/datasources/{datasource_id}", m_auth.authed(this::getDatasource));
        _router.delete("/api/v1/applications/{id}/datasources/{datasource_id}", m_auth.authed(this::disconnectDatasource));
    }

    // ------------------------------------------------------------- views

    private Map<String, Object> serviceSummary(final CatalogState.Application _application, final CatalogState.AppService _service) {
        return Json.obj("id", _service.getId(), "type", _service.getType(), "status", m_state.applicationStatus(_application), "message", m_state.applicationMessage(_application));
    }

    /**
     * Builds the application object; list views carry only service summaries, detail views full services.
     *
     * @param _application the application
     * @param _isDetail whether to include endpoints and components
     * @return the application object
     */
    private Map<String, Object> view(final CatalogState.Application _application, final boolean _isDetail) {
        final Map<String, Object> view = Json.obj("id", _application.getId(), "name", _application.getName(), "catalog_id", _application.getCatalogId(), "deployment_type",
                _application.getDeploymentType(), "type", _application.getType(), "status", m_state.applicationStatus(_application), "message",
                m_state.applicationMessage(_application), "version", _application.getVersion());
        if (_isDetail || "architectures".equals(_application.getDeploymentType())) {
            final List<Object> services = new ArrayList<Object>();
            for (final CatalogState.AppService service : _application.getServices()) {
                services.add(_isDetail ? serviceDetail(_application, service) : serviceSummary(_application, service));
            }
            view.put("services", services);
        }
        view.put("created_at", Times.rfc3339(_application.getCreatedAt()));
        view.put("updated_at", Times.rfc3339(_application.getUpdatedAt()));
        view.put("worker", _application.getWorker().toRef());
        return view;
    }

    /**
     * Builds the full service object of an application detail view.
     *
     * @param _application the application
     * @param _service the service
     * @return the service object
     */
    private Map<String, Object> serviceDetail(final CatalogState.Application _application, final CatalogState.AppService _service) {
        final String status = m_state.applicationStatus(_application);
        final Map<String, Object> detail = Json.obj("id", _service.getId(), "type", _service.getType(), "catalog_id", _service.getCatalogId(), "status", status, "message",
                m_state.applicationMessage(_application), "version", _service.getVersion(), "created_at", Times.rfc3339(_application.getCreatedAt()), "updated_at",
                Times.rfc3339(_application.getUpdatedAt()));
        final Object[] endpoint = SERVICE_ENDPOINTS.get(_service.getCatalogId());
        if ("Running".equals(status) && null != endpoint) {
            final int port = m_state.getConfig().getPort((String) endpoint[0], ((Integer) endpoint[1]).intValue());
            detail.put("endpoints", Json.list(Json.obj("type", "api", "url", "http://localhost:" + port), Json.obj("host", _application.getName() + "--" + _service.getCatalogId(), "port", String.valueOf(port))));
        }
        final List<Object> components = new ArrayList<Object>();
        for (final CatalogState.Component component : _service.getComponents()) {
            components.add(Json.obj("id", component.getId(), "type", component.getType(), "provider", Json.obj("id", component.getProviderId(), "name", component.getProviderName()), "status", status, "message", "Running".equals(status) ? "" : m_state.applicationMessage(_application)));
        }
        if (!components.isEmpty()) {
            detail.put("components", components);
        }
        return detail;
    }

    /**
     * Resolves the {@code :id} application, enforcing UUID format and ownership.
     *
     * @param _request the request
     * @return the application
     */
    private CatalogState.Application requireApplication(final Request _request) {
        final String id = _request.getPathParam("id");
        if (!Ids.isUuid(id)) {
            throw Gin.error(400, "Invalid application ID format");
        }
        final CatalogState.Application application = m_state.applications().get(id);
        if (null == application) {
            throw Gin.error(404, "application not found");
        }
        if (!application.getOwner().equals(m_auth.userOf(_request))) {
            throw Gin.error(403, "you do not have access to this application");
        }
        return application;
    }

    // ---------------------------------------------------------- handlers

    /**
     * Handles {@code GET /api/v1/applications/}.
     *
     * @param _request the request
     * @return a page of the caller's applications
     */
    private Response list(final Request _request) {
        final Gin.PageRequest page = Gin.page(_request);
        final String deploymentType = _request.getQuery("deployment_type");
        if (null != deploymentType && !deploymentType.isEmpty() && !"architectures".equals(deploymentType) && !"services".equals(deploymentType)) {
            throw Gin.error(400, "deployment_type must be 'architectures' or 'services'");
        }
        final String catalogId = _request.getQuery("catalog_id");
        final String query = null == _request.getQuery("q") ? null : _request.getQuery("q").toLowerCase(Locale.ROOT);
        final String user = m_auth.userOf(_request);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_state) {
            for (final CatalogState.Application application : m_state.applications().values()) {
                if (application.getOwner().equals(user) && matchesFilter(deploymentType, application.getDeploymentType()) && matchesFilter(catalogId, application.getCatalogId())
                        && (null == query || application.getName().toLowerCase(Locale.ROOT).contains(query))) {
                    items.add(view(application, false));
                }
            }
        }
        return Response.json(200, Json.obj("data", Pages.slice(items, page.getOffset(), page.getPageSize()), "pagination", Pages.pageInfo(page.getPage(), page.getPageSize(), items.size())));
    }

    /**
     * Returns whether a value passes an optional exact-match query filter.
     *
     * @param _filter the filter value; {@code null} or empty matches everything
     * @param _value the value to test
     * @return {@code true} if the value matches
     */
    private static boolean matchesFilter(final String _filter, final String _value) {
        return null == _filter || _filter.isEmpty() || _filter.equals(_value);
    }

    /**
     * Handles {@code POST /api/v1/applications/}: validates the request against the catalog and starts a simulated deployment.
     *
     * @param _request the request
     * @return {@code 202} with the application ID
     */
    @SuppressWarnings("unchecked")
    private Response create(final Request _request) {
        final JsonBody body = _request.getJson();
        final String name = body.getString("name", "");
        if (name.isEmpty()) {
            throw Gin.bindingError(CREATE_STRUCT, "Name", "required");
        }
        if (3 > name.length()) {
            throw Gin.bindingError(CREATE_STRUCT, "Name", "min");
        }
        if (100 < name.length()) {
            throw Gin.bindingError(CREATE_STRUCT, "Name", "max");
        }
        final String catalogId = body.getString("catalog_id", "");
        if (catalogId.isEmpty()) {
            throw Gin.bindingError(CREATE_STRUCT, "CatalogID", "required");
        }
        final String version = body.getString("version", "");
        if (version.isEmpty()) {
            throw Gin.bindingError(CREATE_STRUCT, "Version", "required");
        }
        final List<Object> services = body.getList("services");
        if (null == services) {
            throw Gin.bindingError(CREATE_STRUCT, "Services", "required");
        }
        final boolean isArchitecture = "rag".equals(catalogId);
        final CatalogData.Service standalone = CatalogData.service(catalogId);
        if (!isArchitecture && null == standalone) {
            throw Gin.error(422, "catalog item \"" + catalogId + "\" not found in architectures or services");
        }
        final String user = m_auth.userOf(_request);
        synchronized (m_state) {
            final CatalogState.Worker worker = m_state.workerByName(body.getString("worker_name", null));
            if (null == worker) {
                throw Gin.error(422, "worker \"" + body.getString("worker_name", "") + "\" not found");
            }
            for (final CatalogState.Application existing : m_state.applications().values()) {
                if (existing.getName().equals(name) && existing.getOwner().equals(user)) {
                    throw Gin.error(409, "application with name '" + name + "' already exists");
                }
            }
            final CatalogState.Application application = new CatalogState.Application(name, catalogId, isArchitecture ? "architectures" : "services", isArchitecture ? "Digital assistants" : standalone.getName(), version, user, worker, MockService.requestsFailure(name));
            for (int i = 0; i < services.size(); i++) {
                if (!(services.get(i) instanceof Map)) {
                    throw Gin.bindingError(CREATE_STRUCT, "Services[" + i + "]", "required");
                }
                application.getServices().add(buildService(new JsonBody((Map<String, Object>) services.get(i)), i, application));
            }
            if (application.getServices().isEmpty() && !isArchitecture) {
                application.getServices().add(new CatalogState.AppService(catalogId, standalone.getName(), version));
            }
            m_state.applications().put(application.getId(), application);
            return Response.json(202, Json.obj("id", application.getId()));
        }
    }

    /**
     * Validates one requested service and builds its deployed form, connecting any requested datasources.
     *
     * @param _spec the service request
     * @param _index the service index, for error messages
     * @param _application the application being created
     * @return the service
     */
    @SuppressWarnings("unchecked")
    private CatalogState.AppService buildService(final JsonBody _spec, final int _index, final CatalogState.Application _application) {
        final String field = "Services[" + _index + "]";
        final String catalogId = _spec.getString("catalog_id", "");
        if (catalogId.isEmpty()) {
            throw Gin.bindingError(CREATE_STRUCT, field + ".CatalogID", "required");
        }
        final String version = _spec.getString("version", "");
        if (version.isEmpty()) {
            throw Gin.bindingError(CREATE_STRUCT, field + ".Version", "required");
        }
        final List<Object> components = _spec.getList("components");
        if (null == components) {
            throw Gin.bindingError(CREATE_STRUCT, field + ".Components", "required");
        }
        final CatalogData.Service catalogService = CatalogData.service(catalogId);
        if (null == catalogService) {
            throw Gin.error(422, "service \"" + catalogId + "\" not found in catalog");
        }
        final CatalogState.AppService service = new CatalogState.AppService(catalogId, catalogService.getName(), version);
        for (int j = 0; j < components.size(); j++) {
            final JsonBody component = new JsonBody(components.get(j) instanceof Map ? (Map<String, Object>) components.get(j) : null);
            final String componentField = field + ".Components[" + j + "]";
            final String type = component.getString("component_type", "");
            final String providerId = component.getString("provider_id", "");
            if (type.isEmpty()) {
                throw Gin.bindingError(CREATE_STRUCT, componentField + ".ComponentType", "required");
            }
            if (providerId.isEmpty()) {
                throw Gin.bindingError(CREATE_STRUCT, componentField + ".ProviderID", "required");
            }
            if (component.getString("version", "").isEmpty()) {
                throw Gin.bindingError(CREATE_STRUCT, componentField + ".Version", "required");
            }
            final CatalogData.Provider provider = CatalogData.provider(type, providerId);
            if (null == provider) {
                throw Gin.error(422, "provider \"" + providerId + "\" not found for component type \"" + type + "\"");
            }
            service.getComponents().add(new CatalogState.Component(type, providerId, provider.getName()));
        }
        final List<Object> connectors = null == _spec.getList("connectors") ? new ArrayList<Object>() : _spec.getList("connectors");
        for (final Object connector : connectors) {
            final JsonBody ref = new JsonBody(connector instanceof Map ? (Map<String, Object>) connector : null);
            final String datasourceId = ref.getString("id", "");
            if (!"datasource".equals(ref.getString("type", "")) || null == m_state.datasources().get(datasourceId)) {
                throw Gin.error(422, "datasource \"" + datasourceId + "\" not found");
            }
            if (!catalogService.isAcceptsDatasource()) {
                throw Gin.error(422, "service \"" + catalogId + "\" does not accept datasources");
            }
            _application.getDatasources().put(datasourceId, Long.valueOf(System.currentTimeMillis()));
        }
        return service;
    }

    private Response get(final Request _request) {
        synchronized (m_state) {
            return Response.json(200, view(requireApplication(_request), true));
        }
    }

    /**
     * Handles {@code PUT /api/v1/applications/:id}: renames an application.
     *
     * @param _request the request
     * @return the updated application
     */
    private Response update(final Request _request) {
        final String name = _request.getJson().getString("name", "");
        if (name.isEmpty()) {
            throw Gin.bindingError("UpdateApplicationRequest", "Name", "required");
        }
        if (3 > name.length() || 100 < name.length()) {
            throw Gin.bindingError("UpdateApplicationRequest", "Name", 3 > name.length() ? "min" : "max");
        }
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            for (final CatalogState.Application other : m_state.applications().values()) {
                if (other != application && other.getName().equals(name) && other.getOwner().equals(application.getOwner())) {
                    throw Gin.error(409, "application with name '" + name + "' already exists");
                }
            }
            application.rename(name);
            return Response.json(200, view(application, true));
        }
    }

    /**
     * Handles {@code DELETE /api/v1/applications/:id}.
     *
     * @param _request the request
     * @return {@code 202} with the deletion status
     */
    private Response delete(final Request _request) {
        final String keepData = _request.getQuery("keep_data");
        if (null != keepData && !"true".equals(keepData) && !"false".equals(keepData)) {
            throw Gin.error(400, "invalid keep_data parameter: must be 'true' or 'false'");
        }
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            if (application.isDeleting()) {
                throw Gin.error(409, "application is already being deleted");
            }
            application.requestDelete();
            return Response.json(202, Json.obj("id", application.getId(), "status", "Deleting", "message", "Deletion initiated successfully"));
        }
    }

    /**
     * Handles {@code GET /api/v1/applications/:id/resources}.
     *
     * @param _request the request
     * @return the resources consumed by the application
     */
    private Response resources(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            final boolean isRunning = "Running".equals(m_state.applicationStatus(application));
            final List<Object> cards = new ArrayList<Object>();
            for (final CatalogState.AppService service : application.getServices()) {
                for (final CatalogState.Component component : service.getComponents()) {
                    if ("vllm-spyre".equals(component.getProviderId())) {
                        cards.add(String.format("0000:%02x:00.0", Integer.valueOf(0x1a + cards.size())));
                    }
                }
            }
            final int services = application.getServices().size();
            final long gib = 1024L * 1024L * 1024L;
            final Map<String, Object> cpu = Json.obj("total_cpu", Integer.valueOf(2 * services + 4), "used_cpu", Double.valueOf(isRunning ? 0.35 * services + 1.2 : 0.0));
            final Map<String, Object> memory = Json.obj("total_bytes", Long.valueOf((4L * services + 16L) * gib), "used_bytes", Long.valueOf(isRunning ? (2L * services + 6L) * gib : 0L));
            return Response.json(200, Json.obj("cpu", cpu, "memory", memory, "accelerators", cards.isEmpty() ? Json.obj() : Json.obj(CatalogData.SPYRE, cards)));
        }
    }

    /**
     * Handles {@code GET /api/v1/applications/:id/ps}: the application's pods.
     *
     * @param _request the request
     * @return the pod listing
     */
    private Response ps(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            final String status = m_state.applicationStatus(application);
            final String podStatus = "Running".equals(status) ? "running" : "Error".equals(status) ? "exited" : "Deleting".equals(status) ? "removing" : "created";
            final List<Object> services = new ArrayList<Object>();
            final List<Object> components = new ArrayList<Object>();
            for (final CatalogState.AppService service : application.getServices()) {
                services.add(pod(application, service.getCatalogId(), podStatus));
                for (final CatalogState.Component component : service.getComponents()) {
                    components.add(pod(application, component.getType() + "-" + component.getProviderId(), podStatus));
                }
            }
            final CatalogState.Worker worker = application.getWorker();
            final Map<String, Object> body = Json.obj("id", application.getId(), "name", application.getName(), "worker_name", worker.getName());
            if ("openshift".equals(worker.getRuntimeType())) {
                body.put("namespace", application.getName());
            }
            body.put("runtime_type", worker.getRuntimeType());
            body.put("services", services);
            body.put("components", components);
            return Response.json(200, body);
        }
    }

    /**
     * Builds one simulated pod entry for {@code ps}.
     *
     * @param _application the application
     * @param _suffix the pod name suffix
     * @param _status the pod status
     * @return the pod object
     */
    private static Map<String, Object> pod(final CatalogState.Application _application, final String _suffix, final String _status) {
        final boolean isHealthy = "running".equals(_status);
        final String podName = _application.getName() + "--" + _suffix;
        return Json.obj("pod_id", Integer.toHexString(podName.hashCode()) + "a1b2c3", "pod_name", podName, "status", _status, "created",
                Times.rfc3339(_application.getCreatedAt()), "healthy", Boolean.valueOf(isHealthy), "labels", Json.obj("ai-services.io/application", _application.getName()),
                "containers", Json.list(Json.obj("name", _suffix, "status", _status, "healthy", Boolean.valueOf(isHealthy))));
    }

    // ------------------------------------------------------- datasources

    private static Map<String, Object> providerRef(final String _providerId) {
        return Json.obj("id", _providerId, "name", "object_storage".equals(_providerId) ? "Object Storage" : "File System");
    }

    private static int fileCount(final CatalogState.Datasource _datasource) {
        return 5 + Math.abs(_datasource.getName().hashCode() % 40);
    }

    /**
     * Handles {@code GET /api/v1/applications/:id/datasources}.
     *
     * @param _request the request
     * @return a page of connected datasources with sync status
     */
    private Response listDatasources(final Request _request) {
        final Gin.PageRequest page = Gin.page(_request);
        final String query = null == _request.getQuery("q") ? null : _request.getQuery("q").toLowerCase(Locale.ROOT);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            for (final Map.Entry<String, Long> connection : application.getDatasources().entrySet()) {
                final CatalogState.Datasource datasource = m_state.datasources().get(connection.getKey());
                if (null == datasource || (null != query && !datasource.getName().toLowerCase(Locale.ROOT).contains(query))) {
                    continue;
                }
                final boolean isSynced = System.currentTimeMillis() - connection.getValue().longValue() >= m_state.getConfig().getJobDurationMillis();
                final Map<String, Object> item = Json.obj("id", datasource.getId(), "name", datasource.getName(), "provider", providerRef(datasource.getProviderId()), "status",
                        isSynced ? "completed" : "syncing", "files", Integer.valueOf(isSynced ? fileCount(datasource) : 0), "last_sync",
                        isSynced ? Times.rfc3339(connection.getValue().longValue() + m_state.getConfig().getJobDurationMillis()) : null);
                if (!isSynced) {
                    item.put("message", "Processing files");
                }
                items.add(item);
            }
        }
        return Response.json(200, Json.obj("data", Pages.slice(items, page.getOffset(), page.getPageSize()), "pagination", Pages.pageInfo(page.getPage(), page.getPageSize(), items.size())));
    }

    /**
     * Handles {@code PUT /api/v1/applications/:id/datasources}: connects datasources, reporting per-datasource failures with 207.
     *
     * @param _request the request
     * @return {@code 204}, or {@code 207} with errors
     */
    private Response connectDatasources(final Request _request) {
        final List<Object> ids = _request.getJson().getList("datasource_ids");
        if (null == ids) {
            throw Gin.bindingError("ConnectDatasourcesRequest", "DatasourceIDs", "required");
        }
        if (ids.isEmpty()) {
            throw Gin.bindingError("ConnectDatasourcesRequest", "DatasourceIDs", "min");
        }
        final List<Object> errors = new ArrayList<Object>();
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            if (!application.acceptsDatasources()) {
                throw Gin.error(422, "application does not have a service that accepts datasources");
            }
            for (final Object rawId : ids) {
                final String id = String.valueOf(rawId);
                if (!Ids.isUuid(id)) {
                    throw Gin.error(400, "invalid datasource id: " + id);
                }
                if (null == m_state.datasources().get(id)) {
                    errors.add(Json.obj("datasource_id", id, "error", "datasource not found"));
                } else if (!application.getDatasources().containsKey(id)) {
                    application.getDatasources().put(id, Long.valueOf(System.currentTimeMillis()));
                }
            }
        }
        return errors.isEmpty() ? Response.empty(204) : Response.json(207, Json.obj("errors", errors));
    }

    /**
     * Resolves the {@code :datasource_id} datasource, which must be connected to the application.
     *
     * @param _application the application
     * @param _request the request
     * @return the datasource
     */
    private CatalogState.Datasource requireConnected(final CatalogState.Application _application, final Request _request) {
        final String datasourceId = _request.getPathParam("datasource_id");
        if (!Ids.isUuid(datasourceId)) {
            throw Gin.error(400, "Invalid datasource ID format");
        }
        final CatalogState.Datasource datasource = m_state.datasources().get(datasourceId);
        if (null == datasource || !_application.getDatasources().containsKey(datasourceId)) {
            throw Gin.error(404, "datasource not connected to application");
        }
        return datasource;
    }

    /**
     * Handles {@code GET /api/v1/applications/:id/datasources/:datasource_id}.
     *
     * @param _request the request
     * @return the connection and its sync details
     */
    private Response getDatasource(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            final CatalogState.Datasource datasource = requireConnected(application, _request);
            final long connectedAt = application.getDatasources().get(datasource.getId()).longValue();
            final boolean isSynced = System.currentTimeMillis() - connectedAt >= m_state.getConfig().getJobDurationMillis();
            final Map<String, Object> details = Json.obj("sync_status", isSynced ? "completed" : "syncing", "total_files", isSynced ? Integer.valueOf(fileCount(datasource)) : null, "last_sync_at", isSynced ? Times.rfc3339(connectedAt + m_state.getConfig().getJobDurationMillis()) : null);
            return Response.json(200, Json.obj("id", datasource.getId(), "name", datasource.getName(), "status", "connected", "provider", providerRef(datasource.getProviderId()), "service_details", details));
        }
    }

    /**
     * Handles {@code DELETE /api/v1/applications/:id/datasources/:datasource_id}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response disconnectDatasource(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Application application = requireApplication(_request);
            final CatalogState.Datasource datasource = requireConnected(application, _request);
            application.getDatasources().remove(datasource.getId());
        }
        return Response.empty(204);
    }
}
