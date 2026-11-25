package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.List;
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
 * Mock of the catalog server's {@code /api/v1/models} routes for user-deployed models.
 * <p>
 * Local models progress through {@code Downloading}, {@code Deploying} and {@code Running}; remote models are {@code connected} immediately.
 */
final class ModelsApi {

    private static final String CREATE_STRUCT = "CreateModelRequest";

    private final CatalogState m_state;
    private final CatalogAuth m_auth;

    ModelsApi(final CatalogState _state, final CatalogAuth _auth) {
        m_state = _state;
        m_auth = _auth;
    }

    /**
     * Registers the model routes.
     *
     * @param _router the catalog router
     */
    void register(final Router _router) {
        _router.post("/api/v1/models", m_auth.authed(this::create));
        _router.get("/api/v1/models", m_auth.authed(this::list));
        _router.get("/api/v1/models/{id}", m_auth.authed(this::get));
        _router.post("/api/v1/models/{id}/keys", m_auth.authed(this::generateKey));
        _router.delete("/api/v1/models/{id}", m_auth.authed(this::delete));
    }

    /**
     * Returns the reported status of a model.
     *
     * @param _model the model
     * @return the status
     */
    private String status(final CatalogState.DeployedModel _model) {
        if (_model.isDeleting()) {
            return "Deleting";
        }
        if ("remote".equals(_model.getDeploymentType())) {
            return _model.isFailing() ? "offline" : "connected";
        }
        return m_state.deploymentStatus(_model.getCreatedAt(), _model.isFailing());
    }

    /**
     * Builds the model list item.
     *
     * @param _model the model
     * @return the model view
     */
    private Map<String, Object> view(final CatalogState.DeployedModel _model) {
        final String status = status(_model);
        final Map<String, Object> view = Json.obj("id", _model.getId(), "deployment_type", _model.getDeploymentType(), "name", _model.getName(), "provider", Json.obj("id",
                _model.getProvider().getId(), "name", _model.getProvider().getName()), "worker", null == _model.getWorker() ? null : _model.getWorker().toRef(), "model",
                _model.getModel());
        if (!_model.getParams().isEmpty()) {
            view.put("params", _model.getParams());
        }
        view.put("status", status);
        view.put("message", "Error".equals(status) ? "Model server failed to start (simulated)" : "Downloading".equals(status) ? "Downloading model weights" : "");
        view.put("connected_services", Integer.valueOf(0));
        view.put("created_at", Times.rfc3339(_model.getCreatedAt()));
        view.put("updated_at", Times.rfc3339("Running".equals(status) ? _model.getCreatedAt() + m_state.getConfig().getJobDurationMillis() : _model.getCreatedAt()));
        return view;
    }

    /**
     * Handles {@code POST /api/v1/models}.
     *
     * @param _request the request
     * @return {@code 202} for local deployments or {@code 201} for remote registrations, with the model ID
     */
    private Response create(final Request _request) {
        final JsonBody body = _request.getJson();
        final String deploymentType = requireField(body, "deployment_type", "DeploymentType");
        final String componentType = requireField(body, "component_type", "ComponentType");
        final String name = requireField(body, "name", "Name");
        final String providerId = requireField(body, "provider_id", "ProviderID");
        final String modelId = requireField(body, "model", "Model");
        if (!"local".equals(deploymentType) && !"remote".equals(deploymentType)) {
            throw Gin.bindingError(CREATE_STRUCT, "DeploymentType", "oneof");
        }
        if (3 > name.length() || 100 < name.length()) {
            throw Gin.bindingError(CREATE_STRUCT, "Name", 3 > name.length() ? "min" : "max");
        }
        if (!CatalogData.isComponentType(componentType)) {
            throw Gin.error(400, "invalid component_type \"" + componentType + "\"");
        }
        final CatalogData.Provider provider = CatalogData.provider(componentType, providerId);
        if (null == provider) {
            throw Gin.error(404, "provider \"" + providerId + "\" not found for component type \"" + componentType + "\"");
        }
        if (!provider.getDeploymentType().equals(deploymentType)) {
            throw Gin.error(422, "provider \"" + providerId + "\" does not support deployment_type \"" + deploymentType + "\"");
        }
        final boolean isLocal = "local".equals(deploymentType);
        final CatalogData.Model catalogModel = CatalogData.model(modelId);
        if (isLocal && (null == catalogModel || !catalogModel.getProviderIds().contains(providerId))) {
            throw Gin.error(404, "model \"" + modelId + "\" not found for provider \"" + providerId + "\"");
        }
        final Map<String, Object> params = null == body.getMap("params") ? Json.obj() : body.getMap("params");
        final String user = m_auth.userOf(_request);
        synchronized (m_state) {
            for (final CatalogState.DeployedModel existing : m_state.models().values()) {
                if (existing.getName().equals(name) && existing.getOwner().equals(user)) {
                    throw Gin.error(409, "model with name '" + name + "' already exists");
                }
            }
            CatalogState.Worker worker = null;
            if (isLocal) {
                worker = m_state.workerByName(body.getString("worker_name", null));
                if (null == worker) {
                    throw Gin.error(404, "worker \"" + body.getString("worker_name", "") + "\" not found");
                }
            }
            final String profile = body.getString("profile", isLocal ? "standard" : null);
            final CatalogState.DeployedModel model = new CatalogState.DeployedModel(deploymentType, componentType, name, provider, modelId, profile, worker, params, user, MockService.requestsFailure(name));
            m_state.models().put(model.getId(), model);
            return Response.json(isLocal ? 202 : 201, Json.obj("id", model.getId()));
        }
    }

    /**
     * Reads a required string field, failing like gin's {@code required} binding tag.
     *
     * @param _body the body
     * @param _jsonName the JSON field name
     * @param _goName the Go struct field name used in the error
     * @return the value
     */
    private static String requireField(final JsonBody _body, final String _jsonName, final String _goName) {
        final String value = _body.getString(_jsonName, "");
        if (value.isEmpty()) {
            throw Gin.bindingError(CREATE_STRUCT, _goName, "required");
        }
        return value;
    }

    /**
     * Handles {@code GET /api/v1/models}.
     *
     * @param _request the request
     * @return a page of the caller's models
     */
    private Response list(final Request _request) {
        final Gin.PageRequest page = Gin.page(_request);
        final String componentType = _request.getQuery("component_type");
        final String deploymentType = _request.getQuery("deployment_type");
        final List<String> providers = _request.getQueryValues("provider_id");
        final List<String> modelNames = _request.getQueryValues("model");
        final String status = _request.getQuery("status");
        final String user = m_auth.userOf(_request);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_state) {
            for (final CatalogState.DeployedModel model : m_state.models().values()) {
                final boolean isMatch = model.getOwner().equals(user) && (null == componentType || componentType.equals(model.getComponentType())) && (null == deploymentType || deploymentType.equals(model.getDeploymentType()))
                        && (providers.isEmpty() || providers.contains(model.getProvider().getId())) && (modelNames.isEmpty() || modelNames.contains(model.getModel()))
                        && (null == status || status.equalsIgnoreCase(status(model)));
                if (isMatch) {
                    items.add(view(model));
                }
            }
        }
        return Response.json(200, Json.obj("data", Pages.slice(items, page.getOffset(), page.getPageSize()), "pagination", Pages.pageInfo(page.getPage(), page.getPageSize(), items.size())));
    }

    /**
     * Resolves the {@code :id} model, enforcing UUID format and ownership.
     *
     * @param _request the request
     * @return the model
     */
    private CatalogState.DeployedModel requireModel(final Request _request) {
        final String id = _request.getPathParam("id");
        if (!Ids.isUuid(id)) {
            throw Gin.error(400, "Invalid model ID format");
        }
        final CatalogState.DeployedModel model = m_state.models().get(id);
        if (null == model) {
            throw Gin.error(404, "model not found");
        }
        if (!model.getOwner().equals(m_auth.userOf(_request))) {
            throw Gin.error(403, "you do not have access to this model");
        }
        return model;
    }

    /**
     * Handles {@code GET /api/v1/models/:id}.
     *
     * @param _request the request
     * @return the model details with endpoints and consuming applications
     */
    private Response get(final Request _request) {
        synchronized (m_state) {
            final CatalogState.DeployedModel model = requireModel(_request);
            final Map<String, Object> detail = view(model);
            final String status = status(model);
            if ("remote".equals(model.getDeploymentType()) && model.getParams().get("url") instanceof String) {
                detail.put("endpoints", Json.list(Json.obj("type", "external", "url", model.getParams().get("url"))));
            } else if ("Running".equals(status)) {
                detail.put("endpoints", Json.list(Json.obj("type", "internal", "url", "http://" + model.getName() + ":8000/v1")));
            }
            detail.put("applications", Json.list());
            return Response.json(200, detail);
        }
    }

    /**
     * Handles {@code POST /api/v1/models/:id/keys}: issues an API key for the model endpoint.
     *
     * @param _request the request
     * @return {@code 201} with the key
     */
    private Response generateKey(final Request _request) {
        synchronized (m_state) {
            final CatalogState.DeployedModel model = requireModel(_request);
            if ("openshift".equals(m_state.getConfig().getCatalogRuntime())) {
                throw Gin.error(400, "model API keys are not supported on the openshift runtime");
            }
            if (model.isDeleting()) {
                throw Gin.error(400, "model is being deleted");
            }
            final String key = "sk-" + Ids.uuid().replace("-", "") + Ids.uuid().replace("-", "").substring(0, 16);
            return Response.json(201, Json.obj("key", key, "type", "public", "model", model.getModel())).withHeader("Cache-Control", "no-store");
        }
    }

    /**
     * Handles {@code DELETE /api/v1/models/:id}.
     *
     * @param _request the request
     * @return {@code 202} while a local model is undeployed, or {@code 204} for remote models
     */
    private Response delete(final Request _request) {
        synchronized (m_state) {
            final CatalogState.DeployedModel model = requireModel(_request);
            if (model.isDeleting()) {
                throw Gin.error(409, "model is already being deleted");
            }
            if ("remote".equals(model.getDeploymentType())) {
                m_state.models().remove(model.getId());
                return Response.empty(204);
            }
            model.requestDelete();
            return Response.json(202, Json.obj("id", model.getId(), "status", "Deleting", "message", "Undeploy initiated"));
        }
    }
}
