package com.ibm.aiservices.mock.http;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Locale;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * A decoded {@code multipart/form-data} request body.
 */
public final class MultipartForm {

    private static final Pattern BOUNDARY_PATTERN = Pattern.compile("boundary=(?:\"([^\"]+)\"|([^;\\s]+))", Pattern.CASE_INSENSITIVE);
    private static final Pattern NAME_PATTERN = Pattern.compile("(?:^|;)\\s*name=\"([^\"]*)\"", Pattern.CASE_INSENSITIVE);
    private static final Pattern FILENAME_PATTERN = Pattern.compile("(?:^|;)\\s*filename=\"([^\"]*)\"", Pattern.CASE_INSENSITIVE);

    private final List<Part> m_parts;

    /**
     * One part of a multipart body: either a plain form field or an uploaded file.
     */
    public static final class Part {

        private final String m_name;
        private final String m_filename;
        private final String m_contentType;
        private final byte[] m_data;

        Part(final String _name, final String _filename, final String _contentType, final byte[] _data) {
            m_name = _name;
            m_filename = _filename;
            m_contentType = _contentType;
            m_data = _data;
        }

        /**
         * Returns the form field name.
         *
         * @return the field name
         */
        public String getName() {
            return m_name;
        }

        /**
         * Returns the uploaded file name.
         *
         * @return the file name, or {@code null} for a plain form field
         */
        public String getFilename() {
            return m_filename;
        }

        /**
         * Returns the declared content type of the part.
         *
         * @return the content type, or {@code null} if none was sent
         */
        public String getContentType() {
            return m_contentType;
        }

        /**
         * Returns the raw part content.
         *
         * @return the content bytes
         */
        public byte[] getData() {
            return m_data;
        }

        /**
         * Returns the part content decoded as UTF-8.
         *
         * @return the content as text
         */
        public String getText() {
            return new String(m_data, StandardCharsets.UTF_8);
        }

        /**
         * Returns whether this part is a file upload.
         *
         * @return {@code true} if the part carries a file name
         */
        public boolean isFile() {
            return null != m_filename;
        }
    }

    private MultipartForm(final List<Part> _parts) {
        m_parts = _parts;
    }

    /**
     * Decodes a multipart body.
     *
     * @param _contentType the request {@code Content-Type} header, which carries the boundary
     * @param _body the raw request body
     * @return the decoded form
     * @throws InvalidBodyException if the boundary is missing or the body is malformed
     */
    public static MultipartForm parse(final String _contentType, final byte[] _body) {
        final Matcher boundaryMatcher = BOUNDARY_PATTERN.matcher(null == _contentType ? "" : _contentType);
        if (!boundaryMatcher.find()) {
            throw new InvalidBodyException("Missing multipart boundary", null);
        }
        final String boundary = null != boundaryMatcher.group(1) ? boundaryMatcher.group(1) : boundaryMatcher.group(2);
        final byte[] delimiter = ("--" + boundary).getBytes(StandardCharsets.ISO_8859_1);
        final byte[] partSeparator = ("\r\n--" + boundary).getBytes(StandardCharsets.ISO_8859_1);
        final byte[] headerTerminator = "\r\n\r\n".getBytes(StandardCharsets.ISO_8859_1);

        final List<Part> parts = new ArrayList<Part>();
        int position = indexOf(_body, delimiter, 0);
        if (position < 0) {
            throw new InvalidBodyException("Multipart boundary not found in body", null);
        }
        position += delimiter.length;
        while (position + 1 < _body.length && !('-' == _body[position] && '-' == _body[position + 1])) {
            // Skip the CRLF that follows every delimiter line.
            position += 2;
            final int headerEnd = indexOf(_body, headerTerminator, position);
            if (headerEnd < 0) {
                throw new InvalidBodyException("Malformed multipart part headers", null);
            }
            final String headers = new String(_body, position, headerEnd - position, StandardCharsets.UTF_8);
            final int dataStart = headerEnd + headerTerminator.length;
            final int dataEnd = indexOf(_body, partSeparator, dataStart);
            if (dataEnd < 0) {
                throw new InvalidBodyException("Unterminated multipart part", null);
            }
            final byte[] data = new byte[dataEnd - dataStart];
            System.arraycopy(_body, dataStart, data, 0, data.length);
            parts.add(createPart(headers, data));
            position = dataEnd + partSeparator.length;
        }
        return new MultipartForm(parts);
    }

    /**
     * Builds a part from its raw header block.
     *
     * @param _headers the part headers, separated by CRLF
     * @param _data the part content
     * @return the part
     */
    private static Part createPart(final String _headers, final byte[] _data) {
        String name = "";
        String filename = null;
        String contentType = null;
        for (final String line : _headers.split("\r\n")) {
            final int colon = line.indexOf(':');
            if (colon <= 0) {
                continue;
            }
            final String headerName = line.substring(0, colon).trim().toLowerCase(Locale.ROOT);
            final String headerValue = line.substring(colon + 1).trim();
            if ("content-disposition".equals(headerName)) {
                final Matcher nameMatcher = NAME_PATTERN.matcher(headerValue);
                if (nameMatcher.find()) {
                    name = nameMatcher.group(1);
                }
                final Matcher filenameMatcher = FILENAME_PATTERN.matcher(headerValue);
                if (filenameMatcher.find()) {
                    filename = filenameMatcher.group(1);
                }
            } else if ("content-type".equals(headerName)) {
                contentType = headerValue;
            }
        }
        return new Part(name, filename, contentType, _data);
    }

    /**
     * Finds the first occurrence of a byte sequence.
     *
     * @param _haystack the bytes to search
     * @param _needle the sequence to find
     * @param _from the index to start searching from
     * @return the index of the first match, or {@code -1}
     */
    private static int indexOf(final byte[] _haystack, final byte[] _needle, final int _from) {
        final int last = _haystack.length - _needle.length;
        for (int i = Math.max(0, _from); i <= last; i++) {
            int j = 0;
            while (j < _needle.length && _haystack[i + j] == _needle[j]) {
                j++;
            }
            if (j == _needle.length) {
                return i;
            }
        }
        return -1;
    }

    /**
     * Returns the value of the first plain form field with the given name.
     *
     * @param _name the field name
     * @return the field value, or {@code null} if absent
     */
    public String getField(final String _name) {
        for (final Part part : m_parts) {
            if (!part.isFile() && _name.equals(part.getName())) {
                return part.getText();
            }
        }
        return null;
    }

    /**
     * Returns every file part uploaded under the given field name, in upload order.
     *
     * @param _name the field name
     * @return the file parts; empty if none
     */
    public List<Part> getFiles(final String _name) {
        final List<Part> files = new ArrayList<Part>();
        for (final Part part : m_parts) {
            if (part.isFile() && _name.equals(part.getName())) {
                files.add(part);
            }
        }
        return files;
    }

    /**
     * Returns the first file part uploaded under the given field name.
     *
     * @param _name the field name
     * @return the file part, or {@code null} if absent
     */
    public Part getFile(final String _name) {
        final List<Part> files = getFiles(_name);
        return files.isEmpty() ? null : files.get(0);
    }

    /**
     * Returns all parts.
     *
     * @return an unmodifiable list of every part in the body
     */
    public List<Part> getParts() {
        return Collections.unmodifiableList(m_parts);
    }
}
