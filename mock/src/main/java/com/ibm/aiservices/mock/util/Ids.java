package com.ibm.aiservices.mock.util;

import java.util.UUID;
import java.util.regex.Pattern;

/**
 * Identifier helpers.
 */
public final class Ids {

    private static final Pattern UUID_PATTERN = Pattern.compile("^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$");

    private Ids() {
    }

    /**
     * Generates a random UUID string.
     *
     * @return a new version 4 UUID in canonical form
     */
    public static String uuid() {
        return UUID.randomUUID().toString();
    }

    /**
     * Returns whether a string is a canonical UUID.
     *
     * @param _candidate the string to check; may be {@code null}
     * @return {@code true} if the string is a UUID in canonical 36 character form
     */
    public static boolean isUuid(final String _candidate) {
        return null != _candidate && UUID_PATTERN.matcher(_candidate).matches();
    }
}
