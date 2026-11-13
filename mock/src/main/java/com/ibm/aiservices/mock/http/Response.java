package com.ibm.aiservices.mock.http;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.LinkedHashMap;
import java.util.Map;

import com.ibm.aiservices.mock.util.Json;
import com.sun.net.httpserver.HttpExchange;

/**
 * An HTTP response: status, headers and either a fixed body or a streamed body.
 */
public final class Response {

    /** Content type used for JSON bodies. */
    public static final String JSON_CONTENT_TYPE = "application/json";

    /** Content type used for server-sent event streams. */
    public static final String SSE_CONTENT_TYPE = "text/event-stream";

    private final int m_status;
    private final Map<String, String> m_headers = new LinkedHashMap<String, String>();
    private final byte[] m_body;
    private final StreamBody m_stream;

    /**
     * Writes a response body incrementally, for example server-sent events.
     */
    public interface StreamBody {

        /**
         * Writes the body. Implementations should flush after each logical chunk.
         *
         * @param _out the response body stream
         * @throws IOException if the client disconnects or the write fails
         */
        void writeTo(OutputStream _out) throws IOException;
    }

    /**
     * Creates a response; use the static factory methods.
     *
     * @param _status the HTTP status
     * @param _contentType the content type, or {@code null} for none
     * @param _body the fixed body, or {@code null}
     * @param _stream the streamed body, or {@code null}
     */
    private Response(final int _status, final String _contentType, final byte[] _body, final StreamBody _stream) {
        m_status = _status;
        m_body = _body;
        m_stream = _stream;
        if (null != _contentType) {
            m_headers.put("Content-Type", _contentType);
        }
    }

    /**
     * Creates a JSON response.
     *
     * @param _status the HTTP status code
     * @param _body the value to serialize as JSON
     * @return the response
     */
    public static Response json(final int _status, final Object _body) {
        return new Response(_status, JSON_CONTENT_TYPE, Json.toJson(_body).getBytes(StandardCharsets.UTF_8), null);
    }

    /**
     * Creates a response with a text body.
     *
     * @param _status the HTTP status code
     * @param _contentType the content type, including any charset parameter
     * @param _body the body text, encoded as UTF-8
     * @return the response
     */
    public static Response text(final int _status, final String _contentType, final String _body) {
        return new Response(_status, _contentType, _body.getBytes(StandardCharsets.UTF_8), null);
    }

    /**
     * Creates a response without a body, for example {@code 204 No Content}.
     *
     * @param _status the HTTP status code
     * @return the response
     */
    public static Response empty(final int _status) {
        return new Response(_status, null, null, null);
    }

    /**
     * Creates a streamed response whose body is written after the headers are sent.
     *
     * @param _status the HTTP status code
     * @param _contentType the content type
     * @param _stream the body writer
     * @return the response
     */
    public static Response stream(final int _status, final String _contentType, final StreamBody _stream) {
        return new Response(_status, _contentType, null, _stream);
    }

    /**
     * Adds or replaces a response header.
     *
     * @param _name the header name
     * @param _value the header value
     * @return this response, for chaining
     */
    public Response withHeader(final String _name, final String _value) {
        m_headers.put(_name, _value);
        return this;
    }

    /**
     * Returns the HTTP status code.
     *
     * @return the status code
     */
    public int getStatus() {
        return m_status;
    }

    /**
     * Writes this response to the exchange and closes it.
     *
     * @param _exchange the exchange to respond to
     * @param _isHead whether the request was a {@code HEAD} request, in which case no body is written
     * @throws IOException if writing to the client fails
     */
    void send(final HttpExchange _exchange, final boolean _isHead) throws IOException {
        for (final Map.Entry<String, String> header : m_headers.entrySet()) {
            _exchange.getResponseHeaders().set(header.getKey(), header.getValue());
        }
        try {
            if (null != m_stream && !_isHead) {
                // Length 0 selects chunked transfer encoding, so events reach the client as they are written.
                _exchange.sendResponseHeaders(m_status, 0);
                final OutputStream out = _exchange.getResponseBody();
                m_stream.writeTo(out);
                out.flush();
            } else if (null == m_body || 0 == m_body.length || _isHead || 204 == m_status) {
                _exchange.sendResponseHeaders(m_status, -1);
            } else {
                _exchange.sendResponseHeaders(m_status, m_body.length);
                final OutputStream out = _exchange.getResponseBody();
                out.write(m_body);
                out.flush();
            }
        } finally {
            _exchange.close();
        }
    }
}
