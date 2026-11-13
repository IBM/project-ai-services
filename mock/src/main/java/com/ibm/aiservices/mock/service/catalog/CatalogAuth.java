package com.ibm.aiservices.mock.service.catalog;

import java.io.UnsupportedEncodingException;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.security.InvalidKeyException;
import java.security.NoSuchAlgorithmException;
import java.security.SecureRandom;
import java.util.Base64;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

import com.ibm.aiservices.mock.MockConfig;
import com.ibm.aiservices.mock.http.ApiException;
import com.ibm.aiservices.mock.http.Request;
import com.ibm.aiservices.mock.http.Response;
import com.ibm.aiservices.mock.http.RouteHandler;
import com.ibm.aiservices.mock.http.Router;
import com.ibm.aiservices.mock.util.Ids;
import com.ibm.aiservices.mock.util.Json;
import com.ibm.aiservices.mock.util.JsonBody;
import com.ibm.aiservices.mock.util.Times;

/**
 * Mock of the catalog server's JWT authentication: login, ManageIQ token exchange, refresh rotation, logout blacklisting and the bearer middleware.
 * <p>
 * Tokens are real HS256 JWTs signed with a per-process random key, so clients that decode the {@code exp} claim behave as they do against the real server.
 */
final class CatalogAuth {

    private static final long ACCESS_TTL_MILLIS = 15L * 60L * 1000L;
    private static final long REFRESH_TTL_MILLIS = 24L * 60L * 60L * 1000L;
    private static final String ISSUER = "ai-services-catalog-server";

    private final MockConfig m_config;
    private final byte[] m_signingKey = new byte[32];
    private final Map<String, TokenInfo> m_tokens = new HashMap<String, TokenInfo>();
    private final Set<String> m_revoked = new HashSet<String>();

    /**
     * Claims of an issued token.
     */
    private static final class TokenInfo {

        private final String m_userId;
        private final String m_audience;
        private final long m_expiresAt;

        TokenInfo(final String _userId, final String _audience, final long _expiresAt) {
            m_userId = _userId;
            m_audience = _audience;
            m_expiresAt = _expiresAt;
        }
    }

    CatalogAuth(final MockConfig _config) {
        m_config = _config;
        new SecureRandom().nextBytes(m_signingKey);
    }

    /**
     * Registers the {@code /api/v1/auth} routes.
     *
     * @param _router the catalog router
     */
    void register(final Router _router) {
        _router.post("/api/v1/auth/login", this::login);
        _router.post("/api/v1/auth/token", this::tokenLogin);
        _router.post("/api/v1/auth/logout", authed(this::logout));
        _router.post("/api/v1/auth/refresh", this::refresh);
        _router.get("/api/v1/auth/me", authed(this::me));
    }

    /**
     * Wraps a handler with the bearer token middleware, which also sets {@code X-Token-Exp} on every authenticated response.
     *
     * @param _handler the protected handler
     * @return the wrapped handler
     */
    RouteHandler authed(final RouteHandler _handler) {
        return _request -> {
            final TokenInfo token = authenticate(_request);
            final String expiry = Times.rfc3339(token.m_expiresAt);
            try {
                return _handler.handle(_request).withHeader("X-Token-Exp", expiry);
            } catch (final ApiException e) {
                throw new ApiException(e.getResponse().withHeader("X-Token-Exp", expiry));
            }
        };
    }

    /**
     * Returns the user ID of an authenticated request.
     *
     * @param _request a request that passed {@link #authed(RouteHandler)}
     * @return the user ID
     */
    String userOf(final Request _request) {
        return authenticate(_request).m_userId;
    }

    /**
     * Validates the access token of a request.
     *
     * @param _request the request
     * @return the token claims
     */
    private TokenInfo authenticate(final Request _request) {
        final String token = _request.getBearerToken();
        if (null == token) {
            throw Gin.error(401, "missing bearer token");
        }
        if (token.isEmpty()) {
            throw Gin.error(401, "invalid bearer token");
        }
        synchronized (m_tokens) {
            if (m_revoked.contains(token)) {
                throw Gin.error(401, "token revoked");
            }
            final TokenInfo info = m_tokens.get(token);
            if (null == info || !"access".equals(info.m_audience) || info.m_expiresAt <= System.currentTimeMillis()) {
                throw Gin.error(401, "invalid token");
            }
            return info;
        }
    }

    /**
     * Issues an access and refresh token pair.
     *
     * @param _userId the user ID
     * @return the token response body
     */
    private Map<String, Object> issuePair(final String _userId) {
        final long now = System.currentTimeMillis();
        final String access = sign(_userId, "access", now, now + ACCESS_TTL_MILLIS);
        final String refresh = sign(_userId, "refresh", now, now + REFRESH_TTL_MILLIS);
        synchronized (m_tokens) {
            m_tokens.put(access, new TokenInfo(_userId, "access", now + ACCESS_TTL_MILLIS));
            m_tokens.put(refresh, new TokenInfo(_userId, "refresh", now + REFRESH_TTL_MILLIS));
        }
        return Json.obj("access_token", access, "refresh_token", refresh, "token_type", "Bearer");
    }

    /**
     * Builds and signs a JWT.
     *
     * @param _userId the subject
     * @param _audience {@code access} or {@code refresh}
     * @param _issuedAt the issue time in epoch milliseconds
     * @param _expiresAt the expiry time in epoch milliseconds
     * @return the compact JWT
     */
    private String sign(final String _userId, final String _audience, final long _issuedAt, final long _expiresAt) {
        final Base64.Encoder encoder = Base64.getUrlEncoder().withoutPadding();
        final String header = encoder.encodeToString("{\"alg\":\"HS256\",\"typ\":\"JWT\"}".getBytes(StandardCharsets.UTF_8));
        final Map<String, Object> claims = Json.obj("uid", _userId, "iss", ISSUER, "sub", _userId, "aud", Json.list(_audience), "exp", Long.valueOf(_expiresAt / 1000L), "nbf", Long.valueOf(_issuedAt / 1000L), "iat", Long.valueOf(_issuedAt / 1000L), "jti", Ids.uuid());
        final String payload = encoder.encodeToString(Json.toJson(claims).getBytes(StandardCharsets.UTF_8));
        try {
            final Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(m_signingKey, "HmacSHA256"));
            final byte[] signature = mac.doFinal((header + "." + payload).getBytes(StandardCharsets.US_ASCII));
            return header + "." + payload + "." + encoder.encodeToString(signature);
        } catch (final NoSuchAlgorithmException | InvalidKeyException e) {
            // HmacSHA256 is mandatory on every Java platform and the key is always 32 bytes.
            throw new IllegalStateException(e);
        }
    }

    /**
     * Handles {@code POST /api/v1/auth/login}.
     *
     * @param _request the request
     * @return the token pair
     */
    private Response login(final Request _request) {
        final JsonBody body = _request.getJson();
        final String username = body.getString("username", "");
        final String password = body.getString("password", "");
        if (username.isEmpty() || password.isEmpty()) {
            throw Gin.error(400, "invalid payload");
        }
        if (!m_config.getCatalogUsername().equals(username) || !m_config.getCatalogPassword().equals(password)) {
            throw Gin.error(401, "invalid credentials");
        }
        return Response.json(200, issuePair(username));
    }

    /**
     * Handles {@code POST /api/v1/auth/token}: exchanges any non-empty ManageIQ bearer token for a token pair.
     *
     * @param _request the request
     * @return the token pair
     */
    private Response tokenLogin(final Request _request) {
        final String token = _request.getBearerToken();
        if (null == token || token.isEmpty()) {
            throw Gin.error(401, "missing ManageIQ token in Authorization header");
        }
        return Response.json(200, issuePair(m_config.getCatalogUsername()));
    }

    /**
     * Handles {@code POST /api/v1/auth/refresh}, rotating the refresh token.
     *
     * @param _request the request
     * @return a new token pair
     */
    private Response refresh(final Request _request) {
        final String refreshToken;
        // Like gin's ShouldBind, form bodies are accepted; JSON is accepted regardless of the declared content type.
        if (_request.hasContentType("application/x-www-form-urlencoded") && !_request.getBodyText().trim().startsWith("{")) {
            refreshToken = formValue(_request.getBodyText(), "refresh_token");
        } else {
            refreshToken = _request.getJson().getString("refresh_token", "");
        }
        if (null == refreshToken || refreshToken.isEmpty()) {
            throw Gin.error(400, "invalid payload");
        }
        final TokenInfo info;
        synchronized (m_tokens) {
            info = m_tokens.get(refreshToken);
            if (null == info || !"refresh".equals(info.m_audience) || info.m_expiresAt <= System.currentTimeMillis()) {
                throw Gin.error(401, "invalid refresh token");
            }
            m_revoked.add(refreshToken);
        }
        return Response.json(200, issuePair(info.m_userId));
    }

    /**
     * Extracts and decodes one field of a URL-encoded form body.
     *
     * @param _body the form body
     * @param _name the field name
     * @return the decoded value, or {@code null} if absent
     */
    private static String formValue(final String _body, final String _name) {
        for (final String pair : _body.split("&")) {
            if (pair.startsWith(_name + "=")) {
                try {
                    return URLDecoder.decode(pair.substring(_name.length() + 1), "UTF-8");
                } catch (final UnsupportedEncodingException e) {
                    // UTF-8 support is mandatory on every Java platform.
                    throw new IllegalStateException(e);
                }
            }
        }
        return null;
    }

    /**
     * Handles {@code POST /api/v1/auth/logout}, revoking the access token and an optional refresh token.
     *
     * @param _request the request
     * @return a confirmation message
     */
    private Response logout(final Request _request) {
        final String refreshToken = _request.getHeader("X-Refresh-Token");
        synchronized (m_tokens) {
            m_revoked.add(_request.getBearerToken());
            if (null != refreshToken && !refreshToken.isEmpty()) {
                m_revoked.add(refreshToken);
            }
        }
        return Response.json(200, Json.obj("message", "logged out"));
    }

    private Response me(final Request _request) {
        final String userId = userOf(_request);
        return Response.json(200, Json.obj("id", userId, "username", userId, "name", "Administrator"));
    }
}
