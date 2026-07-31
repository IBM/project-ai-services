package common

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// HTTPClientConfig contains configuration for HTTP clients.
type HTTPClientConfig struct {
	Timeout time.Duration
	// InsecureSkipVerify skips TLS certificate verification (useful for self-signed certs).
	InsecureSkipVerify bool
	// PoolConnections enables connection pooling for better performance.
	PoolConnections bool
}

// DefaultHTTPConfig returns sensible defaults for HTTP communication.
func DefaultHTTPConfig() HTTPClientConfig {
	return HTTPClientConfig{
		Timeout:            10 * time.Second,
		InsecureSkipVerify: true, // Self-signed certs common in test environments
		PoolConnections:    true,
	}
}

// GetHTTPClient returns an HTTP client configured with the given config.
// If poolConnections is true, uses a shared transport with connection pooling.
// Otherwise, creates a minimal client suitable for single requests.
func GetHTTPClient(config HTTPClientConfig) *http.Client {
	tlsConfig := &tls.Config{InsecureSkipVerify: config.InsecureSkipVerify} //nolint:gosec

	if !config.PoolConnections {
		// Minimal client for simple one-off requests
		return &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}
	}

	// Pooled transport for better performance across multiple requests
	transport := &http.Transport{
		TLSClientConfig:      tlsConfig,
		MaxIdleConnsPerHost:  4,                //nolint:mnd
		IdleConnTimeout:      90 * time.Second, //nolint:mnd
		ResponseHeaderTimeout: 35 * time.Second, //nolint:mnd
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second, //nolint:mnd
			KeepAlive: 30 * time.Second, //nolint:mnd
		}).DialContext,
	}

	return &http.Client{
		Timeout:   config.Timeout,
		Transport: transport,
	}
}

// DrainAndClose drains and closes an HTTP response body,
// allowing the underlying TCP connection to be returned to the pool.
func DrainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

// DoRequest sends an HTTP request with the given method, URL, body, and content type.
// It handles request creation, client setup, body reading, and cleanup.
// Returns (responseBody, statusCode, error).
func DoRequest(ctx context.Context, method, url string, body *bytes.Buffer, contentType string, config HTTPClientConfig) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = body
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	client := GetHTTPClient(config)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer DrainAndClose(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// DoGET sends a GET request and returns (body, statusCode, error).
func DoGET(ctx context.Context, url string, config HTTPClientConfig) ([]byte, int, error) {
	return DoRequest(ctx, http.MethodGet, url, nil, "", config)
}

// DoPOST sends a POST request with the given body and content type.
func DoPOST(ctx context.Context, url string, body *bytes.Buffer, contentType string, config HTTPClientConfig) ([]byte, int, error) {
	return DoRequest(ctx, http.MethodPost, url, body, contentType, config)
}

// DoDELETE sends a DELETE request.
func DoDELETE(ctx context.Context, url string, config HTTPClientConfig) ([]byte, int, error) {
	return DoRequest(ctx, http.MethodDelete, url, nil, "", config)
}

// ValidateStatusAndUnmarshal checks that the HTTP status code matches the expected status,
// then unmarshals the response body into the provided value.
// If v is nil, skips unmarshaling.
func ValidateStatusAndUnmarshal(body []byte, statusCode, expectedStatus int, v any) error {
	if statusCode != expectedStatus {
		return fmt.Errorf("unexpected status code %d: %s", statusCode, string(body))
	}

	if v == nil {
		return nil
	}

	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	return nil
}

// ParseErrorResponse parses a response body as a generic error response.
// Expects JSON with an "error" field containing "code", "message", and "status" fields.
type ErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`    // HTTP status code as number
		Message string `json:"message"` // Error message
		Status  string `json:"status"`  // Error status string (e.g., "UNSUPPORTED_FILE_TYPE")
	} `json:"error,omitempty"`
}

// ParseErrorResponse parses the response body as an error response.
func ParseErrorResponse(respBody []byte, statusCode int) (*ErrorResponse, error) {
	var errorResp ErrorResponse
	if err := json.Unmarshal(respBody, &errorResp); err != nil {
		return nil, fmt.Errorf("failed to parse error response (status %d): %w, body: %s", statusCode, err, string(respBody))
	}

	return &errorResp, nil
}

// ExpectErrorResponse checks if the response status differs from the expected success status,
// and returns a parsed error response. If the status was successful, returns an error indicating unexpected success.
func ExpectErrorResponse(body []byte, statusCode, successStatus int) (*ErrorResponse, error) {
	if statusCode != successStatus {
		return ParseErrorResponse(body, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(body))
}
