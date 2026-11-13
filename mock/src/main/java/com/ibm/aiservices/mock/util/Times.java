package com.ibm.aiservices.mock.util;

import java.time.Instant;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;

/**
 * Timestamp formatting in the exact styles the real services emit.
 */
public final class Times {

    /** Python {@code isoformat()} plus a {@code Z} suffix, e.g. {@code 2026-11-12T10:00:00.123456Z}. */
    private static final DateTimeFormatter ISO_MICROS_Z = DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss.SSSSSS'Z'").withZone(ZoneOffset.UTC);

    /** Python {@code isoformat()} on an aware UTC datetime, e.g. {@code 2026-11-12T10:00:00.123456+00:00}. */
    private static final DateTimeFormatter ISO_MICROS_OFFSET = DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss.SSSSSS'+00:00'").withZone(ZoneOffset.UTC);

    /** Go RFC 3339 with second precision, e.g. {@code 2026-11-12T10:00:00Z}. */
    private static final DateTimeFormatter RFC3339 = DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss'Z'").withZone(ZoneOffset.UTC);

    /** Human readable form used by the chatbot performance metrics. */
    private static final DateTimeFormatter READABLE = DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss").withZone(ZoneOffset.UTC);

    private Times() {
    }

    /**
     * Formats epoch milliseconds as ISO 8601 with microseconds and a {@code Z} suffix.
     *
     * @param _epochMillis the instant in epoch milliseconds
     * @return the formatted timestamp
     */
    public static String isoMicrosZ(final long _epochMillis) {
        return ISO_MICROS_Z.format(Instant.ofEpochMilli(_epochMillis));
    }

    /**
     * Formats epoch milliseconds as ISO 8601 with microseconds and a {@code +00:00} offset.
     *
     * @param _epochMillis the instant in epoch milliseconds
     * @return the formatted timestamp
     */
    public static String isoMicrosOffset(final long _epochMillis) {
        return ISO_MICROS_OFFSET.format(Instant.ofEpochMilli(_epochMillis));
    }

    /**
     * Formats epoch milliseconds as RFC 3339 with second precision.
     *
     * @param _epochMillis the instant in epoch milliseconds
     * @return the formatted timestamp
     */
    public static String rfc3339(final long _epochMillis) {
        return RFC3339.format(Instant.ofEpochMilli(_epochMillis));
    }

    /**
     * Formats epoch milliseconds as {@code yyyy-MM-dd HH:mm:ss} in UTC.
     *
     * @param _epochMillis the instant in epoch milliseconds
     * @return the formatted timestamp
     */
    public static String readable(final long _epochMillis) {
        return READABLE.format(Instant.ofEpochMilli(_epochMillis));
    }
}
