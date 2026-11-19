package com.ibm.aiservices.mock.service.forecast;

import java.util.Set;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.ErrorResponder;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.service.common.FastApi;
import com.ibm.aiservices.mock.service.common.FastApiService;

/**
 * Mock of the forecast service ({@code services/forecast}).
 * <p>
 * The real service currently exposes only {@code GET /health}; connector and forecast routers are planned. It wraps every framework error, including 404 and 422, in its own {@code {"error": {...}}} envelope.
 */
public final class ForecastService extends FastApiService {

    private static final ErrorResponder ERRORS = new ErrorResponder() {

        @Override
        public Response notFound(final Request _request) {
            return FastApi.envelope(404, "RESOURCE_NOT_FOUND", "Not Found");
        }

        @Override
        public Response methodNotAllowed(final Request _request, final Set<String> _allowedMethods) {
            return FastApi.envelope(405, "METHOD_NOT_ALLOWED", "Method Not Allowed");
        }

        @Override
        public Response invalidBody(final Request _request, final String _detail) {
            return FastApi.envelope(422, "INVALID_REQUEST", "Request validation failed: body: " + _detail);
        }

        @Override
        public Response internalError(final Request _request) {
            return FastApi.envelope(500, "INTERNAL_SERVER_ERROR", "An unexpected internal server error occurred.");
        }
    };

    /**
     * Creates the service.
     *
     * @param _config the shared mock configuration
     */
    public ForecastService(final MockConfig _config) {
        super(_config);
    }

    @Override
    public String getName() {
        return "forecast";
    }

    @Override
    public String getTitle() {
        return "Forecast API";
    }

    @Override
    public int getDefaultPort() {
        return 8500;
    }

    @Override
    protected boolean hasRootPage() {
        return false;
    }

    @Override
    protected ErrorResponder getErrorResponder() {
        return ERRORS;
    }

    @Override
    protected void registerApiRoutes(final Router _router) {
        // TODO: Add connector and forecast routes once the real service implements them.
    }
}
