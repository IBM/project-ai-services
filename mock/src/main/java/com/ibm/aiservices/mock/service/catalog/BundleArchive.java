package com.ibm.aiservices.mock.service.catalog;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.zip.GZIPInputStream;

/**
 * Reads the {@code metadata.yaml} of an uploaded catalog bundle ({@code .tar.gz}).
 */
final class BundleArchive {

    private static final int BLOCK = 512;
    private static final long MAX_UNCOMPRESSED_BYTES = 200L * 1024L * 1024L;

    private BundleArchive() {
    }

    /**
     * Identity of a bundle, taken from its top-level {@code metadata.yaml}.
     */
    static final class Metadata {

        private final String m_id;
        private final String m_name;
        private final String m_version;
        private final String m_type;

        Metadata(final String _id, final String _name, final String _version, final String _type) {
            m_id = _id;
            m_name = _name;
            m_version = _version;
            m_type = _type;
        }

        String getId() {
            return m_id;
        }

        String getName() {
            return m_name;
        }

        String getVersion() {
            return m_version;
        }

        String getType() {
            return m_type;
        }
    }

    /**
     * Thrown when an archive is not a valid catalog bundle.
     */
    static final class InvalidBundleException extends Exception {

        private static final long serialVersionUID = 1L;

        InvalidBundleException(final String _message, final Throwable _cause) {
            super(_message, _cause);
        }
    }

    /**
     * Extracts bundle metadata from a gzip-compressed tar archive.
     *
     * @param _archive the archive bytes
     * @return the metadata
     * @throws InvalidBundleException if the archive is unreadable or lacks a valid {@code metadata.yaml}
     */
    static Metadata read(final byte[] _archive) throws InvalidBundleException {
        final String yaml;
        try {
            yaml = findMetadata(untar(new GZIPInputStream(new ByteArrayInputStream(_archive))));
        } catch (final IOException e) {
            throw new InvalidBundleException("invalid bundle archive: " + e.getMessage(), e);
        }
        if (null == yaml) {
            throw new InvalidBundleException("invalid bundle: metadata.yaml not found at archive root", null);
        }
        final String id = yamlValue(yaml, "id");
        final String version = yamlValue(yaml, "version");
        final String type = yamlValue(yaml, "type");
        if (null == id || null == version || null == type) {
            throw new InvalidBundleException("invalid bundle: metadata.yaml must define id, version and type", null);
        }
        if (!"service".equals(type) && !"architecture".equals(type)) {
            throw new InvalidBundleException("invalid bundle: unsupported catalog type \"" + type + "\"", null);
        }
        return new Metadata(id, yamlValue(yaml, "name"), version, type);
    }

    /**
     * Decompresses a stream fully, bounded by {@link #MAX_UNCOMPRESSED_BYTES}.
     *
     * @param _in the decompressing stream
     * @return the uncompressed bytes
     */
    private static byte[] untar(final InputStream _in) throws IOException {
        final ByteArrayOutputStream out = new ByteArrayOutputStream();
        final byte[] buffer = new byte[8192];
        int read = _in.read(buffer);
        while (-1 != read) {
            out.write(buffer, 0, read);
            if (out.size() > MAX_UNCOMPRESSED_BYTES) {
                throw new IOException("archive expands beyond " + MAX_UNCOMPRESSED_BYTES + " bytes");
            }
            read = _in.read(buffer);
        }
        return out.toByteArray();
    }

    /**
     * Walks tar headers looking for the shallowest {@code metadata.yaml}.
     *
     * @param _tar the uncompressed tar bytes
     * @return the file content, or {@code null} if absent
     * @throws IOException if a header is malformed
     */
    private static String findMetadata(final byte[] _tar) throws IOException {
        String best = null;
        int bestDepth = Integer.MAX_VALUE;
        int offset = 0;
        while (offset + BLOCK <= _tar.length && 0 != _tar[offset]) {
            final String name = cString(_tar, offset, 100);
            final String prefix = cString(_tar, offset + 345, 155);
            final String sizeField = cString(_tar, offset + 124, 12).trim();
            final long size;
            try {
                size = sizeField.isEmpty() ? 0L : Long.parseLong(sizeField, 8);
            } catch (final NumberFormatException e) {
                throw new IOException("malformed tar header for '" + name + "'", e);
            }
            final String path = (prefix.isEmpty() ? name : prefix + "/" + name).replaceFirst("^\\./", "");
            final int dataStart = offset + BLOCK;
            if (("metadata.yaml".equals(path) || path.endsWith("/metadata.yaml")) && dataStart + size <= _tar.length) {
                final int depth = path.split("/").length;
                if (depth < bestDepth) {
                    best = new String(_tar, dataStart, (int) size, StandardCharsets.UTF_8);
                    bestDepth = depth;
                }
            }
            offset = dataStart + (int) ((size + BLOCK - 1) / BLOCK) * BLOCK;
        }
        return 2 >= bestDepth ? best : null;
    }

    /**
     * Reads a NUL terminated field from a tar header.
     *
     * @param _data the header bytes
     * @param _offset the field offset
     * @param _length the field length
     * @return the field text
     */
    private static String cString(final byte[] _data, final int _offset, final int _length) {
        int end = _offset;
        while (end < _offset + _length && 0 != _data[end]) {
            end++;
        }
        return new String(_data, _offset, end - _offset, StandardCharsets.UTF_8);
    }

    /**
     * Reads a top-level scalar from simple YAML, stripping quotes and trailing comments.
     *
     * @param _yaml the YAML text
     * @param _key the key
     * @return the value, or {@code null} if absent
     */
    private static String yamlValue(final String _yaml, final String _key) {
        for (final String line : _yaml.split("\r?\n")) {
            if (line.startsWith(_key + ":")) {
                String value = line.substring(_key.length() + 1).trim();
                if (value.startsWith("\"") || value.startsWith("'")) {
                    final int close = value.indexOf(value.charAt(0), 1);
                    value = close > 0 ? value.substring(1, close) : value.substring(1);
                } else if (value.contains(" #")) {
                    value = value.substring(0, value.indexOf(" #")).trim();
                }
                return value.isEmpty() ? null : value;
            }
        }
        return null;
    }
}
