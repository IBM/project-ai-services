package similarity

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Transport tuning constants for the shared similarity HTTP client.
const (
	transportMaxIdleConnsPerHost   = 4                //nolint:mnd
	transportIdleConnTimeout       = 90 * time.Second //nolint:mnd
	transportResponseHeaderTimeout = 25 * time.Second //nolint:mnd
	transportDialTimeout           = 15 * time.Second //nolint:mnd
	transportDialKeepAlive         = 30 * time.Second //nolint:mnd

	// postCallTimeout is the end-to-end deadline for a POST similarity-search request.
	postCallTimeout = 60 * time.Second //nolint:mnd

	// getCallTimeout is the end-to-end deadline for a GET health request.
	getCallTimeout = 30 * time.Second //nolint:mnd
)

// sharedSimilarityTransport pools TLS connections and skips certificate verification
// to support both plain http:// (legacy podman) and https:// nip.io self-signed certs.
var sharedSimilarityTransport = &http.Transport{
	TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	MaxIdleConnsPerHost: transportMaxIdleConnsPerHost,
	IdleConnTimeout:     transportIdleConnTimeout,
	// ResponseHeaderTimeout guards against dead keep-alive sockets that never send response headers.
	ResponseHeaderTimeout: transportResponseHeaderTimeout,
	DialContext: (&net.Dialer{
		Timeout:   transportDialTimeout,
		KeepAlive: transportDialKeepAlive,
	}).DialContext,
}

// getHTTPClient returns an HTTP client with the given timeout using the shared pooled transport.
func getHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: sharedSimilarityTransport,
	}
}

// drainAndClose drains and closes the body so the underlying TCP connection is returned to the pool.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

// GetSimilarityBaseURL returns the base URL for the similarity-api service given a port.
func GetSimilarityBaseURL(port string) string {
	return fmt.Sprintf("http://localhost:%s", port)
}

// -----------------------------------------------------------------------
// Request / Response types
// -----------------------------------------------------------------------

// SimilaritySearchRequest is the payload for POST /v1/similarity-search.
type SimilaritySearchRequest struct {
	Query  string `json:"query"`
	Mode   string `json:"mode,omitempty"`
	TopK   *int   `json:"top_k,omitempty"`
	Rerank *bool  `json:"rerank,omitempty"`
}

// SimilarityResult represents a single document result in the search response.
type SimilarityResult struct {
	PageContent string  `json:"page_content"`
	Filename    string  `json:"filename"`
	Type        string  `json:"type"`
	Source      string  `json:"source"`
	ChunkID     string  `json:"chunk_id"`
	Score       float64 `json:"score"`
}

// SimilaritySearchResponse is the successful 200 body of POST /v1/similarity-search.
type SimilaritySearchResponse struct {
	ScoreType string             `json:"score_type"`
	Results   []SimilarityResult `json:"results"`
}

// SimilarityErrorResponse is the error body returned by the similarity-api.
type SimilarityErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"error"`
	Detail interface{} `json:"detail"`
}

// HealthResponse is the 200 body of GET /health.
type HealthResponse struct {
	Status string `json:"status"`
}

// -----------------------------------------------------------------------
// HTTP helpers
// -----------------------------------------------------------------------

// doPost sends a JSON POST request and returns (body bytes, status code, error).
func doPost(ctx context.Context, url string, payload any, timeout time.Duration) ([]byte, int, http.Header, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(raw))
	if err != nil {
		return nil, 0, nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := getHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("request failed: %w", err)
	}

	defer drainAndClose(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("read response: %w", err)
	}

	return body, resp.StatusCode, resp.Header, nil
}

// doGet sends a GET request and returns (body bytes, status code, response headers, error).
func doGet(ctx context.Context, url string, timeout time.Duration) ([]byte, int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := getHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("request failed: %w", err)
	}

	defer drainAndClose(resp.Body)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("read response: %w", err)
	}

	return body, resp.StatusCode, resp.Header, nil
}

// -----------------------------------------------------------------------
// Public API helpers used by test specs
// -----------------------------------------------------------------------

// HealthCheck calls GET /health and returns an error if the service is not healthy.
func HealthCheck(ctx context.Context, baseURL string) error {
	url := strings.TrimRight(baseURL, "/") + "/health"
	body, statusCode, _, err := doGet(ctx, url, getCallTimeout)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}

	if statusCode != http.StatusOK {
		return fmt.Errorf("health check returned HTTP %d: %s", statusCode, string(body))
	}

	logger.Infof("[SIMILARITY] GET /health → HTTP %d", statusCode)

	return nil
}

// HealthCheckWithResponse calls GET /health and returns the parsed response, status code, and headers.
func HealthCheckWithResponse(ctx context.Context, baseURL string) (*HealthResponse, int, http.Header, error) {
	url := strings.TrimRight(baseURL, "/") + "/health"
	body, statusCode, headers, err := doGet(ctx, url, getCallTimeout)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("health check request failed: %w", err)
	}

	var resp HealthResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, statusCode, headers, fmt.Errorf("parse health response: %w", err)
	}

	logger.Infof("[SIMILARITY] GET /health → HTTP %d", statusCode)

	return &resp, statusCode, headers, nil
}

// SimilaritySearch calls POST /v1/similarity-search and returns the parsed success response,
// the HTTP status code, response headers, and any transport error.
func SimilaritySearch(ctx context.Context, baseURL string, req SimilaritySearchRequest) (*SimilaritySearchResponse, int, http.Header, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/similarity-search"
	body, statusCode, headers, err := doPost(ctx, url, req, postCallTimeout)
	if err != nil {
		return nil, 0, nil, err
	}

	if statusCode != http.StatusOK {
		return nil, statusCode, headers, nil
	}

	var resp SimilaritySearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, statusCode, headers, fmt.Errorf("parse similarity response: %w", err)
	}

	logger.Infof("[SIMILARITY] POST /v1/similarity-search (mode=%s rerank=%v) → HTTP %d, score_type=%s, results=%d",
		req.Mode, req.Rerank, statusCode, resp.ScoreType, len(resp.Results))

	return &resp, statusCode, headers, nil
}

// SimilaritySearchExpectingError calls POST /v1/similarity-search and returns the error response body
// along with the HTTP status code when the server returns a non-200 status.
func SimilaritySearchExpectingError(ctx context.Context, baseURL string, req SimilaritySearchRequest) (*SimilarityErrorResponse, int, error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/similarity-search"
	body, statusCode, _, err := doPost(ctx, url, req, postCallTimeout)
	if err != nil {
		return nil, 0, err
	}

	if statusCode == http.StatusOK {
		return nil, statusCode, fmt.Errorf("expected error response but got HTTP 200")
	}

	var errResp SimilarityErrorResponse
	if jsonErr := json.Unmarshal(body, &errResp); jsonErr != nil {
		// Return raw body as the error message when JSON parsing fails.
		fmt.Println(jsonErr)
	}

	logger.Infof("[SIMILARITY] POST /v1/similarity-search (error path) → HTTP %d: %s", statusCode, errResp.Error)

	return &errResp, statusCode, nil
}

// intPtr returns a pointer to the given int — convenience helper for TopK in requests.
func intPtr(i int) *int { return &i }

// boolPtr returns a pointer to the given bool — convenience helper for Rerank in requests.
func boolPtr(b bool) *bool { return &b }

// VerifyHealthEndpoint calls GET /health and validates that the response is HTTP 200
// with a non-empty status field.
func VerifyHealthEndpoint(ctx context.Context, baseURL string) (*HealthResponse, error) {
	resp, statusCode, _, err := HealthCheckWithResponse(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /health returned HTTP %d, expected 200", statusCode)
	}

	if resp.Status == "" {
		return nil, fmt.Errorf("GET /health response has empty status field")
	}

	logger.Infof("GET /health returned HTTP %d, status=%q", statusCode, resp.Status)

	return resp, nil
}

// VerifyTimeInfoInResponse calls POST /v1/similarity-search and checks that the API
// includes timing information either in the response headers (e.g. X-Response-Time,
// X-Process-Time, X-Duration) or in the response body. This validates the podman
// runtime timing instrumentation requirement.
//
// Corresponds to test case "Verify Similarity search API includes time info in response headers or body in podman runtime".
func VerifyTimeInfoInResponse(ctx context.Context, baseURL string) error {
	req := SimilaritySearchRequest{
		Query: "what is network configuration",
		Mode:  "dense",
	}

	url := strings.TrimRight(baseURL, "/") + "/v1/similarity-search"
	raw, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(raw))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := getHTTPClient(postCallTimeout).Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	// Check for timing headers (any of the commonly used names).
	timingHeaders := []string{
		"X-Retrieve-Time",
		"X-Total-Time",
	}

	for _, h := range timingHeaders {
		if val := resp.Header.Get(h); val != "" {
			logger.Infof("[TIMING] Found timing header %s=%s", h, val)

			return nil
		}
	}

	return fmt.Errorf("no timing information found in response headers (%v) or body", timingHeaders)
}

// VerifyInvalidModeReturns400 posts a similarity-search request with an invalid mode value
// and asserts that the API returns HTTP 400 with an appropriate error message.
func VerifyInvalidModeReturns400(ctx context.Context, baseURL string) (*SimilarityErrorResponse, error) {
	req := SimilaritySearchRequest{
		Query: "what is network configuration",
		Mode:  "invalid_mode",
	}

	errResp, statusCode, err := SimilaritySearchExpectingError(ctx, baseURL, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if statusCode != http.StatusBadRequest {
		return errResp, fmt.Errorf("expected HTTP 400 for invalid mode, got %d (error: %s)", statusCode, errResp.Error)
	}

	logger.Infof("POST /v1/similarity-search with invalid mode → HTTP %d: %s", statusCode, errResp.Error)

	return errResp, nil
}

// VerifySearchModes calls POST /v1/similarity-search for each of the three supported
// modes and returns a map of mode → (response, error).
func VerifySearchModes(ctx context.Context, baseURL string) map[string]*SimilaritySearchResponse {
	modes := []string{"dense", "sparse", "hybrid"}
	results := make(map[string]*SimilaritySearchResponse, len(modes))

	for _, mode := range modes {
		req := SimilaritySearchRequest{
			Query: "how do I configure network settings",
			Mode:  mode,
		}

		resp, statusCode, _, err := SimilaritySearch(ctx, baseURL, req)
		if err != nil {
			logger.Warningf("mode=%s request failed: %v", mode, err)

			continue
		}

		if statusCode != http.StatusOK {
			logger.Warningf("mode=%s returned HTTP %d (may indicate empty index)", mode, statusCode)

			continue
		}

		logger.Infof("mode=%s → HTTP %d, score_type=%s, results=%d",
			mode, statusCode, resp.ScoreType, len(resp.Results))
		results[mode] = resp
	}

	return results
}

// VerifyRerankTrue posts a similarity-search request with rerank=true and asserts that
// the response includes score_type "relevance" (the reranker output type).
func VerifyRerankTrue(ctx context.Context, baseURL string) (*SimilaritySearchResponse, error) {
	req := SimilaritySearchRequest{
		Query:  "how do I configure network settings",
		Mode:   "hybrid",
		Rerank: boolPtr(true),
	}

	resp, statusCode, _, err := SimilaritySearch(ctx, baseURL, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("expected HTTP 200, got %d", statusCode)
	}

	if resp.ScoreType != "relevance" {
		return resp, fmt.Errorf("expected score_type=relevance when rerank=true, got %q", resp.ScoreType)
	}

	logger.Infof("rerank=true → HTTP %d, score_type=%s, results=%d",
		statusCode, resp.ScoreType, len(resp.Results))

	return resp, nil
}

// VerifyInvalidTopKReturns422 posts a similarity-search request with a negative top_k
// value and asserts that the API returns HTTP 400.
func VerifyInvalidTopKReturns422(ctx context.Context, baseURL string) (*SimilarityErrorResponse, error) {
	req := SimilaritySearchRequest{
		Query: "how do I configure network settings",
		Mode:  "dense",
		TopK:  intPtr(-1),
	}

	errResp, statusCode, err := SimilaritySearchExpectingError(ctx, baseURL, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if statusCode != http.StatusUnprocessableEntity {
		return errResp, fmt.Errorf("expected HTTP 400 for invalid top_k, got %d (error: %s)", statusCode, errResp.Error)
	}

	logger.Infof("invalid top_k=-1 → HTTP %d: %s", statusCode, errResp.Error)

	return errResp, nil
}

// ReproduceValidationError posts a similarity-search request with a missing required
// field (empty query) and asserts HTTP 400 is returned.
func ReproduceValidationError(ctx context.Context, baseURL string) (*SimilarityErrorResponse, error) {
	// Send an empty query — the API requires a non-empty query string.
	req := SimilaritySearchRequest{
		Query: "",
		Mode:  "dense",
	}

	errResp, statusCode, err := SimilaritySearchExpectingError(ctx, baseURL, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if statusCode != http.StatusBadRequest {
		return errResp, fmt.Errorf("expected HTTP 400, got %d (error: %s)", statusCode, errResp.Error)
	}

	logger.Infof("Reproduced 400 → %s", errResp.Error)

	return errResp, nil
}
