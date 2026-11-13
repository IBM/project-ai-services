package com.ibm.aiservices.mock.service.catalog;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import com.ibm.aiservices.mock.http.MultipartForm;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.common.Pages;
import com.ibm.aiservices.mock.util.Json;

/**
 * Mock of the catalog server's {@code /api/v1/catalog/bundles} routes. Uploaded archives are inspected for their {@code metadata.yaml} but not stored.
 */
final class BundlesApi {

    private static final long MAX_BUNDLE_BYTES = 20L * 1024L * 1024L;

    private final CatalogState m_state;
    private final CatalogAuth m_auth;

    BundlesApi(final CatalogState _state, final CatalogAuth _auth) {
        m_state = _state;
        m_auth = _auth;
    }

    /**
     * Registers the bundle routes. {@code /validate} is registered before {@code /:id} so it is not captured as an ID.
     *
     * @param _router the catalog router
     */
    void register(final Router _router) {
        _router.post("/api/v1/catalog/bundles", m_auth.authed(this::create));
        _router.post("/api/v1/catalog/bundles/validate", m_auth.authed(this::validate));
        _router.get("/api/v1/catalog/bundles", m_auth.authed(this::list));
        _router.get("/api/v1/catalog/bundles/{id}", m_auth.authed(this::get));
        _router.put("/api/v1/catalog/bundles/{id}", m_auth.authed(this::update));
        _router.delete("/api/v1/catalog/bundles/{id}", m_auth.authed(this::delete));
    }

    /**
     * Reads and validates the uploaded {@code file} field.
     *
     * @param _request the multipart request
     * @return the archive part
     */
    private static MultipartForm.Part uploadedFile(final Request _request) {
        if (!_request.hasContentType("multipart/form-data")) {
            throw Gin.error(400, "missing or unreadable 'file' field: request Content-Type isn't multipart/form-data");
        }
        final MultipartForm.Part file = _request.getMultipart().getFile("file");
        if (null == file) {
            throw Gin.error(400, "missing or unreadable 'file' field: http: no such file");
        }
        if (!file.getFilename().endsWith(".tar.gz")) {
            throw Gin.error(400, "file must be a .tar.gz archive");
        }
        if (file.getData().length > MAX_BUNDLE_BYTES) {
            throw Gin.error(400, "file exceeds the maximum bundle size of 20 MB");
        }
        return file;
    }

    /**
     * Reads bundle metadata, reporting invalid archives as {@code 422}.
     *
     * @param _file the uploaded archive
     * @return the metadata
     */
    private static BundleArchive.Metadata inspect(final MultipartForm.Part _file) {
        try {
            return BundleArchive.read(_file.getData());
        } catch (final BundleArchive.InvalidBundleException e) {
            throw Gin.error(422, e.getMessage());
        }
    }

    /**
     * Handles {@code POST /api/v1/catalog/bundles}.
     *
     * @param _request the request
     * @return {@code 201} with the bundle
     */
    private Response create(final Request _request) {
        final MultipartForm.Part file = uploadedFile(_request);
        final BundleArchive.Metadata metadata = inspect(file);
        synchronized (m_state) {
            for (final CatalogState.Bundle existing : m_state.bundles().values()) {
                if (existing.getCatalogId().equals(metadata.getId()) && existing.getVersion().equals(metadata.getVersion())) {
                    throw Gin.error(409, "bundle for " + metadata.getType() + " \"" + metadata.getId() + "\" version " + metadata.getVersion() + " already exists");
                }
            }
            final CatalogState.Bundle bundle = new CatalogState.Bundle(m_auth.userOf(_request));
            bundle.update(metadata, file.getData().length);
            m_state.bundles().put(bundle.getId(), bundle);
            return Response.json(201, bundle.toView()).withHeader("Location", "/api/v1/catalog/bundles/" + bundle.getId());
        }
    }

    /**
     * Handles {@code POST /api/v1/catalog/bundles/validate}.
     *
     * @param _request the request
     * @return the validation result
     */
    private Response validate(final Request _request) {
        final BundleArchive.Metadata metadata = inspect(uploadedFile(_request));
        final Map<String, Object> body = Json.obj("valid", Boolean.TRUE, "catalog_type", metadata.getType(), "catalog_id", metadata.getId(), "version", metadata.getVersion());
        if (null != metadata.getName()) {
            body.put("name", metadata.getName());
        }
        return Response.json(200, body);
    }

    /**
     * Handles {@code GET /api/v1/catalog/bundles}, newest first under the {@code bundles} key.
     *
     * @param _request the request
     * @return a page of bundles
     */
    private Response list(final Request _request) {
        final Gin.PageRequest page = Gin.page(_request);
        final List<Map<String, Object>> items = new ArrayList<Map<String, Object>>();
        synchronized (m_state) {
            final List<CatalogState.Bundle> bundles = new ArrayList<CatalogState.Bundle>(m_state.bundles().values());
            bundles.sort((_a, _b) -> Long.compare(_b.getCreatedAt(), _a.getCreatedAt()));
            for (final CatalogState.Bundle bundle : bundles) {
                items.add(bundle.toView());
            }
        }
        return Response.json(200, Json.obj("bundles", Pages.slice(items, page.getOffset(), page.getPageSize()), "pagination", Pages.pageInfo(page.getPage(), page.getPageSize(), items.size())));
    }

    /**
     * Resolves the {@code :id} bundle.
     *
     * @param _request the request
     * @return the bundle
     */
    private CatalogState.Bundle requireBundle(final Request _request) {
        final String id = _request.getPathParam("id");
        final CatalogState.Bundle bundle = m_state.bundles().get(id);
        if (null == bundle) {
            throw Gin.error(404, "bundle \"" + id + "\" not found");
        }
        return bundle;
    }

    private Response get(final Request _request) {
        synchronized (m_state) {
            return Response.json(200, requireBundle(_request).toView());
        }
    }

    /**
     * Handles {@code PUT /api/v1/catalog/bundles/:id}: replaces the archive of an existing bundle.
     *
     * @param _request the request
     * @return the updated bundle
     */
    private Response update(final Request _request) {
        synchronized (m_state) {
            final CatalogState.Bundle bundle = requireBundle(_request);
            final MultipartForm.Part file = uploadedFile(_request);
            final BundleArchive.Metadata metadata = inspect(file);
            if (!bundle.getCatalogId().equals(metadata.getId()) || !bundle.getCatalogType().equals(metadata.getType())) {
                throw Gin.error(422, "bundle identity mismatch: expected " + bundle.getCatalogType() + " \"" + bundle.getCatalogId() + "\"");
            }
            bundle.update(metadata, file.getData().length);
            return Response.json(200, bundle.toView()).withHeader("Location", "/api/v1/catalog/bundles/" + bundle.getId());
        }
    }

    private Response delete(final Request _request) {
        synchronized (m_state) {
            m_state.bundles().remove(requireBundle(_request).getId());
        }
        return Response.empty(204);
    }
}
