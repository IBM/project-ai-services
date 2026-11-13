package com.ibm.aiservices.mock.http;

import java.io.IOException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Set;
import java.util.TreeSet;
import java.util.logging.Level;
import java.util.logging.Logger;

import com.ibm.aiservices.mock.util.Ids;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;

/**
 * Dispatches requests to route handlers by method and path pattern.
 * <p>
 * Patterns are literal segments separated by {@code /}; a segment written {@code {name}} binds one decoded path segment, and a final segment written {@code {name*}} binds the remainder of the path. Trailing slashes are ignored on both patterns and requests.
 */
public final class Router implements HttpHandler {

    private static final Logger LOG = Logger.getLogger(Router.class.getName());

    private final String m_serviceName;
    private final ErrorResponder m_errors;
    private final List<Route> m_routes = new ArrayList<Route>();

    /**
     * A registered route.
     */
    public static final class Route {

        private final String m_method;
        private final String m_pattern;
        private final List<String> m_segments;
        private final RouteHandler m_handler;

        /**
         * Creates a route, pre-splitting its pattern into segments.
         *
         * @param _method the HTTP method
         * @param _pattern the path pattern
         * @param _handler the handler
         */
        Route(final String _method, final String _pattern, final RouteHandler _handler) {
            m_method = _method;
            m_pattern = _pattern;
            m_handler = _handler;
            final List<String> segments = new ArrayList<String>();
            for (final String segment : _pattern.split("/")) {
                if (!segment.isEmpty()) {
                    segments.add(segment);
                }
            }
            m_segments = segments;
        }

        /**
         * Returns the HTTP method.
         *
         * @return the method in upper case
         */
        public String getMethod() {
            return m_method;
        }

        /**
         * Returns the path pattern as registered.
         *
         * @return the pattern, e.g. {@code /v1/jobs/{job_id}}
         */
        public String getPattern() {
            return m_pattern;
        }

        /**
         * Matches the request path against this route and binds path parameters on success.
         *
         * @param _request the request whose path parameters are bound
         * @return {@code true} if the path matches this route's pattern
         */
        boolean matchPath(final Request _request) {
            final List<String> path = _request.getSegments();
            _request.clearPathParams();
            for (int i = 0; i < m_segments.size(); i++) {
                final String segment = m_segments.get(i);
                if (segment.startsWith("{") && segment.endsWith("*}")) {
                    final StringBuilder rest = new StringBuilder();
                    for (int j = i; j < path.size(); j++) {
                        rest.append('/').append(path.get(j));
                    }
                    _request.bindPathParam(segment.substring(1, segment.length() - 2), rest.toString());
                    return true;
                }
                if (i >= path.size()) {
                    return false;
                }
                if (segment.startsWith("{") && segment.endsWith("}")) {
                    _request.bindPathParam(segment.substring(1, segment.length() - 1), path.get(i));
                } else if (!segment.equals(path.get(i))) {
                    return false;
                }
            }
            return m_segments.size() == path.size();
        }
    }

    /**
     * Creates a router.
     *
     * @param _serviceName the service name used in log messages
     * @param _errors produces framework-level errors in the service's format
     */
    public Router(final String _serviceName, final ErrorResponder _errors) {
        m_serviceName = _serviceName;
        m_errors = _errors;
    }

    /**
     * Registers a route. Routes are matched in registration order, so register literal paths before overlapping parameterized ones.
     *
     * @param _method the HTTP method
     * @param _pattern the path pattern
     * @param _handler the handler
     * @return this router, for chaining
     */
    public Router add(final String _method, final String _pattern, final RouteHandler _handler) {
        m_routes.add(new Route(_method, _pattern, _handler));
        return this;
    }

    /**
     * Registers a {@code GET} route.
     *
     * @param _pattern the path pattern
     * @param _handler the handler
     * @return this router, for chaining
     */
    public Router get(final String _pattern, final RouteHandler _handler) {
        return add("GET", _pattern, _handler);
    }

    /**
     * Registers a {@code POST} route.
     *
     * @param _pattern the path pattern
     * @param _handler the handler
     * @return this router, for chaining
     */
    public Router post(final String _pattern, final RouteHandler _handler) {
        return add("POST", _pattern, _handler);
    }

    /**
     * Registers a {@code PUT} route.
     *
     * @param _pattern the path pattern
     * @param _handler the handler
     * @return this router, for chaining
     */
    public Router put(final String _pattern, final RouteHandler _handler) {
        return add("PUT", _pattern, _handler);
    }

    /**
     * Registers a {@code DELETE} route.
     *
     * @param _pattern the path pattern
     * @param _handler the handler
     * @return this router, for chaining
     */
    public Router delete(final String _pattern, final RouteHandler _handler) {
        return add("DELETE", _pattern, _handler);
    }

    /**
     * Returns the registered routes.
     *
     * @return an unmodifiable list of routes in registration order
     */
    public List<Route> getRoutes() {
        return Collections.unmodifiableList(m_routes);
    }

    /**
     * Handles one exchange: assigns a request ID, dispatches, converts exceptions to error responses and writes the result.
     *
     * @param _exchange the exchange
     * @throws IOException if writing the response fails
     */
    @Override
    public void handle(final HttpExchange _exchange) throws IOException {
        final long started = System.nanoTime();
        final String incomingId = _exchange.getRequestHeaders().getFirst("X-Request-ID");
        final String requestId = null == incomingId || incomingId.isEmpty() ? Ids.uuid() : incomingId;
        _exchange.getResponseHeaders().set("X-Request-ID", requestId);
        addCorsHeaders(_exchange);

        Request request = null;
        Response response;
        try {
            request = new Request(_exchange, requestId);
            response = dispatch(request);
        } catch (final ApiException e) {
            response = e.getResponse();
        } catch (final InvalidBodyException e) {
            LOG.log(Level.FINE, "[" + m_serviceName + "] Invalid request body: " + e.getMessage(), e);
            response = null == request ? Response.json(400, Collections.singletonMap("detail", e.getMessage())) : m_errors.invalidBody(request, e.getMessage());
        } catch (final RuntimeException e) {
            LOG.log(Level.SEVERE, "[" + m_serviceName + "] Unhandled error for " + _exchange.getRequestMethod() + " " + _exchange.getRequestURI().getRawPath(), e);
            response = null == request ? Response.json(500, Collections.singletonMap("detail", "Internal Server Error")) : m_errors.internalError(request);
        }

        try {
            response.send(_exchange, "HEAD".equalsIgnoreCase(_exchange.getRequestMethod()));
        } catch (final IOException e) {
            // Clients routinely abandon streaming responses; record it without failing the server thread.
            LOG.log(Level.FINE, "[" + m_serviceName + "] Client disconnected while writing response", e);
        }
        final long elapsedMillis = (System.nanoTime() - started) / 1000000L;
        LOG.info(String.format("[%s] %s %s -> %d (%d ms)", m_serviceName, _exchange.getRequestMethod(), _exchange.getRequestURI(), response.getStatus(), elapsedMillis));
    }

    /**
     * Finds and invokes the handler for a request.
     *
     * @param _request the request
     * @return the handler's response, or a not-found, method-not-allowed or preflight response
     */
    private Response dispatch(final Request _request) {
        final String method = "HEAD".equals(_request.getMethod()) ? "GET" : _request.getMethod();
        final Set<String> allowed = new TreeSet<String>();
        for (final Route route : m_routes) {
            if (route.matchPath(_request)) {
                if (route.getMethod().equals(method)) {
                    return route.m_handler.handle(_request);
                }
                allowed.add(route.getMethod());
            }
        }
        if ("OPTIONS".equals(method) && !allowed.isEmpty()) {
            return Response.empty(204).withHeader("Allow", joinMethods(allowed));
        }
        if (allowed.isEmpty()) {
            return m_errors.notFound(_request);
        }
        return m_errors.methodNotAllowed(_request, allowed).withHeader("Allow", joinMethods(allowed));
    }

    /**
     * Formats methods for an {@code Allow} header.
     *
     * @param _methods the methods
     * @return the comma separated list
     */
    private static String joinMethods(final Set<String> _methods) {
        final StringBuilder joined = new StringBuilder();
        for (final String allowedMethod : _methods) {
            if (0 < joined.length()) {
                joined.append(", ");
            }
            joined.append(allowedMethod);
        }
        return joined.toString();
    }

    /**
     * Adds permissive CORS headers so browser-based UIs on another origin can call the mock.
     *
     * @param _exchange the exchange
     */
    private static void addCorsHeaders(final HttpExchange _exchange) {
        final String origin = _exchange.getRequestHeaders().getFirst("Origin");
        _exchange.getResponseHeaders().set("Access-Control-Allow-Origin", null == origin ? "*" : origin);
        _exchange.getResponseHeaders().set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS");
        _exchange.getResponseHeaders().set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-Refresh-Token");
        _exchange.getResponseHeaders().set("Access-Control-Expose-Headers", String.join(", ", Arrays.asList("X-Request-ID", "X-Token-Exp", "X-Rephrased-Query", "X-Retrieve-Time", "X-Rerank-Time", "X-Total-Time", "Location", "Content-Disposition")));
        if (null != origin) {
            _exchange.getResponseHeaders().set("Access-Control-Allow-Credentials", "true");
            _exchange.getResponseHeaders().set("Vary", "Origin");
        }
    }
}
