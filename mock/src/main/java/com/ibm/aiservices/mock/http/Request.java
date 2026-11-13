package com.ibm.aiservices.mock.http;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.UnsupportedEncodingException;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

import com.google.gson.JsonParseException;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;
import com.sun.net.httpserver.HttpExchange;

/**
 * An incoming HTTP request with decoded path segments, query parameters and lazily read body.
 */
public final class Request {

    /** Upper bound on accepted request bodies; uploads to the mock are not persisted, so this only guards memory. */
    private static final int MAX_BODY_BYTES = 64 * 1024 * 1024;

    private final HttpExchange m_exchange;
    private final String m_method;
    private final String m_path;
    private final List<String> m_segments;
    private final Map<String, List<String>> m_query;
    private final Map<String, String> m_pathParams = new LinkedHashMap<String, String>();
    private final String m_requestId;
    private byte[] m_body;
    private MultipartForm m_multipart;

    /**
     * Creates a request from an exchange.
     *
     * @param _exchange the exchange
     * @param _requestId the request ID echoed in the {@code X-Request-ID} response header
     */
    Request(final HttpExchange _exchange, final String _requestId) {
        m_exchange = _exchange;
        m_method = _exchange.getRequestMethod().toUpperCase(Locale.ROOT);
        m_requestId = _requestId;
        final String rawPath = _exchange.getRequestURI().getRawPath();
        m_segments = splitPath(null == rawPath ? "/" : rawPath);
        m_path = joinPath(m_segments);
        m_query = parseQuery(_exchange.getRequestURI().getRawQuery());
    }

    /**
     * Splits a raw path into decoded segments, dropping empty segments so trailing slashes are ignored.
     * <p>
     * Segments are decoded individually so that an encoded slash ({@code %2F}) stays inside its segment, which the catalog API relies on for model IDs such as {@code ibm-granite%2Fgranite-3.3-8b-instruct}.
     *
     * @param _rawPath the undecoded request path
     * @return the decoded segments
     */
    private static List<String> splitPath(final String _rawPath) {
        final List<String> segments = new ArrayList<String>();
        for (final String rawSegment : _rawPath.split("/")) {
            if (!rawSegment.isEmpty()) {
                // A literal '+' in a path is not a space, unlike in a query string.
                segments.add(decode(rawSegment.replace("+", "%2B")));
            }
        }
        return segments;
    }

    /**
     * Joins decoded segments back into a normalized path.
     *
     * @param _segments the path segments
     * @return the path, or {@code /} for no segments
     */
    private static String joinPath(final List<String> _segments) {
        final StringBuilder path = new StringBuilder();
        for (final String segment : _segments) {
            path.append('/').append(segment);
        }
        return 0 == path.length() ? "/" : path.toString();
    }

    /**
     * Parses a raw query string into a multi-valued map.
     *
     * @param _rawQuery the undecoded query string; may be {@code null}
     * @return the parameters in order of first appearance
     */
    private static Map<String, List<String>> parseQuery(final String _rawQuery) {
        final Map<String, List<String>> query = new LinkedHashMap<String, List<String>>();
        if (null == _rawQuery || _rawQuery.isEmpty()) {
            return query;
        }
        for (final String pair : _rawQuery.split("&")) {
            if (pair.isEmpty()) {
                continue;
            }
            final int equals = pair.indexOf('=');
            final String name = decode(equals < 0 ? pair : pair.substring(0, equals));
            final String value = equals < 0 ? "" : decode(pair.substring(equals + 1));
            List<String> values = query.get(name);
            if (null == values) {
                values = new ArrayList<String>();
                query.put(name, values);
            }
            values.add(value);
        }
        return query;
    }

    /**
     * Percent-decodes a URI component as UTF-8.
     *
     * @param _value the encoded text
     * @return the decoded text
     */
    private static String decode(final String _value) {
        try {
            return URLDecoder.decode(_value, "UTF-8");
        } catch (final UnsupportedEncodingException e) {
            // UTF-8 support is mandatory on every Java platform.
            throw new IllegalStateException(e);
        } catch (final IllegalArgumentException e) {
            throw new InvalidBodyException("Malformed percent-encoding in request URI", e);
        }
    }

    /**
     * Returns the request method in upper case.
     *
     * @return the method, e.g. {@code GET}
     */
    public String getMethod() {
        return m_method;
    }

    /**
     * Returns the decoded request path without a trailing slash.
     *
     * @return the path, e.g. {@code /v1/jobs/123}
     */
    public String getPath() {
        return m_path;
    }

    /**
     * Returns the decoded path segments.
     *
     * @return an unmodifiable list of segments
     */
    List<String> getSegments() {
        return Collections.unmodifiableList(m_segments);
    }

    /**
     * Returns the request ID assigned to this request.
     *
     * @return the request ID
     */
    public String getRequestId() {
        return m_requestId;
    }

    void bindPathParam(final String _name, final String _value) {
        m_pathParams.put(_name, _value);
    }

    void clearPathParams() {
        m_pathParams.clear();
    }

    /**
     * Returns a path parameter bound by the matched route pattern.
     *
     * @param _name the parameter name as written in the route pattern
     * @return the decoded value, or {@code null} if the route has no such parameter
     */
    public String getPathParam(final String _name) {
        return m_pathParams.get(_name);
    }

    /**
     * Returns the first value of a query parameter.
     *
     * @param _name the parameter name
     * @return the decoded value, or {@code null} if absent
     */
    public String getQuery(final String _name) {
        final List<String> values = m_query.get(_name);
        return null == values || values.isEmpty() ? null : values.get(0);
    }

    /**
     * Returns every value of a repeatable query parameter.
     *
     * @param _name the parameter name
     * @return the decoded values; empty if absent
     */
    public List<String> getQueryValues(final String _name) {
        final List<String> values = m_query.get(_name);
        return null == values ? Collections.<String>emptyList() : Collections.unmodifiableList(values);
    }

    /**
     * Returns whether a query parameter was supplied.
     *
     * @param _name the parameter name
     * @return {@code true} if the parameter appears at least once
     */
    public boolean hasQuery(final String _name) {
        return m_query.containsKey(_name);
    }

    /**
     * Returns a request header.
     *
     * @param _name the header name, case-insensitive
     * @return the first header value, or {@code null} if absent
     */
    public String getHeader(final String _name) {
        return m_exchange.getRequestHeaders().getFirst(_name);
    }

    /**
     * Returns the bearer token from the {@code Authorization} header.
     *
     * @return the token, possibly empty, or {@code null} if the header is missing or is not a bearer credential
     */
    public String getBearerToken() {
        final String authorization = getHeader("Authorization");
        if (null == authorization || !authorization.startsWith("Bearer ")) {
            return null;
        }
        return authorization.substring("Bearer ".length()).trim();
    }

    /**
     * Returns whether the request declares the given media type in its {@code Content-Type} header.
     *
     * @param _mediaType the media type, e.g. {@code multipart/form-data}
     * @return {@code true} if the content type starts with the media type
     */
    public boolean hasContentType(final String _mediaType) {
        final String contentType = getHeader("Content-Type");
        return null != contentType && contentType.toLowerCase(Locale.ROOT).startsWith(_mediaType);
    }

    /**
     * Returns the raw request body, reading it on first use.
     *
     * @return the body bytes; empty if there is no body
     * @throws InvalidBodyException if the body cannot be read or exceeds the size limit
     */
    public byte[] getBody() {
        if (null == m_body) {
            m_body = readBody(m_exchange.getRequestBody());
        }
        return m_body;
    }

    /**
     * Reads a body stream fully, enforcing {@link #MAX_BODY_BYTES}.
     *
     * @param _in the body stream
     * @return the body bytes
     */
    private static byte[] readBody(final InputStream _in) {
        final ByteArrayOutputStream buffer = new ByteArrayOutputStream();
        final byte[] chunk = new byte[8192];
        try {
            int read = _in.read(chunk);
            while (-1 != read) {
                buffer.write(chunk, 0, read);
                if (buffer.size() > MAX_BODY_BYTES) {
                    throw new InvalidBodyException("Request body exceeds " + MAX_BODY_BYTES + " bytes", null);
                }
                read = _in.read(chunk);
            }
        } catch (final IOException e) {
            throw new InvalidBodyException("Failed to read request body", e);
        }
        return buffer.toByteArray();
    }

    /**
     * Returns the body decoded as UTF-8 text.
     *
     * @return the body text
     */
    public String getBodyText() {
        return new String(getBody(), StandardCharsets.UTF_8);
    }

    /**
     * Returns whether the request has a non-blank body.
     *
     * @return {@code true} if the body contains anything other than whitespace
     */
    public boolean hasBody() {
        return !getBodyText().trim().isEmpty();
    }

    /**
     * Decodes the body as a JSON object.
     *
     * @return the parsed body
     * @throws InvalidBodyException if the body is not a JSON object
     */
    public JsonBody getJson() {
        try {
            return new JsonBody(Json.parseObject(getBodyText()));
        } catch (final JsonParseException e) {
            throw new InvalidBodyException("Request body is not a valid JSON object", e);
        }
    }

    /**
     * Decodes the body as {@code multipart/form-data}.
     *
     * @return the decoded form
     * @throws InvalidBodyException if the body is not valid multipart content
     */
    public MultipartForm getMultipart() {
        if (null == m_multipart) {
            m_multipart = MultipartForm.parse(getHeader("Content-Type"), getBody());
        }
        return m_multipart;
    }
}
