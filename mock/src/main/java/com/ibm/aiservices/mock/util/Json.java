package com.ibm.aiservices.mock.util;

import java.lang.reflect.Type;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonParseException;
import com.google.gson.ToNumberPolicy;
import com.google.gson.reflect.TypeToken;

/**
 * JSON helpers shared by every mock service.
 * <p>
 * Response bodies are built from plain {@link Map} and {@link List} trees, which keeps the field names visible next to the code that produces them and keeps insertion order, so the wire output matches the real services field for field.
 */
public final class Json {

    /** Serializer that keeps null fields, because the real services emit explicit {@code null} values. */
    private static final Gson GSON = new GsonBuilder().serializeNulls().disableHtmlEscaping().setObjectToNumberStrategy(ToNumberPolicy.LONG_OR_DOUBLE).create();

    /** Pretty-printing variant used for downloadable result files. */
    private static final Gson GSON_PRETTY = new GsonBuilder().serializeNulls().disableHtmlEscaping().setPrettyPrinting().setObjectToNumberStrategy(ToNumberPolicy.LONG_OR_DOUBLE).create();

    private static final Type MAP_TYPE = new TypeToken<LinkedHashMap<String, Object>>() {
    }.getType();

    private Json() {
    }

    /**
     * Serializes a value to compact JSON.
     *
     * @param _value the value to serialize; may be {@code null}
     * @return the JSON text
     */
    public static String toJson(final Object _value) {
        return GSON.toJson(_value);
    }

    /**
     * Serializes a value to indented JSON.
     *
     * @param _value the value to serialize; may be {@code null}
     * @return the JSON text
     */
    public static String toPrettyJson(final Object _value) {
        return GSON_PRETTY.toJson(_value);
    }

    /**
     * Parses JSON text that must contain an object at the top level.
     * <p>
     * Integral numbers come back as {@link Long} and other numbers as {@link Double}, so a value echoed back to a client keeps its original form.
     *
     * @param _text the JSON text
     * @return the parsed object, never {@code null}
     * @throws JsonParseException if the text is not valid JSON or is not a JSON object
     */
    public static Map<String, Object> parseObject(final String _text) {
        final String trimmed = null == _text ? "" : _text.trim();
        if (!trimmed.startsWith("{")) {
            throw new JsonParseException("Expected a JSON object");
        }
        final Map<String, Object> parsed = GSON.fromJson(trimmed, MAP_TYPE);
        if (null == parsed) {
            throw new JsonParseException("Expected a JSON object");
        }
        return parsed;
    }

    /**
     * Builds an ordered map from alternating key and value arguments.
     *
     * @param _keysAndValues alternating {@code String} keys and values
     * @return a mutable, insertion-ordered map
     * @throws IllegalArgumentException if an odd number of arguments is passed or a key is not a string
     */
    public static Map<String, Object> obj(final Object... _keysAndValues) {
        if (0 != _keysAndValues.length % 2) {
            throw new IllegalArgumentException("Expected alternating keys and values");
        }
        final Map<String, Object> map = new LinkedHashMap<String, Object>();
        for (int i = 0; i < _keysAndValues.length; i += 2) {
            if (!(_keysAndValues[i] instanceof String)) {
                throw new IllegalArgumentException("Key at position " + i + " is not a string");
            }
            map.put((String) _keysAndValues[i], _keysAndValues[i + 1]);
        }
        return map;
    }

    /**
     * Builds a mutable list from the given items.
     *
     * @param _items the list items
     * @return a mutable list
     */
    public static List<Object> list(final Object... _items) {
        return new ArrayList<Object>(Arrays.asList(_items));
    }
}
