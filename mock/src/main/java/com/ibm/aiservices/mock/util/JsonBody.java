package com.ibm.aiservices.mock.util;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Typed read access to a parsed JSON request body.
 * <p>
 * Getters never throw on a type mismatch; they return the supplied default instead, so each service decides how to report missing or malformed fields in its own error format.
 */
public final class JsonBody {

    private final Map<String, Object> m_values;

    /**
     * Wraps a parsed JSON object.
     *
     * @param _values the parsed object; {@code null} is treated as an empty object
     */
    public JsonBody(final Map<String, Object> _values) {
        m_values = null == _values ? new LinkedHashMap<String, Object>() : _values;
    }

    /**
     * Returns whether the body contains the key with a non-null value.
     *
     * @param _key the field name
     * @return {@code true} if the field is present and not {@code null}
     */
    public boolean has(final String _key) {
        return null != m_values.get(_key);
    }

    /**
     * Returns the raw value of a field.
     *
     * @param _key the field name
     * @return the value, or {@code null}
     */
    public Object get(final String _key) {
        return m_values.get(_key);
    }

    /**
     * Returns a string field.
     *
     * @param _key the field name
     * @param _default the value returned when the field is missing or not a string
     * @return the string value or the default
     */
    public String getString(final String _key, final String _default) {
        final Object value = m_values.get(_key);
        return value instanceof String ? (String) value : _default;
    }

    /**
     * Returns an integral numeric field.
     *
     * @param _key the field name
     * @param _default the value returned when the field is missing or not a number
     * @return the numeric value or the default
     */
    public long getLong(final String _key, final long _default) {
        final Object value = m_values.get(_key);
        return value instanceof Number ? ((Number) value).longValue() : _default;
    }

    /**
     * Returns a floating point numeric field.
     *
     * @param _key the field name
     * @param _default the value returned when the field is missing or not a number
     * @return the numeric value or the default
     */
    public double getDouble(final String _key, final double _default) {
        final Object value = m_values.get(_key);
        return value instanceof Number ? ((Number) value).doubleValue() : _default;
    }

    /**
     * Returns a boolean field.
     *
     * @param _key the field name
     * @param _default the value returned when the field is missing or not a boolean
     * @return the boolean value or the default
     */
    public boolean getBoolean(final String _key, final boolean _default) {
        final Object value = m_values.get(_key);
        return value instanceof Boolean ? ((Boolean) value).booleanValue() : _default;
    }

    /**
     * Returns a nested object field.
     *
     * @param _key the field name
     * @return the nested object, or {@code null} if missing or not an object
     */
    @SuppressWarnings("unchecked")
    public Map<String, Object> getMap(final String _key) {
        final Object value = m_values.get(_key);
        return value instanceof Map ? (Map<String, Object>) value : null;
    }

    /**
     * Returns an array field.
     *
     * @param _key the field name
     * @return the array, or {@code null} if missing or not an array
     */
    @SuppressWarnings("unchecked")
    public List<Object> getList(final String _key) {
        final Object value = m_values.get(_key);
        return value instanceof List ? (List<Object>) value : null;
    }

    /**
     * Returns the underlying values.
     *
     * @return an unmodifiable view of the parsed object
     */
    public Map<String, Object> asMap() {
        return Collections.unmodifiableMap(m_values);
    }
}
