package similarity

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
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
	Error  ErrorResponse       `json:"error"`
	Detail interface{} `json:"detail"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status"`
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

func getHealthEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/health"
}


func getSimilaritySearchEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/similarity-search"
}

// HealthCheckWithResponse calls GET /health and returns the parsed response, status code, and headers.
func HealthCheckWithResponse(ctx context.Context, baseURL string) (*HealthResponse, int, http.Header, error) {
	url := getHealthEndpoint(baseURL)
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
	url := getSimilaritySearchEndpoint(baseURL)
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
	url := getSimilaritySearchEndpoint(baseURL)
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
// includes timing information either in the response headers (e.g. X-Retrieve-Time,
// X-Total-Time) or in the response body. This validates the podman
// runtime timing instrumentation requirement.
//
// Corresponds to test case "Verify Similarity search API includes time info in response headers or body in podman runtime".
func VerifyTimeInfoInResponse(ctx context.Context, baseURL string) error {
	req := SimilaritySearchRequest{
		Query: "what is network configuration",
		Mode:  "dense",
	}

	url := getSimilaritySearchEndpoint(baseURL)
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
		return errResp, fmt.Errorf("expected HTTP 400 for invalid mode, got %d", statusCode)
	}

	logger.Infof("POST /v1/similarity-search with invalid mode → HTTP %d: %v", statusCode, errResp.Error)

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
		return errResp, fmt.Errorf("expected HTTP 422 for invalid top_k, got %d", statusCode)
	}

	logger.Infof("invalid top_k=-1 → HTTP %d: %v", statusCode, errResp.Error)

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
		return errResp, fmt.Errorf("expected HTTP 400, got %d", statusCode)
	}

	return errResp, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Failure-path validators — used by similarity_failure_test.go
// ─────────────────────────────────────────────────────────────────────────────

// parseSimilarityErrorBody decodes rawBody into SimilarityErrorResponse.
// Returns a non-nil err if the body cannot be decoded or Error.Code is empty.
func parseSimilarityErrorBody(rawBody string) (*SimilarityErrorResponse, error) {
	var env SimilarityErrorResponse
	if err := json.Unmarshal([]byte(rawBody), &env); err != nil {
		return nil, fmt.Errorf("parse similarity error envelope: %w — body: %s", err, rawBody)
	}
	if env.Error.Code == "" {
		return nil, fmt.Errorf("similarity error envelope missing 'code' field — body: %s", rawBody)
	}

	return &env, nil
}

// ValidateSimilarityEmptyQueryError asserts that the service correctly rejected
// an empty query with HTTP 400 and error code EMPTY_INPUT.
func ValidateSimilarityEmptyQueryError(statusCode int, rawBody string) error {
	if statusCode != http.StatusBadRequest {
		return fmt.Errorf("expected HTTP 400 for empty query, got %d — body: %s", statusCode, rawBody)
	}
	env, err := parseSimilarityErrorBody(rawBody)
	if err != nil {
		return err
	}
	if env.Error.Code != "EMPTY_INPUT" {
		return fmt.Errorf("expected error code EMPTY_INPUT, got %q — body: %s", env.Error.Code, rawBody)
	}
	if !strings.Contains(env.Error.Message, "query is required") {
		return fmt.Errorf("expected message to contain %q, got %q", "query is required", env.Error.Message)
	}

	return nil
}

// ValidateSimilarityInvalidModeError asserts that the service correctly rejected
// an unsupported mode value with HTTP 400 and error code INVALID_PARAMETER.
func ValidateSimilarityInvalidModeError(statusCode int, rawBody string) error {
	if statusCode != http.StatusBadRequest {
		return fmt.Errorf("expected HTTP 400 for invalid mode, got %d — body: %s", statusCode, rawBody)
	}
	env, err := parseSimilarityErrorBody(rawBody)
	if err != nil {
		return err
	}
	if env.Error.Code != "INVALID_PARAMETER" {
		return fmt.Errorf("expected error code INVALID_PARAMETER, got %q — body: %s", env.Error.Code, rawBody)
	}
	if !strings.Contains(env.Error.Message, "mode must be one of") {
		return fmt.Errorf("expected message to contain %q, got %q", "mode must be one of", env.Error.Message)
	}

	return nil
}

// pydanticDetailEntry is one element of the FastAPI 422 {"detail":[...]} array.
type pydanticDetailEntry struct {
	Loc  []interface{} `json:"loc"`
	Msg  string        `json:"msg"`
	Type string        `json:"type"`
}

// findTopKDetailEntry searches detail entries for one whose loc contains "top_k"
// and whose msg contains "greater than or equal to".
// Returns an error if no matching entry is found or the message is wrong.
func findTopKDetailEntry(details []pydanticDetailEntry) error {
	for _, d := range details {
		for _, loc := range d.Loc {
			if locStr, ok := loc.(string); ok && locStr == "top_k" {
				if !strings.Contains(d.Msg, "greater than or equal to") {
					return fmt.Errorf("expected top_k detail msg to contain %q, got %q",
						"greater than or equal to", d.Msg)
				}

				return nil
			}
		}
	}

	return fmt.Errorf("expected a detail entry with loc containing %q", "top_k")
}

// parseTopK422Body unmarshals a FastAPI 422 body into a slice of pydanticDetailEntry.
func parseTopK422Body(rawBody string) ([]pydanticDetailEntry, error) {
	var env SimilarityErrorResponse
	if err := json.Unmarshal([]byte(rawBody), &env); err != nil {
		return nil, fmt.Errorf("parse 422 response body: %w — body: %s", err, rawBody)
	}

	detailBytes, err := json.Marshal(env.Detail)
	if err != nil {
		return nil, fmt.Errorf("re-marshal detail field: %w — body: %s", err, rawBody)
	}

	var details []pydanticDetailEntry
	if err := json.Unmarshal(detailBytes, &details); err != nil || len(details) == 0 {
		return nil, fmt.Errorf("expected non-empty detail array in 422 body — body: %s", rawBody)
	}

	return details, nil
}

// ValidateSimilarityInvalidTopKError asserts that the service correctly rejected
// a top_k value below the minimum (ge=1) with HTTP 422 (Pydantic validation).
//
// FastAPI 422 bodies use {"detail":[{"loc":[...],"msg":"...","type":"..."}]}
// rather than the custom error envelope.  This validator checks:
//   - HTTP status is 422
//   - at least one detail entry references "top_k" in its loc array
//   - the entry's msg contains the expected constraint phrase
func ValidateSimilarityInvalidTopKError(statusCode int, rawBody string) error {
	if statusCode != http.StatusUnprocessableEntity {
		return fmt.Errorf("expected HTTP 422 for invalid top_k, got %d — body: %s", statusCode, rawBody)
	}

	details, err := parseTopK422Body(rawBody)
	if err != nil {
		return err
	}

	return findTopKDetailEntry(details)
}

// ValidateSimilarityUnreachableError asserts that a call to an unreachable host
// produced a transport-level error, by checking that err is non-nil and its
// message contains at least one known connectivity-failure keyword.
//
// Note: this validator is used exclusively with PostSimilaritySearchRaw (rag package),
// which preserves raw transport errors.  Do not use with SimilaritySearchExpectingError,
// which wraps transport errors as "request failed: %w".
func ValidateSimilarityUnreachableError(err error) error {
	if err == nil {
		return errors.New("expected a transport error for unreachable similarity API, but got nil")
	}
	msg := strings.ToLower(err.Error())
	keywords := []string{"connection", "dial", "no such host", "i/o timeout", "context deadline exceeded"}
	for _, kw := range keywords {
		if strings.Contains(msg, kw) {
			return nil
		}
	}

	return fmt.Errorf(
		"expected transport error to contain one of %v — got: %s",
		keywords, err.Error(),
	)
}

// ValidateSimilarityNotReadyError asserts that the service correctly reported an
// empty vector index with HTTP 503 and error code VECTOR_STORE_NOT_READY.
func ValidateSimilarityNotReadyError(statusCode int, rawBody string) error {
	if statusCode != http.StatusServiceUnavailable {
		return fmt.Errorf("expected HTTP 503 for empty index, got %d — body: %s", statusCode, rawBody)
	}
	env, err := parseSimilarityErrorBody(rawBody)
	if err != nil {
		return err
	}
	if env.Error.Code != "VECTOR_STORE_NOT_READY" {
		return fmt.Errorf("expected error code VECTOR_STORE_NOT_READY, got %q — body: %s", env.Error.Code, rawBody)
	}
	if !strings.Contains(env.Error.Message, "Ingest documents first") {
		return fmt.Errorf("expected message to contain %q, got %q", "Ingest documents first", env.Error.Message)
	}

	return nil
}
