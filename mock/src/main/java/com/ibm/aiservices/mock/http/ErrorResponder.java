package com.ibm.aiservices.mock.http;

import java.util.Set;

/**
 * Produces framework-level error responses in the format of a particular service.
 * <p>
 * The real services disagree on error shapes (FastAPI, Starlette, gin and custom envelopes), so each mock service supplies its own implementation.
 */
public interface ErrorResponder {

    /**
     * Builds the response for a path that matches no route.
     *
     * @param _request the request
     * @return the error response
     */
    Response notFound(Request _request);

    /**
     * Builds the response for a path that exists but does not accept the request method.
     *
     * @param _request the request
     * @param _allowedMethods the methods the path does accept
     * @return the error response
     */
    Response methodNotAllowed(Request _request, Set<String> _allowedMethods);

    /**
     * Builds the response for a request body that could not be decoded.
     *
     * @param _request the request
     * @param _detail a description of the problem
     * @return the error response
     */
    Response invalidBody(Request _request, String _detail);

    /**
     * Builds the response for an unexpected failure inside a handler.
     *
     * @param _request the request
     * @return the error response
     */
    Response internalError(Request _request);
}
