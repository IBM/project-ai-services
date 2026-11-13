package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.ErrorResponder;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.MockService;
import com.ibm.aiservices.mock.service.common.FastApi;
import com.ibm.aiservices.mock.service.common.OpenApiDoc;
import com.ibm.aiservices.mock.util.Json;

/**
 * Mock of the Go catalog API server ({@code ai-services/internal/pkg/catalog/apiserver}): authentication, the read-only catalog, applications, workers, datasources, bundles and models.
 */
public final class CatalogService extends MockService {

    private final CatalogState m_state;
    private final CatalogAuth m_auth;

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public CatalogService(final MockConfig _config) {
        super(_config);
        m_state = new CatalogState(_config);
        m_auth = new CatalogAuth(_config);
    }

    @Override
    public String getName() {
        return "catalog";
    }

    @Override
    public String getTitle() {
        return "AI Services Catalog API Server";
    }

    @Override
    public int getDefaultPort() {
        return 8080;
    }

    @Override
    protected ErrorResponder getErrorResponder() {
        return Gin.ERRORS;
    }

    @Override
    protected void registerRoutes(final Router _router) {
        _router.get("/healthz", _request -> Response.json(200, Json.obj("message", "ok")));
        _router.get("/health", _request -> Response.json(200, Json.obj("message", "ok")));
        _router.get("/swagger/doc.json", _request -> Response.json(200, OpenApiDoc.build(getTitle(), "1.0", _router)));
        _router.get("/swagger/{any*}", _request -> FastApi.swaggerUi(getTitle(), "/swagger/doc.json"));
        m_auth.register(_router);
        registerCatalogRoutes(_router);
        new ApplicationsApi(m_state, m_auth).register(_router);
        new WorkersApi(m_state, m_auth).register(_router);
        new DatasourcesApi(m_state, m_auth).register(_router);
        new BundlesApi(m_state, m_auth).register(_router);
        new ModelsApi(m_state, m_auth).register(_router);
    }

    /**
     * Registers the read-only catalog routes, all behind the bearer middleware.
     *
     * @param _router the catalog router
     */
    private void registerCatalogRoutes(final Router _router) {
        _router.get("/api/v1/resources", m_auth.authed(this::getResources));
        _router.get("/api/v1/architectures", m_auth.authed(this::listArchitectures));
        _router.get("/api/v1/architectures/{id}", m_auth.authed(_request -> Response.json(200, requireArchitecture(_request))));
        _router.get("/api/v1/architectures/{id}/deploy-options", m_auth.authed(this::architectureDeployOptions));
        _router.get("/api/v1/architectures/{id}/images", m_auth.authed(this::architectureImages));
        _router.get("/api/v1/architectures/{id}/models", m_auth.authed(this::architectureModels));
        _router.get("/api/v1/services", m_auth.authed(this::listServices));
        _router.get("/api/v1/services/{id}", m_auth.authed(_request -> Response.json(200, requireService(_request).toDetail())));
        _router.get("/api/v1/services/{id}/deploy-options", m_auth.authed(this::serviceDeployOptions));
        _router.get("/api/v1/services/{id}/params", m_auth.authed(this::serviceParams));
        _router.get("/api/v1/services/{id}/images", m_auth.authed(this::serviceImages));
        _router.get("/api/v1/services/{id}/models", m_auth.authed(this::serviceModels));
        _router.get("/api/v1/services/{id}/steps", m_auth.authed(this::serviceSteps));
        _router.get("/api/v1/catalog/models", m_auth.authed(this::listCatalogModels));
        _router.get("/api/v1/catalog/models/{id}", m_auth.authed(this::getCatalogModel));
        _router.get("/api/v1/catalog/models/{id}/deploy-options", m_auth.authed(this::modelDeployOptions));
        _router.get("/api/v1/components/{component_type}/providers/{provider_id}/params", m_auth.authed(this::componentProviderParams));
        _router.get("/api/v1/connectors", m_auth.authed(this::listConnectors));
        _router.get("/api/v1/connectors/{connector_type}/providers/{provider_id}/params", m_auth.authed(this::connectorProviderParams));
    }

    private String serverRuntime() {
        return getConfig().getCatalogRuntime();
    }

    /**
     * Handles {@code GET /api/v1/resources}: capacity of a worker, less what running deployments consume.
     *
     * @param _request the request
     * @return the resource summary
     */
    private Response getResources(final Request _request) {
        final String workerName = null == _request.getQuery("worker") ? CatalogState.LOCAL_WORKER : _request.getQuery("worker");
        synchronized (m_state) {
            final CatalogState.Worker worker = m_state.workerByName(workerName);
            if (null == worker || !"ready".equals(m_state.workerStatus(worker))) {
                throw Gin.error(400, "worker \"" + workerName + "\" is not connected");
            }
            int usedCards = 0;
            for (final CatalogState.DeployedModel model : m_state.models().values()) {
                if (model.getWorker() == worker && "vllm-spyre".equals(model.getProvider().getId())) {
                    usedCards += "balanced".equals(model.getProfile()) ? 2 : 4;
                }
            }
            final int apps = m_state.applicationIdsOn(worker).size();
            final long gib = 1024L * 1024L * 1024L;
            final Map<String, Object> cpu = Json.obj("total_cpu", Integer.valueOf(64), "available_cpu", Double.valueOf(Math.max(0.0, 64.0 - 6.5 * apps - 2.0 * usedCards)));
            final Map<String, Object> memory = Json.obj("total_bytes", Long.valueOf(512L * gib), "available_bytes", Long.valueOf(Math.max(0L, 512L * gib - apps * 24L * gib - usedCards * 50L * gib)));
            final Map<String, Object> accelerators = Json.obj(CatalogData.SPYRE, Json.obj("total", Integer.valueOf(8), "available", Integer.valueOf(Math.max(0, 8 - usedCards))));
            return Response.json(200, Json.obj("cpu", cpu, "memory", memory, "accelerators", accelerators));
        }
    }

    private Response listArchitectures(final Request _request) {
        return Response.json(200, Json.list(Json.obj("id", "rag", "name", "Digital assistants", "description", CatalogData.architectureDescription(), "certified_by", "IBM", "services", CatalogData.architectureServices())));
    }

    /**
     * Resolves the {@code :id} architecture; only {@code rag} exists.
     *
     * @param _request the request
     * @return the architecture
     */
    private static Map<String, Object> requireArchitecture(final Request _request) {
        final String id = _request.getPathParam("id");
        if (!"rag".equals(id)) {
            throw Gin.error(404, "Architecture '" + id + "' not found: no such architecture in catalog");
        }
        return CatalogData.architecture();
    }

    /**
     * Handles {@code GET /api/v1/architectures/:id/deploy-options}.
     *
     * @param _request the request
     * @return the architecture's global components and services with their providers
     */
    private Response architectureDeployOptions(final Request _request) {
        Gin.runtime(_request, serverRuntime());
        final Map<String, Object> architecture = requireArchitecture(_request);
        final List<Object> services = new ArrayList<Object>();
        for (final String serviceId : CatalogData.architectureServices()) {
            services.add(CatalogData.serviceDeployOptions(CatalogData.service(serviceId)));
        }
        final List<Object> globals = Json.list(CatalogData.componentOption("vector_store"), CatalogData.componentOption("embedding"));
        return Response.json(200, Json.obj("id", architecture.get("id"), "name", architecture.get("name"), "version", architecture.get("version"), "global_components", globals, "services", services));
    }

    private Response architectureImages(final Request _request) {
        requireArchitecture(_request);
        rejectOnOpenShift();
        return Response.json(200, CatalogData.images(CatalogData.architectureServices()));
    }

    private Response architectureModels(final Request _request) {
        requireArchitecture(_request);
        return Response.json(200, modelNamesFor(Arrays.asList("llm", "embedding", "reranker"), _request.getQuery("exclude_providers")));
    }

    private void rejectOnOpenShift() {
        if ("openshift".equals(serverRuntime())) {
            throw Gin.error(422, "this endpoint is not supported on OpenShift deployments");
        }
    }

    /**
     * Lists the model IDs usable by the given component types, minus excluded providers.
     *
     * @param _componentTypes the component types
     * @param _excludeProviders comma separated provider IDs to exclude, or {@code null}
     * @return the model IDs
     */
    private static List<String> modelNamesFor(final List<String> _componentTypes, final String _excludeProviders) {
        final List<String> excluded = null == _excludeProviders ? new ArrayList<String>() : Arrays.asList(_excludeProviders.split("\\s*,\\s*"));
        final List<String> names = new ArrayList<String>();
        for (final CatalogData.Model model : CatalogData.models()) {
            if (!_componentTypes.contains(model.getComponentType())) {
                continue;
            }
            for (final String providerId : model.getProviderIds()) {
                if (!excluded.contains(providerId)) {
                    names.add(model.getId());
                    break;
                }
            }
        }
        return names;
    }

    /**
     * Handles {@code GET /api/v1/services}.
     *
     * @param _request the request
     * @return the service summaries
     */
    private Response listServices(final Request _request) {
        final List<Object> services = new ArrayList<Object>();
        for (final CatalogData.Service service : CatalogData.services()) {
            services.add(service.toSummary());
        }
        return Response.json(200, services);
    }

    /**
     * Resolves the {@code :id} catalog service.
     *
     * @param _request the request
     * @return the service
     */
    private static CatalogData.Service requireService(final Request _request) {
        final String id = _request.getPathParam("id");
        final CatalogData.Service service = CatalogData.service(id);
        if (null == service) {
            throw Gin.error(404, "Service '" + id + "' not found: no such service in catalog");
        }
        return service;
    }

    private Response serviceDeployOptions(final Request _request) {
        Gin.runtime(_request, serverRuntime());
        return Response.json(200, CatalogData.serviceDeployOptions(requireService(_request)));
    }

    private Response serviceParams(final Request _request) {
        Gin.runtime(_request, serverRuntime());
        return Response.json(200, CatalogData.serviceParamsSchema(requireService(_request)));
    }

    private Response serviceImages(final Request _request) {
        final CatalogData.Service service = requireService(_request);
        rejectOnOpenShift();
        return Response.json(200, CatalogData.images(Arrays.asList(service.getId())));
    }

    private Response serviceModels(final Request _request) {
        final CatalogData.Service service = requireService(_request);
        return Response.json(200, modelNamesFor(service.getDependencies(), _request.getQuery("exclude_providers")));
    }

    private Response serviceSteps(final Request _request) {
        final String runtime = Gin.runtime(_request, "podman");
        return Response.json(200, CatalogData.steps(requireService(_request), runtime));
    }

    /**
     * Builds the catalog entry of a model.
     *
     * @param _model the model
     * @return the entry with its component type and providers
     */
    private static Map<String, Object> modelEntry(final CatalogData.Model _model) {
        final List<Object> providers = new ArrayList<Object>();
        for (final String providerId : _model.getProviderIds()) {
            final CatalogData.Provider provider = CatalogData.provider(_model.getComponentType(), providerId);
            providers.add(Json.obj("id", provider.getId(), "name", provider.getName(), "deployment_type", provider.getDeploymentType()));
        }
        return Json.obj("id", _model.getId(), "name", _model.getName(), "component_type", CatalogData.componentType(_model.getComponentType()), "certified_by", _model.getCertifiedBy(), "description", _model.getDescription(), "providers", providers);
    }

    /**
     * Handles {@code GET /api/v1/catalog/models}, optionally filtered by component type.
     *
     * @param _request the request
     * @return the model entries, sorted by ID
     */
    private Response listCatalogModels(final Request _request) {
        final String componentType = _request.getQuery("component_type");
        if (null != componentType && !Arrays.asList("llm", "embedding", "reranker").contains(componentType)) {
            throw Gin.error(400, "invalid component_type \"" + componentType + "\": must be one of llm, embedding, reranker");
        }
        final List<Object> entries = new ArrayList<Object>();
        for (final CatalogData.Model model : CatalogData.models()) {
            if (null == componentType || componentType.equals(model.getComponentType())) {
                entries.add(modelEntry(model));
            }
        }
        return Response.json(200, entries);
    }

    /**
     * Resolves the {@code :id} catalog model; the ID may contain an encoded slash.
     *
     * @param _request the request
     * @return the model
     */
    private static CatalogData.Model requireModel(final Request _request) {
        final String id = _request.getPathParam("id");
        final CatalogData.Model model = CatalogData.model(id);
        if (null == model) {
            throw Gin.error(404, "model \"" + id + "\" not found");
        }
        return model;
    }

    /**
     * Handles {@code GET /api/v1/catalog/models/:id}.
     *
     * @param _request the request
     * @return the model details
     */
    private Response getCatalogModel(final Request _request) {
        final CatalogData.Model model = requireModel(_request);
        final Map<String, Object> detail = modelEntry(model);
        detail.put("license", model.getLicense());
        detail.put("about", Json.list(Json.obj("title", "Model strengths", "values", Json.list(model.getDescription()))));
        return Response.json(200, detail);
    }

    /**
     * Handles {@code GET /api/v1/catalog/models/:id/deploy-options}.
     *
     * @param _request the request
     * @return the providers able to serve the model, with local profiles
     */
    private Response modelDeployOptions(final Request _request) {
        Gin.runtime(_request, serverRuntime());
        final CatalogData.Model model = requireModel(_request);
        final String providerFilter = _request.getQuery("provider_id");
        if (null != providerFilter && !model.getProviderIds().contains(providerFilter)) {
            throw Gin.error(404, "provider \"" + providerFilter + "\" not found for model \"" + model.getId() + "\"");
        }
        final List<Object> providers = new ArrayList<Object>();
        for (int i = 0; i < model.getProviderIds().size(); i++) {
            final String providerId = model.getProviderIds().get(i);
            if (null != providerFilter && !providerFilter.equals(providerId)) {
                continue;
            }
            final CatalogData.Provider provider = CatalogData.provider(model.getComponentType(), providerId);
            final Map<String, Object> option = Json.obj("id", provider.getId(), "name", provider.getName(), "deployment_type", provider.getDeploymentType(), "default", Boolean.valueOf(0 == i));
            if ("local".equals(provider.getDeploymentType())) {
                option.put("profiles", CatalogData.profiles(provider));
            } else {
                option.put("schema", "/api/v1/components/" + model.getComponentType() + "/providers/" + provider.getId() + "/params");
            }
            providers.add(option);
        }
        return Response.json(200, Json.obj("id", model.getId(), "name", model.getName(), "component_type", CatalogData.componentType(model.getComponentType()), "providers", providers));
    }

    /**
     * Handles {@code GET /api/v1/components/:component_type/providers/:provider_id/params}.
     *
     * @param _request the request
     * @return the provider's parameter schema
     */
    private Response componentProviderParams(final Request _request) {
        Gin.runtime(_request, serverRuntime());
        final String componentType = _request.getPathParam("component_type");
        final String providerId = _request.getPathParam("provider_id");
        final CatalogData.Provider provider = CatalogData.provider(componentType, providerId);
        if (null == provider) {
            throw Gin.error(404, "Failed to get parameters for provider '" + componentType + "/" + providerId + "': provider not found");
        }
        return Response.json(200, CatalogData.providerParamsSchema(provider));
    }

    /**
     * Handles {@code GET /api/v1/connectors}, optionally filtered by connector type.
     *
     * @param _request the request
     * @return the connector providers
     */
    private Response listConnectors(final Request _request) {
        final String type = _request.getQuery("type");
        final List<Object> connectors = new ArrayList<Object>();
        for (final String[] row : CatalogData.connectors()) {
            if (null == type || type.equals(row[0])) {
                connectors.add(Json.obj("type", row[0], "name", row[1], "provider", Json.obj("id", row[2], "name", row[3], "description", row[4], "schema", "/api/v1/connectors/" + row[0] + "/providers/" + row[2] + "/params")));
            }
        }
        if (null != type && connectors.isEmpty()) {
            throw Gin.error(404, "connector type \"" + type + "\" not found");
        }
        return Response.json(200, connectors);
    }

    /**
     * Handles {@code GET /api/v1/connectors/:connector_type/providers/:provider_id/params}.
     *
     * @param _request the request
     * @return the connector provider's parameter schema
     */
    private Response connectorProviderParams(final Request _request) {
        final String type = _request.getPathParam("connector_type");
        final String providerId = _request.getPathParam("provider_id");
        for (final String[] row : CatalogData.connectors()) {
            if (row[0].equals(type) && row[2].equals(providerId)) {
                return Response.json(200, CatalogData.connectorParamsSchema(providerId)).withHeader("Content-Type", "application/json; charset=utf-8");
            }
        }
        throw Gin.error(404, "connector provider \"" + type + "/" + providerId + "\" not found");
    }
}
