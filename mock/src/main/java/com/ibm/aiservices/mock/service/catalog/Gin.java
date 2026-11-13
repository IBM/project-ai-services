package com.ibm.aiservices.mock.service.catalog;

import java.util.Set;

import com.ibm.aiservices.mock.http.ApiException;
import com.ibm.aiservices.mock.http.ErrorResponder;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.util.Json;

/**
 * Reproduces the conventions of the Go gin based catalog API server: the {@code {"error": "..."}} body, gin's plain text 404 page, binding error messages and page-number pagination.
 */
final class Gin {

    /** Gin returns its plain text 404 page for unknown routes, and also for wrong methods because {@code HandleMethodNotAllowed} is off. */
    static final ErrorResponder ERRORS = new ErrorResponder() {

        @Override
        public Response notFound(final Request _request) {
            return Response.text(404, "text/plain", "404 page not found");
        }

        @Override
        public Response methodNotAllowed(final Request _request, final Set<String> _allowedMethods) {
            return Response.text(404, "text/plain", "404 page not found");
        }

        @Override
        public Response invalidBody(final Request _request, final String _detail) {
            return Response.json(400, Json.obj("error", "invalid payload"));
        }

        @Override
        public Response internalError(final Request _request) {
            return Response.json(500, Json.obj("error", "internal server error"));
        }
    };

    private static final int DEFAULT_PAGE_SIZE = 20;
    private static final int MAX_PAGE_SIZE = 100;

    private Gin() {
    }

    /**
     * A parsed {@code page} and {@code page_size} pair.
     */
    static final class PageRequest {

        private final int m_page;
        private final int m_pageSize;

        PageRequest(final int _page, final int _pageSize) {
            m_page = _page;
            m_pageSize = _pageSize;
        }

        int getPage() {
            return m_page;
        }

        int getPageSize() {
            return m_pageSize;
        }

        int getOffset() {
            return (m_page - 1) * m_pageSize;
        }
    }

    /**
     * Creates an exception carrying an {@code {"error": message}} body.
     *
     * @param _status the HTTP status
     * @param _message the message
     * @return the exception to throw
     */
    static ApiException error(final int _status, final String _message) {
        return new ApiException(Response.json(_status, Json.obj("error", _message)));
    }

    /**
     * Creates the 400 exception gin's validator produces for a failed struct tag.
     *
     * @param _struct the request struct name
     * @param _field the Go field name
     * @param _tag the failed validation tag, e.g. {@code required}
     * @return the exception to throw
     */
    static ApiException bindingError(final String _struct, final String _field, final String _tag) {
        return error(400, "Key: '" + _struct + "." + _field + "' Error:Field validation for '" + _field + "' failed on the '" + _tag + "' tag");
    }

    /**
     * Parses the pagination parameters. Missing, non-numeric or zero values fall back to the defaults.
     *
     * @param _request the request
     * @return the page request
     */
    static PageRequest page(final Request _request) {
        final int page = parseOrZero(_request.getQuery("page"));
        final int pageSize = parseOrZero(_request.getQuery("page_size"));
        if (page < 0) {
            throw error(400, "invalid page parameter: must be a positive integer");
        }
        if (pageSize < 0 || pageSize > MAX_PAGE_SIZE) {
            throw error(400, "invalid page_size parameter: must be between 1 and 100");
        }
        return new PageRequest(0 == page ? 1 : page, 0 == pageSize ? DEFAULT_PAGE_SIZE : pageSize);
    }

    /**
     * Parses an integer query value, returning zero when absent or malformed.
     *
     * @param _value the raw value
     * @return the value, or {@code 0}
     */
    private static int parseOrZero(final String _value) {
        // Non-numeric values select the default, matching strconv.Atoi failures in the real handlers.
        if (null == _value || !_value.trim().matches("-?\\d{1,9}")) {
            return 0;
        }
        return Integer.parseInt(_value.trim());
    }

    /**
     * Resolves the {@code runtime} query parameter.
     *
     * @param _request the request
     * @param _default the runtime used when the parameter is absent
     * @return {@code podman} or {@code openshift}
     */
    static String runtime(final Request _request, final String _default) {
        final String runtime = _request.getQuery("runtime");
        if (null == runtime || runtime.isEmpty()) {
            return _default;
        }
        if (!"podman".equals(runtime) && !"openshift".equals(runtime)) {
            throw error(400, "invalid runtime \"" + runtime + "\": must be \"podman\" or \"openshift\"");
        }
        return runtime;
    }
}
