// Package httpclient provides a generic, reusable HTTP client for making
// JSON-based API requests. It abstracts the low-level details of building
// requests, setting headers, and decoding responses.
package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// checkResponseError validates the HTTP response status code and returns an
// error if it's outside the 2xx range. For error responses, it attempts to
// extract a descriptive message from the JSON body before falling back to
// the raw status code. For successful responses, the body is left untouched
// for the caller to process.
func checkResponseError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Only read body for error responses (typically small)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read error response body: %w", err)
	}

	var errBody map[string]any
	if jsonErr := json.Unmarshal(body, &errBody); jsonErr == nil {
		if msg, ok := errBody["error"].(string); ok {
			return fmt.Errorf("server error (%d): %s", resp.StatusCode, msg)
		}
	}

	return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
}

const defaultTimeout = 30 * time.Second

// Request holds all the parameters needed to make an HTTP request.
type Request struct {
	// Method is the HTTP method (e.g. http.MethodGet, http.MethodPost).
	Method string
	// Endpoint is the API path (e.g. "/api/v1/auth/login").
	Endpoint string
	// Headers is an optional map of additional HTTP headers to include.
	Headers map[string]string
	// Query is an optional map of URL query parameters.
	Query map[string]string
	// Payload is the request body, which will be JSON-encoded. May be nil.
	Payload any
	// Out is the target to JSON-decode the response body into. May be nil to discard.
	Out any
}

// HTTPClient is a lightweight HTTP client that sends JSON requests to a base server URL.
type HTTPClient struct {
	serverURL  string
	httpClient *http.Client
}

// New creates a new HTTPClient targeting the given server URL.
func New(serverURL string) *HTTPClient {
	return &HTTPClient{
		serverURL:  serverURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// buildRequestURL constructs the full request URL by combining the base server URL,
// endpoint path, and query parameters. It handles URL parsing, path resolution, and
// query string encoding in a single operation.
func (c *HTTPClient) buildRequestURL(endpoint string, query map[string]string) (*url.URL, error) {
	base, err := url.Parse(c.serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}

	ref, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}

	u := base.ResolveReference(ref)

	if len(query) > 0 {
		q := u.Query()
		for k, v := range query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	return u, nil
}

// createHTTPRequest builds an *http.Request with the given parameters, marshaling
// the payload as JSON if provided, and applying default and custom headers.
func (c *HTTPClient) createHTTPRequest(method, urlStr string, payload any, headers map[string]string) (*http.Request, error) {
	var reqBody io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, urlStr, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set default headers for JSON APIs.
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	// Apply caller-supplied headers (may override defaults).
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return req, nil
}

// Do executes the given Request against the configured server URL.
// It marshals the Payload (if any) as JSON, applies all Headers and Query
// parameters, executes the request, and unmarshals the response into Out (if set).
func (c *HTTPClient) Do(r Request) error {
	u, err := c.buildRequestURL(r.Endpoint, r.Query)
	if err != nil {
		return err
	}

	req, err := c.createHTTPRequest(r.Method, u.String(), r.Payload, r.Headers)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute %s %s: %w", r.Method, r.Endpoint, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if err := checkResponseError(resp); err != nil {
		return err
	}

	// Stream decode the response body directly if an output target is provided.
	// This avoids buffering the entire response in memory.
	if r.Out != nil {
		if err := json.NewDecoder(resp.Body).Decode(r.Out); err != nil {
			return fmt.Errorf("decode response from %s %s: %w", r.Method, r.Endpoint, err)
		}
	}

	return nil
}

// Made with Bob
