package com.ibm.aiservices.mock.service.common;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.ErrorResponder;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.RouteHandler;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.MockService;
import com.ibm.aiservices.mock.util.Json;

/**
 * Base class for the Python FastAPI services. Registers the routes every one of them exposes: the Swagger UI pages, {@code /openapi.json} and {@code /health}.
 */
public abstract class FastApiService extends MockService {

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    protected FastApiService(final MockConfig _config) {
        super(_config);
    }

    /**
     * Registers the service-specific routes.
     *
     * @param _router the router
     */
    protected abstract void registerApiRoutes(Router _router);

    /**
     * Returns whether the service serves Swagger UI at {@code /}. Every service except forecast does.
     *
     * @return {@code true} to register the root page
     */
    protected boolean hasRootPage() {
        return true;
    }

    /**
     * Returns route prefixes the real service hides from its OpenAPI schema.
     *
     * @return the hidden prefixes
     */
    protected String[] hiddenFromSchema() {
        return new String[0];
    }

    /**
     * Returns the error responder; FastAPI services default to Starlette's {@code {"detail": ...}} bodies for unknown routes.
     *
     * @return the error responder
     */
    @Override
    protected ErrorResponder getErrorResponder() {
        return FastApi.STARLETTE_ERRORS;
    }

    /**
     * Registers the documentation and health routes, then the service routes.
     *
     * @param _router the router
     */
    @Override
    protected final void registerRoutes(final Router _router) {
        final String title = getTitle();
        final RouteHandler swagger = _request -> FastApi.swaggerUi(title, "/openapi.json");
        if (hasRootPage()) {
            _router.get("/", swagger);
        }
        _router.get("/docs", swagger);
        _router.get("/redoc", swagger);
        _router.get("/openapi.json", _request -> Response.json(200, OpenApiDoc.build(title, "1.0.0", _router, hiddenFromSchema())));
        _router.get("/health", _request -> Response.json(200, Json.obj("status", "ok")));
        registerApiRoutes(_router);
    }
}
