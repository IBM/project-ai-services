package common

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// HTTPClientConfig holds configuration for HTTP clients.
type HTTPClientConfig struct {
	Timeout            time.Duration
	InsecureSkipVerify bool // skip TLS verification (self-signed certs in test envs)
	PoolConnections    bool // enable connection pooling for repeated requests
}

// DefaultHTTPConfig returns sensible defaults for HTTP communication.
func DefaultHTTPConfig() HTTPClientConfig {
	return HTTPClientConfig{
		Timeout:            10 * time.Second, //nolint:mnd
		InsecureSkipVerify: true,
		PoolConnections:    true,
	}
}

// GetHTTPClient builds an HTTP client from config.
// When PoolConnections is false a minimal single-use client is returned.
func GetHTTPClient(config HTTPClientConfig) *http.Client {
	tlsConfig := &tls.Config{InsecureSkipVerify: config.InsecureSkipVerify} //nolint:gosec

	if !config.PoolConnections {
		return &http.Client{
			Timeout: config.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}
	}

	transport := &http.Transport{
		TLSClientConfig:       tlsConfig,
		MaxIdleConnsPerHost:   4,                //nolint:mnd
		IdleConnTimeout:       90 * time.Second, //nolint:mnd
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

// DrainAndClose drains and closes an HTTP response body to allow connection reuse.
func DrainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

// DoRequest sends an HTTP request and returns (responseBody, statusCode, error).
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

// DoGET sends a GET request.
func DoGET(ctx context.Context, url string, config HTTPClientConfig) ([]byte, int, error) {
	return DoRequest(ctx, http.MethodGet, url, nil, "", config)
}

// DoPOST sends a POST request with a body and content type.
func DoPOST(ctx context.Context, url string, body *bytes.Buffer, contentType string, config HTTPClientConfig) ([]byte, int, error) {
	return DoRequest(ctx, http.MethodPost, url, body, contentType, config)
}

// DoDELETE sends a DELETE request.
func DoDELETE(ctx context.Context, url string, config HTTPClientConfig) ([]byte, int, error) {
	return DoRequest(ctx, http.MethodDelete, url, nil, "", config)
}

// ValidateStatusAndUnmarshal verifies the status code then unmarshals body into v.
// If v is nil, unmarshaling is skipped.
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

// ErrorResponse is the standard API error envelope.
// Shape: {"error":{"code":N,"message":"...","status":"..."}}.
type ErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"` // e.g. "UNSUPPORTED_FILE_TYPE"
	} `json:"error,omitempty"`
}

// BuildMultipartBody builds a multipart/form-data body with a single file field.
// Shared by packages that need to POST files.
func BuildMultipartBody(fieldName, filePath string) (*bytes.Buffer, *multipart.Writer, error) {
	return BuildMultipartBodyWithFields(fieldName, filePath, nil)
}

// BuildMultipartBodyWithFields builds a multipart/form-data body with a file field
// and optional extra string fields (e.g. "level", "length").
// Pass nil or an empty map when no extra fields are needed.
func BuildMultipartBodyWithFields(fieldName, filePath string, fields map[string]string) (*bytes.Buffer, *multipart.Writer, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile(fieldName, filepath.Base(filePath))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, nil, fmt.Errorf("failed to copy file content: %w", err)
	}

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, nil, fmt.Errorf("failed to write field %s: %w", k, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return body, writer, nil
}

// ParseErrorResponse unmarshals a non-success response body into an ErrorResponse.
func ParseErrorResponse(respBody []byte, statusCode int) (*ErrorResponse, error) {
	var errorResp ErrorResponse
	if err := json.Unmarshal(respBody, &errorResp); err != nil {
		return nil, fmt.Errorf("failed to parse error response (status %d): %w, body: %s", statusCode, err, string(respBody))
	}

	return &errorResp, nil
}

// ExpectErrorResponse parses an error response when status != successStatus.
// Returns an error if the server unexpectedly returned a success status.
func ExpectErrorResponse(body []byte, statusCode, successStatus int) (*ErrorResponse, error) {
	if statusCode != successStatus {
		return ParseErrorResponse(body, statusCode)
	}

	return nil, fmt.Errorf("unexpected success with status code %d: %s", statusCode, string(body))
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

	if !strings.Contains(err.Error(), "409") {
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

// SummaryContainsAnyKeyword reports whether summary contains at least one of
// the given keywords (case-insensitive). The first matched keyword is returned
// together with true so callers can include it in assertion failure messages.
func SummaryContainsAnyKeyword(summary string, keywords []string) (string, bool) {
	lower := strings.ToLower(summary)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return kw, true
		}
	}

	return "", false
}
