package com.ibm.aiservices.mock.service.common;

/**
 * Error codes, default HTTP statuses and default messages from {@code services/common/error_utils.py}.
 */
public enum ErrorCode {

    INVALID_REQUEST(400, "Request validation failed"),
    MISSING_INPUT(400, "Required input is missing"),
    EMPTY_INPUT(400, "Input cannot be empty"),
    INVALID_PARAMETER(400, "Invalid parameter value"),
    INVALID_SCHEMA(400, "Schema is invalid"),
    DUPLICATE_FILE(400, "Duplicate file in request"),
    INPUT_TEXT_SMALLER_THAN_SUMMARY_LENGTH(400, "Input text is smaller than the requested summary length"),
    AUTHENTICATION_FAILED(401, "Authentication failed"),
    FORBIDDEN(403, "Access forbidden"),
    RESOURCE_NOT_FOUND(404, "The requested resource was not found"),
    METHOD_NOT_ALLOWED(405, "This operation is not allowed for this resource"),
    RESOURCE_LOCKED(409, "Resource is locked by an active operation"),
    RESOURCE_CONFLICT(409, "A resource with that identifier already exists"),
    REQUEST_TOO_LARGE(413, "Request body exceeds the maximum allowed size"),
    CONTEXT_LIMIT_EXCEEDED(413, "Input size exceeds maximum token limit"),
    UNSUPPORTED_MEDIA_TYPE(415, "File format not supported"),
    UNSUPPORTED_FILE_TYPE(415, "File type not supported"),
    UNSUPPORTED_CONTENT_TYPE(415, "Content type not supported"),
    INVALID_FILE_CONTENT(415, "File content is invalid"),
    JOB_FAILED(422, "The job completed with a failure status"),
    EXTRACTION_VALIDATION_FAILED(422, "Extraction output failed schema validation"),
    RATE_LIMIT_EXCEEDED(429, "Too many requests"),
    SERVER_BUSY(429, "Server is busy. Please try again later"),
    INTERNAL_SERVER_ERROR(500, "An unexpected error occurred"),
    LLM_ERROR(500, "Failed to generate response. Please try again later"),
    FILE_STAGING_ERROR(500, "Failed to stage uploaded file"),
    DATABASE_ERROR(500, "Database operation failed"),
    LLM_UNAVAILABLE(503, "LLM service is unavailable. Please try again later"),
    VECTOR_STORE_NOT_READY(503, "Vector store not initialized"),
    INSUFFICIENT_STORAGE(507, "Insufficient storage");

    private final int m_status;
    private final String m_defaultMessage;

    ErrorCode(final int _status, final String _defaultMessage) {
        m_status = _status;
        m_defaultMessage = _defaultMessage;
    }

    /**
     * Returns the HTTP status associated with the code.
     *
     * @return the status code
     */
    public int getStatus() {
        return m_status;
    }

    /**
     * Returns the default message that prefixes every detail.
     *
     * @return the default message
     */
    public String getDefaultMessage() {
        return m_defaultMessage;
    }
}
