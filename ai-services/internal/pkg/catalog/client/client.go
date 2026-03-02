//go:build catalog_api
// +build catalog_api

// Package client provides an HTTP client for the AI Services catalog API server.
// It handles authentication, automatic token refresh, and all API calls.
package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
)

const (
	defaultTimeout = 30 * time.Second

	// tokenRefreshSkew is the window before the access token's expiry within
	// which a proactive refresh is triggered. If the token expires in less than
	// this duration it is considered "about to expire".
	tokenRefreshSkew = 30 * time.Second
)

// Client is an authenticated HTTP client for the catalog API server.
type Client struct {
	serverURL  string
	httpClient *http.Client
	creds      config.Credentials
}

// LoginResponse is the JSON body returned by POST /api/v1/auth/login and POST /api/v1/auth/refresh.
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

// UserInfo is the JSON body returned by GET /api/v1/auth/me.
type UserInfo struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// New creates a Client using credentials loaded from the local config file.
// It refreshes the access token only when it is about to expire (within
// tokenRefreshSkew of its expiry time); otherwise the stored token is reused.
func New() (*Client, error) {
	creds, err := config.Load()
	if err != nil {
		return nil, err
	}

	c := &Client{
		serverURL:  creds.ServerURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
		creds:      creds,
	}

	if c.accessTokenNeedsRefresh() {
		if err := c.RefreshToken(); err != nil {
			return nil, fmt.Errorf("refresh token: %w", err)
		}
	}

	return c, nil
}

// accessTokenNeedsRefresh returns true when the stored access token is missing,
// has an unknown expiry, or will expire within tokenRefreshSkew.
func (c *Client) accessTokenNeedsRefresh() bool {
	if c.creds.AccessToken == "" {
		return true
	}

	// Use the persisted expiry when available.
	if !c.creds.AccessTokenExpiry.IsZero() {
		return time.Until(c.creds.AccessTokenExpiry) < tokenRefreshSkew
	}

	// Fall back to parsing the JWT payload directly (no signature verification
	// needed – we only want the exp claim to decide whether to refresh).
	exp, err := jwtExpiry(c.creds.AccessToken)
	if err != nil {
		// Cannot determine expiry; refresh to be safe.
		return true
	}

	return time.Until(exp) < tokenRefreshSkew
}

// NewWithLogin creates a Client by performing a fresh login with username/password.
// The resulting tokens are saved to the local config file.
func NewWithLogin(serverURL, username, password string) (*Client, error) {
	c := &Client{
		serverURL:  serverURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}

	resp, err := c.Login(username, password)
	if err != nil {
		return nil, err
	}

	c.creds = config.Credentials{
		ServerURL:    serverURL,
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
	}

	// Best-effort: record the expiry so future calls can skip unnecessary refreshes.
	if exp, err := jwtExpiry(resp.AccessToken); err == nil {
		c.creds.AccessTokenExpiry = exp
	}

	if err := config.Save(c.creds); err != nil {
		return nil, fmt.Errorf("save credentials: %w", err)
	}

	return c, nil
}

// Login calls POST /api/v1/auth/login and returns the token pair.
func (c *Client) Login(username, password string) (LoginResponse, error) {
	body := map[string]string{
		"username": username,
		"password": password,
	}

	var resp LoginResponse
	if err := c.postJSON("/api/v1/auth/login", body, &resp); err != nil {
		return LoginResponse{}, err
	}

	return resp, nil
}

// RefreshToken calls POST /api/v1/auth/refresh using the stored refresh token
// and updates the in-memory credentials (and persists them to disk).
func (c *Client) RefreshToken() error {
	body := map[string]string{
		"refresh_token": c.creds.RefreshToken,
	}

	var resp LoginResponse
	if err := c.postJSON("/api/v1/auth/refresh", body, &resp); err != nil {
		return err
	}

	c.creds.AccessToken = resp.AccessToken
	c.creds.RefreshToken = resp.RefreshToken

	// Record the new expiry so subsequent calls can avoid unnecessary refreshes.
	if exp, err := jwtExpiry(resp.AccessToken); err == nil {
		c.creds.AccessTokenExpiry = exp
	} else {
		c.creds.AccessTokenExpiry = time.Time{} // zero = unknown
	}

	return config.Save(c.creds)
}

// ---------------------------------------------------------------------------
// JWT helpers
// ---------------------------------------------------------------------------

// jwtExpiry decodes the payload of a JWT (without verifying the signature) and
// returns the value of the "exp" claim as a time.Time.
// It is used purely to decide whether a proactive token refresh is needed.
func jwtExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("malformed JWT: expected 3 parts, got %d", len(parts))
	}

	// JWT uses raw (unpadded) base64url encoding for the payload.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("parse JWT claims: %w", err)
	}

	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("JWT has no exp claim")
	}

	return time.Unix(claims.Exp, 0), nil
}

// Me calls GET /api/v1/auth/me and returns the current user info.
func (c *Client) Me() (UserInfo, error) {
	var info UserInfo
	if err := c.getJSON("/api/v1/auth/me", &info); err != nil {
		return UserInfo{}, err
	}

	return info, nil
}

// Logout calls POST /api/v1/auth/logout to invalidate the access token on the server,
// then removes the local credentials file.
func (c *Client) Logout() error {
	// Best-effort server-side logout; ignore errors (token may already be expired).
	_ = c.postJSONAuth("/api/v1/auth/logout", nil, nil)

	return config.Delete()
}

// AccessToken returns the current access token held by the client.
func (c *Client) AccessToken() string {
	return c.creds.AccessToken
}

// ServerURL returns the server URL the client is connected to.
func (c *Client) ServerURL() string {
	return c.serverURL
}

// ---------------------------------------------------------------------------
// low-level helpers
// ---------------------------------------------------------------------------

// postJSON sends an unauthenticated POST request with a JSON body and decodes
// the response into out (may be nil to discard the body).
func (c *Client) postJSON(path string, body interface{}, out interface{}) error {
	return c.doJSON(http.MethodPost, path, body, out, false)
}

// postJSONAuth sends an authenticated POST request.
func (c *Client) postJSONAuth(path string, body interface{}, out interface{}) error {
	return c.doJSON(http.MethodPost, path, body, out, true)
}

// getJSON sends an authenticated GET request and decodes the response into out.
func (c *Client) getJSON(path string, out interface{}) error {
	return c.doJSON(http.MethodGet, path, nil, out, true)
}

func (c *Client) doJSON(method, path string, body interface{}, out interface{}, auth bool) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}

		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.serverURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	req.Header.Set("Accept", "application/json")

	if auth {
		req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to extract an error message from the JSON body.
		var errBody map[string]interface{}
		if jsonErr := json.Unmarshal(respBytes, &errBody); jsonErr == nil {
			if msg, ok := errBody["error"].(string); ok {
				return fmt.Errorf("server error (%d): %s", resp.StatusCode, msg)
			}
		}

		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	if out != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// Made with Bob
