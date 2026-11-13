package com.ibm.aiservices.mock.http;

/**
 * Thrown when a request body cannot be decoded as the expected format (JSON or multipart).
 * <p>
 * The {@link Router} converts it into the owning service's error format through {@link ErrorResponder#invalidBody(Request, String)}.
 */
public final class InvalidBodyException extends RuntimeException {

    private static final long serialVersionUID = 1L;

    /**
     * Creates the exception.
     *
     * @param _message a description of why the body is invalid
     * @param _cause the underlying parse failure; may be {@code null}
     */
    public InvalidBodyException(final String _message, final Throwable _cause) {
        super(_message, _cause);
    }
}
