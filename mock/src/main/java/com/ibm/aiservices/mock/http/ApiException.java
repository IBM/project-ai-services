package com.ibm.aiservices.mock.http;

/**
 * Unchecked exception carrying a complete error {@link Response}.
 * <p>
 * Route handlers throw this to abort processing with a service-specific error body; the {@link Router} catches it and sends the attached response unchanged.
 */
public final class ApiException extends RuntimeException {

    private static final long serialVersionUID = 1L;

    private final transient Response m_response;

    /**
     * Creates an exception for the given error response.
     *
     * @param _response the response to send to the client
     */
    public ApiException(final Response _response) {
        super("HTTP " + _response.getStatus());
        m_response = _response;
    }

    /**
     * Returns the response to send to the client.
     *
     * @return the error response
     */
    public Response getResponse() {
        return m_response;
    }
}
