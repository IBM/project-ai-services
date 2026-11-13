package com.ibm.aiservices.mock.http;

/**
 * Handles a single matched route.
 */
public interface RouteHandler {

    /**
     * Handles the request.
     *
     * @param _request the request, with path parameters already bound
     * @return the response to send
     * @throws ApiException to abort with a prepared error response
     */
    Response handle(Request _request);
}
