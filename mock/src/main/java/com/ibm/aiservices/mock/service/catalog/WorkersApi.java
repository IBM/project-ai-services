package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.common.Pages;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.Times;

/**
 * Mock of the catalog server's {@code /api/v1/workers} routes. A registered worker reports {@code pending} until its simulated bootstrap completes.
 */
final class WorkersApi {

    private final CatalogState m_state;
    private final CatalogAuth m_auth;

    WorkersApi(final CatalogState _state, final CatalogAuth _auth) {
        m_state = _state;
        m_auth = _auth;
    }

    /**
     * Registers the worker routes.
     *
     * @param _router the catalog router
     */
    void register(final Router _router) {
        _router.post("/api/v1/workers", m_auth.authed(this::create));
        _router.get("/api/v1/workers", m_auth.authed(this::list));
        _router.get("/api/v1/workers/{id}", m_auth.authed(this::get));
        _router.delete("/api/v1/workers/{id}", m_auth.authed(this::delete));
    }

    /**
     * Builds the worker object.
     *
     * @param _worker the worker
     * @return the worker view
     */
    private Map<String, Object> view(final CatalogState.Worker _worker) {
        final String status = m_state.workerStatus(_worker);
        final long now = System.currentTimeMillis();
        final Map<String, Object> view = Json.obj("id", _worker.getId(), "name", _worker.getName(), "runtime_type", _worker.getRuntimeType(), "status", status);
        if ("pending".equals(status)) {
            view.put("message", "Waiting for the worker agent to connect");
        } else {
            view.put("last_heartbeat", Times.rfc3339(now - 5000L));
            view.put("metadata", Json.obj("arch", "ppc64le", "os", "linux", "agent_version", "1.0.0"));
        }
        final Set<String> applicationIds = m_state.applicationIdsOn(_worker);
        if (!applicationIds.isEmpty()) {
            view.put("application_ids", new ArrayList<String>(applicationIds));
        }
        view.put("registered_at", Times.rfc3339(_worker.getRegisteredAt()));
        view.put("updated_at", Times.rfc3339("pending".equals(status) ? _worker.getRegisteredAt() : now - 5000L));
        return view;
    }

    /**
     * Handles {@code POST /api/v1/workers}: registers a worker and returns its bootstrap token.
     *
     * @param _request the request
     * @return {@code 201} with the gateway address and token
     */
    private Response create(final Request _request) {
        final String name = _request.getJson().getString("worker_name", "").trim();
        if (name.isEmpty()) {
            throw Gin.error(400, "invalid payload");
        }
        if (3 > name.length() || 64 < name.length()) {
            throw Gin.error(400, "worker name must be between 3 and 64 characters");
        }
        if (CatalogState.LOCAL_WORKER.equals(name)) {
            throw Gin.error(400, "worker name \"" + CatalogState.LOCAL_WORKER + "\" is reserved");
        }
        synchronized (m_state) {
            final CatalogState.Worker existing = m_state.workerByName(name);
            if (null != existing && "ready".equals(m_state.workerStatus(existing))) {
                throw Gin.error(409, "worker \"" + name + "\" is already registered and ready");
            }
            if (null != existing) {
                m_state.workers().remove(existing.getId());
            }
            final CatalogState.Worker worker = new CatalogState.Worker(name, m_state.getConfig().getCatalogRuntime(), false);
            m_state.workers().put(worker.getId(), worker);
        }
        final String token = Ids.uuid().replace("-", "") + Ids.uuid().replace("-", "");
        return Response.json(201, Json.obj("worker_name", name, "gateway_address", "ai-services-worker-gateway.apps.local:9191", "token", token));
    }

    /**
     * Handles {@code GET /api/v1/workers}.
     *
     * @param _request the request
     * @return a page of workers
     */
    private Response list(final Request _request) {
        final Gin.PageRequest page = Gin.page(_request);
        final String query = null == _request.getQuery("q") ? null : _request.getQuery("q").toLowerCase(Locale.ROOT);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_state) {
            for (final CatalogState.Worker worker : m_state.workers().values()) {
                if (null == query || worker.getName().toLowerCase(Locale.ROOT).contains(query)) {
                    items.add(view(worker));
                }
            }
        }
        return Response.json(200, Json.obj("data", Pages.slice(items, page.getOffset(), page.getPageSize()), "pagination", Pages.pageInfo(page.getPage(), page.getPageSize(), items.size())));
    }

    /**
     * Resolves the {@code :id} worker.
     *
     * @param _request the request
     * @return the worker
     */
    private CatalogState.Worker requireWorker(final Request _request) {
        final String id = _request.getPathParam("id");
        if (!Ids.isUuid(id)) {
            throw Gin.error(400, "invalid worker id");
        }
        final CatalogState.Worker worker = m_state.workers().get(id);
        if (null == worker) {
            throw Gin.error(404, "worker not found");
        }
        return worker;
    }

    private Response get(final Request _request) {
        synchronized (m_state) {
            return Response.json(200, view(requireWorker(_request)));
        }
    }

    /**
     * Handles {@code DELETE /api/v1/workers/:id}.
     *
     * @param _request the request
     * @return {@code 204}
     */
    private Response delete(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Worker worker = requireWorker(_request);
            if (CatalogState.LOCAL_WORKER.equals(worker.getName())) {
                throw Gin.error(403, "the Local worker cannot be deleted");
            }
            if (!m_state.applicationIdsOn(worker).isEmpty()) {
                throw Gin.error(409, "worker \"" + worker.getName() + "\" still has applications deployed");
            }
            m_state.workers().remove(worker.getId());
        }
        return Response.empty(204);
    }
}
