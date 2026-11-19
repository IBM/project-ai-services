package com.ibm.aiservices.mock.service.extract;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import com.ibm.aiservices.mock.util.Json;

/**
 * JSON Schema inference and mock extraction used by the extract service.
 */
final class SchemaSupport {

    private SchemaSupport() {
    }

    /**
     * Infers a JSON Schema from an example output object.
     *
     * @param _example the example output
     * @return a draft 2020-12 object schema requiring every example key
     */
    static Map<String, Object> infer(final Map<String, Object> _example) {
        final Map<String, Object> properties = new LinkedHashMap<String, Object>();
        for (final Map.Entry<String, Object> entry : _example.entrySet()) {
            properties.put(entry.getKey(), inferValue(entry.getValue()));
        }
        return Json.obj("type", "object", "properties", properties, "required", new ArrayList<Object>(_example.keySet()));
    }

    /**
     * Infers the schema of a single example value.
     *
     * @param _value the example value
     * @return the value schema
     */
    @SuppressWarnings("unchecked")
    private static Map<String, Object> inferValue(final Object _value) {
        if (_value instanceof Map) {
            return infer((Map<String, Object>) _value);
        }
        if (_value instanceof List) {
            final List<Object> items = (List<Object>) _value;
            return items.isEmpty() ? Json.obj("type", "array") : Json.obj("type", "array", "items", inferValue(items.get(0)));
        }
        if (_value instanceof Boolean) {
            return Json.obj("type", "boolean");
        }
        if (_value instanceof Long || _value instanceof Integer) {
            return Json.obj("type", "integer");
        }
        if (_value instanceof Number) {
            return Json.obj("type", "number");
        }
        return Json.obj("type", "string");
    }

    /**
     * Produces a mock extraction that conforms to a schema, pulling {@code field: value} pairs from the text where present.
     *
     * @param _schema the object schema
     * @param _text the source text
     * @return the extraction object
     */
    @SuppressWarnings("unchecked")
    static Map<String, Object> extract(final Map<String, Object> _schema, final String _text) {
        final Map<String, Object> result = new LinkedHashMap<String, Object>();
        final Object properties = _schema.get("properties");
        if (!(properties instanceof Map)) {
            return result;
        }
        for (final Map.Entry<String, Object> property : ((Map<String, Object>) properties).entrySet()) {
            final Map<String, Object> propertySchema = property.getValue() instanceof Map ? (Map<String, Object>) property.getValue() : Json.obj();
            result.put(property.getKey(), valueFor(property.getKey(), propertySchema, _text));
        }
        return result;
    }

    /**
     * Produces a mock value for one property.
     *
     * @param _name the property name
     * @param _schema the property schema
     * @param _text the source text
     * @return a value of the schema's type
     */
    private static Object valueFor(final String _name, final Map<String, Object> _schema, final String _text) {
        final String type = String.valueOf(_schema.get("type"));
        if ("object".equals(type)) {
            return extract(_schema, _text);
        }
        if ("array".equals(type)) {
            return Json.list();
        }
        if ("boolean".equals(type)) {
            return Boolean.FALSE;
        }
        final String found = findInText(_name, _text);
        if ("integer".equals(type) || "number".equals(type)) {
            final Matcher number = Pattern.compile("-?\\d+(?:\\.\\d+)?").matcher(null == found ? "" : found);
            if (!number.find()) {
                return "integer".equals(type) ? (Object) Long.valueOf(0L) : (Object) Double.valueOf(0.0);
            }
            return "integer".equals(type) ? (Object) Long.valueOf((long) Double.parseDouble(number.group())) : (Object) Double.valueOf(number.group());
        }
        return null == found ? "mock " + _name.replace('_', ' ') : found;
    }

    /**
     * Looks for a {@code label: value} or {@code label #value} occurrence of a property in the text.
     *
     * @param _name the property name; underscores match any separator
     * @param _text the text
     * @return the trimmed value, or {@code null} if not found
     */
    private static String findInText(final String _name, final String _text) {
        final String label = Pattern.quote(_name.toLowerCase(Locale.ROOT)).replace("_", "\\E[\\s_-]*\\Q");
        final Matcher matcher = Pattern.compile("(?i)" + label + "\\s*(?:number|no\\.?)?\\s*[:#=]\\s*([^\\n,;]+)").matcher(_text);
        return matcher.find() ? matcher.group(1).trim() : null;
    }
}
