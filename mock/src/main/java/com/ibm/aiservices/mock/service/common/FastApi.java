package com.ibm.aiservices.mock.service.common;

import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

import com.ibm.aiservices.mock.http.ApiException;
import com.ibm.aiservices.mock.http.ErrorResponder;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.util.Json;

/**
 * Reproduces the request handling conventions shared by the Python FastAPI services: the {@code {"error": {...}}} envelope, FastAPI's default 422 validation body, and query parameter coercion.
 */
public final class FastApi {

    /**
     * Framework errors as produced by FastAPI with a handler registered only for {@code fastapi.HTTPException}: unknown routes fall through to Starlette's plain {@code {"detail": ...}} bodies.
     */
    public static final ErrorResponder STARLETTE_ERRORS = new ErrorResponder() {

        @Override
        public Response notFound(final Request _request) {
            return Response.json(404, Json.obj("detail", "Not Found"));
        }

        @Override
        public Response methodNotAllowed(final Request _request, final Set<String> _allowedMethods) {
            return Response.json(405, Json.obj("detail", "Method Not Allowed"));
        }

        @Override
        public Response invalidBody(final Request _request, final String _detail) {
            return validationResponse("json_invalid", Json.list("body", Integer.valueOf(0)), "JSON decode error", Json.obj(), Json.obj("error", _detail));
        }

        @Override
        public Response internalError(final Request _request) {
            return Response.text(500, "text/plain; charset=utf-8", "Internal Server Error");
        }
    };

    private FastApi() {
    }

    /**
     * Builds an error envelope response.
     *
     * @param _status the HTTP status
     * @param _code the error code string
     * @param _message the full message
     * @return the response
     */
    public static Response envelope(final int _status, final String _code, final String _message) {
        return Response.json(_status, Json.obj("error", Json.obj("code", _code, "message", _message, "status", Integer.valueOf(_status))));
    }

    /**
     * Builds an error envelope response with a {@code details} member.
     *
     * @param _status the HTTP status
     * @param _code the error code string
     * @param _message the full message
     * @param _details the details value
     * @return the response
     */
    public static Response envelope(final int _status, final String _code, final String _message, final Object _details) {
        return Response.json(_status, Json.obj("error", Json.obj("code", _code, "message", _message, "status", Integer.valueOf(_status), "details", _details)));
    }

    /**
     * Creates the exception raised by {@code APIError.raise_error}: the message is the code's default message followed by the detail.
     *
     * @param _code the error code
     * @param _detail the detail text; {@code null} uses the default message alone
     * @return the exception to throw
     */
    public static ApiException error(final ErrorCode _code, final String _detail) {
        return new ApiException(envelope(_code.getStatus(), _code.name(), message(_code, _detail)));
    }

    /**
     * Creates an envelope exception whose message is used verbatim, without the default-message prefix.
     *
     * @param _status the HTTP status
     * @param _code the error code string
     * @param _message the full message
     * @return the exception to throw
     */
    public static ApiException rawError(final int _status, final String _code, final String _message) {
        return new ApiException(envelope(_status, _code, _message));
    }

    /**
     * Formats a message the way {@code APIError.raise_error} does.
     *
     * @param _code the error code
     * @param _detail the detail text; may be {@code null}
     * @return the formatted message
     */
    public static String message(final ErrorCode _code, final String _detail) {
        return null == _detail ? _code.getDefaultMessage() : _code.getDefaultMessage() + ": " + _detail;
    }

    /**
     * Builds FastAPI's default request validation response.
     *
     * @param _type the pydantic error type, e.g. {@code missing}
     * @param _loc the error location, e.g. {@code ["query", "limit"]}
     * @param _msg the human readable message
     * @param _input the offending input value
     * @param _ctx optional pydantic context; {@code null} to omit
     * @return a 422 response
     */
    public static Response validationResponse(final String _type, final List<Object> _loc, final String _msg, final Object _input, final Map<String, Object> _ctx) {
        final Map<String, Object> item = Json.obj("type", _type, "loc", _loc, "msg", _msg, "input", _input);
        if (null != _ctx) {
            item.put("ctx", _ctx);
        }
        return Response.json(422, Json.obj("detail", Json.list(item)));
    }

    /**
     * Creates an exception carrying FastAPI's default validation response.
     *
     * @param _type the pydantic error type
     * @param _loc the error location
     * @param _msg the message
     * @param _input the offending input
     * @return the exception to throw
     */
    public static ApiException validation(final String _type, final List<Object> _loc, final String _msg, final Object _input) {
        return new ApiException(validationResponse(_type, _loc, _msg, _input, null));
    }

    /**
     * Creates the validation exception for a missing required field.
     *
     * @param _where the location kind: {@code body}, {@code query} or {@code form}
     * @param _field the field name
     * @return the exception to throw
     */
    public static ApiException missing(final String _where, final String _field) {
        return validation("missing", Json.list(_where, _field), "Field required", null);
    }

    /**
     * Reads an integer query parameter with FastAPI's coercion and bound checks.
     *
     * @param _request the request
     * @param _name the parameter name
     * @param _default the value when the parameter is absent
     * @param _min the inclusive minimum, or {@code null} for none
     * @param _max the inclusive maximum, or {@code null} for none
     * @return the parameter value
     * @throws ApiException with a 422 validation body if the value is not an integer or is out of range
     */
    public static int queryInt(final Request _request, final String _name, final int _default, final Integer _min, final Integer _max) {
        final String raw = _request.getQuery(_name);
        if (null == raw) {
            return _default;
        }
        final int value;
        try {
            value = Integer.parseInt(raw.trim());
        } catch (final NumberFormatException e) {
            throw validation("int_parsing", Json.list("query", _name), "Input should be a valid integer, unable to parse string as an integer", raw);
        }
        if (null != _min && value < _min.intValue()) {
            throw new ApiException(validationResponse("greater_than_equal", Json.list("query", _name), "Input should be greater than or equal to " + _min, raw, Json.obj("ge", _min)));
        }
        if (null != _max && value > _max.intValue()) {
            throw new ApiException(validationResponse("less_than_equal", Json.list("query", _name), "Input should be less than or equal to " + _max, raw, Json.obj("le", _max)));
        }
        return value;
    }

    /**
     * Reads a boolean query parameter with pydantic's lenient parsing.
     *
     * @param _request the request
     * @param _name the parameter name
     * @param _default the value when the parameter is absent
     * @return the parameter value
     * @throws ApiException with a 422 validation body if the value is not a recognised boolean
     */
    public static boolean queryBool(final Request _request, final String _name, final boolean _default) {
        final String raw = _request.getQuery(_name);
        if (null == raw) {
            return _default;
        }
        final Boolean parsed = parseBool(raw);
        if (null == parsed) {
            throw validation("bool_parsing", Json.list("query", _name), "Input should be a valid boolean, unable to interpret input", raw);
        }
        return parsed.booleanValue();
    }

    /**
     * Parses a boolean using the spellings pydantic accepts.
     *
     * @param _raw the text to parse
     * @return the parsed value, or {@code null} if the text is not a boolean
     */
    public static Boolean parseBool(final String _raw) {
        final String normalized = _raw.trim().toLowerCase(Locale.ROOT);
        if ("true".equals(normalized) || "1".equals(normalized) || "yes".equals(normalized) || "on".equals(normalized) || "y".equals(normalized) || "t".equals(normalized)) {
            return Boolean.TRUE;
        }
        if ("false".equals(normalized) || "0".equals(normalized) || "no".equals(normalized) || "off".equals(normalized) || "n".equals(normalized) || "f".equals(normalized)) {
            return Boolean.FALSE;
        }
        return null;
    }

    /**
     * Reads an enumerated query parameter, rejecting unknown values with a 422 validation body.
     *
     * @param _request the request
     * @param _name the parameter name
     * @param _allowed the allowed values
     * @return the value, or {@code null} if absent
     */
    public static String queryEnum(final Request _request, final String _name, final List<String> _allowed) {
        final String raw = _request.getQuery(_name);
        if (null == raw || _allowed.contains(raw)) {
            return raw;
        }
        throw new ApiException(validationResponse("enum", Json.list("query", _name), "Input should be " + describeChoices(_allowed), raw, Json.obj("expected", describeChoices(_allowed))));
    }

    /**
     * Formats enum choices the way pydantic does, e.g. {@code 'a', 'b' or 'c'}.
     *
     * @param _allowed the allowed values
     * @return the formatted list
     */
    private static String describeChoices(final List<String> _allowed) {
        final StringBuilder text = new StringBuilder();
        for (int i = 0; i < _allowed.size(); i++) {
            if (0 < i) {
                text.append(i == _allowed.size() - 1 ? " or " : ", ");
            }
            text.append('\'').append(_allowed.get(i)).append('\'');
        }
        return text.toString();
    }

    /**
     * Builds the Swagger UI page FastAPI serves at {@code /} and {@code /docs}.
     *
     * @param _title the page title
     * @param _specUrl the URL of the OpenAPI document to render
     * @return the HTML response
     */
    public static Response swaggerUi(final String _title, final String _specUrl) {
        final String html = "<!DOCTYPE html>\n<html>\n<head>\n"
                + "<link type=\"text/css\" rel=\"stylesheet\" href=\"https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css\">\n"
                + "<title>" + _title + " - Swagger UI</title>\n</head>\n<body>\n<div id=\"swagger-ui\"></div>\n"
                + "<script src=\"https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js\"></script>\n<script>\n"
                + "const ui = SwaggerUIBundle({url: '" + _specUrl + "', dom_id: '#swagger-ui', layout: 'BaseLayout', deepLinking: true, "
                + "presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset]})\n</script>\n</body>\n</html>\n";
        return Response.text(200, "text/html; charset=utf-8", html);
    }
}
