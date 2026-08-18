// Package rag — similarity search helpers for E2E failure tests.
//
// PostSimilaritySearchRaw is used exclusively by TC-4 (connectivity failure)
// in similarity_failure_test.go.  It must live here because it depends on
// buildPostJSONRequest and sharedRAGClient, which are unexported in this package.
//
// The ValidateSimilarity* functions and parseSimilarityErrorBody have been moved
// to the similarity package (similarity/similarity.go), which is the natural home
// for all similarity-api response validation logic.
package rag

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// similaritySearchPath is the POST endpoint on the similarity service.
const similaritySearchPath = "/v1/similarity-search"

// PostSimilaritySearchRaw sends a POST to similarityBaseURL/v1/similarity-search
// and always returns the raw HTTP status code and response body, regardless of
// whether the request succeeded or failed.
//
// On a transport-level error (DNS failure, connection refused, context cancelled)
// statusCode will be 0 and rawBody will be empty.
//
// Reuse of existing HTTP infrastructure:
//   - buildPostJSONRequest (evaluator.go) — reused for JSON marshalling, Content-Type,
//     Bearer-token injection, and the shared TLS-skipping transport configuration.
//   - sharedRAGClient (evaluator.go)      — reused as the single pooled HTTP client for
//     all RAG requests, keeping connection behaviour consistent.
//
// PostJSON (evaluator.go) cannot be reused here because it calls handlePostJSONResponse,
// which discards the response body via io.Discard on non-200.  Failure tests must inspect
// the JSON error envelope {"error":{"code":"...","message":"...","status":N}} that the
// similarity service returns on 4xx/5xx, so the body must always be returned as-is.
func PostSimilaritySearchRaw(
	ctx context.Context,
	baseURL string,
	body map[string]any,
) (statusCode int, rawBody string, err error) {
	req, err := buildPostJSONRequest(ctx, baseURL, similaritySearchPath, body)
	if err != nil {
		return 0, "", fmt.Errorf("build similarity search request: %w", err)
	}

	start := time.Now()
	resp, err := sharedRAGClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		logger.Infof("[SIM][http] POST %s%s — transport error after %s: %v",
			baseURL, similaritySearchPath, elapsed.Round(time.Millisecond), err)

		return 0, "", fmt.Errorf("similarity search request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return resp.StatusCode, "", fmt.Errorf("read similarity search response body: %w", readErr)
	}

	logger.Infof("[SIM][http] POST %s%s → HTTP %d in %s",
		baseURL, similaritySearchPath, resp.StatusCode, elapsed.Round(time.Millisecond))

	return resp.StatusCode, string(bodyBytes), nil
}

