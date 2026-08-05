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
	"path/filepath"
	"runtime"
	"strings"
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

// ErrorResponse represents a generic API error response body.
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

// GetTestPDFPath returns the absolute path to the shared test PDF fixture (test_doc.pdf)
// located at ingestion/docs/test_doc.pdf relative to the e2e tests root.
// Returns an empty string if the caller location cannot be resolved.
func GetTestPDFPath() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	// common/ is one level below the e2e root; ingestion/docs is a sibling of common/.
	return filepath.Join(filepath.Dir(filepath.Dir(filename)), "ingestion", "docs", "test_doc.pdf")
}

// IsResourceLockedError reports whether err is an HTTP 409 resource-lock error.
// Any 409 is treated as a lock because both the digitize and summarize APIs only return 409 for that reason.
func IsResourceLockedError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	if !strings.Contains(msg, "409") {
		return false
	}

	// A plain 409 with no body detail is still a resource-locked response.
	return true
}

// IsRateLimitError reports whether err is an HTTP 429 rate-limit error.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "429") &&
		(strings.Contains(err.Error(), "RATE_LIMIT_EXCEEDED") ||
			strings.Contains(err.Error(), "Too many"))
}
