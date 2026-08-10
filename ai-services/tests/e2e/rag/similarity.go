// Package rag — similarity search helpers for E2E failure tests.
//
// This file provides PostSimilaritySearchRaw and a set of ValidateSimilarity*
// functions used by similarity_failure_test.go.
//
// Why PostSimilaritySearchRaw and not PostJSON?
//
//	PostJSON (evaluator.go) calls handlePostJSONResponse which discards the
//	response body on non-200 via io.Discard.  Failure tests need to inspect the
//	JSON error envelope {"error":{"code":"...","message":"...","status":N}} that
//	the similarity service returns on 4xx/5xx, so a Raw variant that always
//	returns the body is required.
package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// similaritySearchPath is the POST endpoint on the similarity service.
const similaritySearchPath = "/v1/similarity-search"

// HTTP status codes used by the similarity service validate functions.
const (
	httpStatusBadRequest          = 400
	httpStatusUnprocessableEntity = 422
	httpStatusServiceUnavailable  = 503
)

// PostSimilaritySearchRaw sends a POST to similarityBaseURL/v1/similarity-search
// and always returns the raw HTTP status code and response body, regardless of
// whether the request succeeded or failed.
//
// On a transport-level error (DNS failure, connection refused, context cancelled)
// statusCode will be 0 and rawBody will be empty.
//
// Reuses buildPostJSONRequest (evaluator.go) for consistent TLS, JSON marshalling,
// and Bearer-token handling, then drives sharedRAGClient directly so the body is
// not discarded on non-200 responses.
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

// ─────────────────────────────────────────────────────────────────────────────
// Private helpers
// ─────────────────────────────────────────────────────────────────────────────

// similarityErrorEnvelope mirrors the JSON error shape returned by every
// similarity service endpoint:
//
//	{"error":{"code":"...","message":"...","status":N}}
type similarityErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	} `json:"error"`
}

// parseSimilarityErrorBody decodes rawBody into the standard error envelope.
// Returns a non-nil err if the body cannot be decoded or the envelope is empty.
func parseSimilarityErrorBody(rawBody string) (code, message string, status int, err error) {
	var env similarityErrorEnvelope
	if jsonErr := json.Unmarshal([]byte(rawBody), &env); jsonErr != nil {
		return "", "", 0, fmt.Errorf("parse similarity error envelope: %w — body: %s", jsonErr, rawBody)
	}
	if env.Error.Code == "" {
		return "", "", 0, fmt.Errorf("similarity error envelope missing 'code' field — body: %s", rawBody)
	}

	return env.Error.Code, env.Error.Message, env.Error.Status, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Validate* functions — one per failure scenario
// ─────────────────────────────────────────────────────────────────────────────

// ValidateSimilarityEmptyQueryError asserts that the service correctly rejected
// an empty query with HTTP 400 and error code EMPTY_INPUT.
func ValidateSimilarityEmptyQueryError(statusCode int, rawBody string) error {
	if statusCode != httpStatusBadRequest {
		return fmt.Errorf("expected HTTP 400 for empty query, got %d — body: %s", statusCode, rawBody)
	}
	code, message, _, err := parseSimilarityErrorBody(rawBody)
	if err != nil {
		return err
	}
	if code != "EMPTY_INPUT" {
		return fmt.Errorf("expected error code EMPTY_INPUT, got %q — body: %s", code, rawBody)
	}
	if !strings.Contains(message, "query is required") {
		return fmt.Errorf("expected message to contain %q, got %q", "query is required", message)
	}

	return nil
}

// ValidateSimilarityInvalidModeError asserts that the service correctly rejected
// an unsupported mode value with HTTP 400 and error code INVALID_PARAMETER.
func ValidateSimilarityInvalidModeError(statusCode int, rawBody string) error {
	if statusCode != httpStatusBadRequest {
		return fmt.Errorf("expected HTTP 400 for invalid mode, got %d — body: %s", statusCode, rawBody)
	}
	code, message, _, err := parseSimilarityErrorBody(rawBody)
	if err != nil {
		return err
	}
	if code != "INVALID_PARAMETER" {
		return fmt.Errorf("expected error code INVALID_PARAMETER, got %q — body: %s", code, rawBody)
	}
	if !strings.Contains(message, "mode must be one of") {
		return fmt.Errorf("expected message to contain %q, got %q", "mode must be one of", message)
	}

	return nil
}

// ValidateSimilarityInvalidTopKError asserts that the service correctly rejected
// a top_k value below the minimum (ge=1) with HTTP 422 (Pydantic validation).
//
// FastAPI's 422 body uses {"detail":[...]} rather than the custom error envelope,
// so this validator checks for the HTTP status and the presence of "top_k" in
// the raw body — a shape-agnostic assertion that works for both response formats.
func ValidateSimilarityInvalidTopKError(statusCode int, rawBody string) error {
	if statusCode != httpStatusUnprocessableEntity {
		return fmt.Errorf("expected HTTP 422 for invalid top_k, got %d — body: %s", statusCode, rawBody)
	}
	if !strings.Contains(rawBody, "top_k") {
		return fmt.Errorf("expected response body to reference %q for top_k validation error — body: %s", "top_k", rawBody)
	}

	return nil
}

// ValidateSimilarityUnreachableError asserts that a call to an unreachable host
// produced a transport-level error (not a structured API error).
//
// Checks that err is non-nil and is NOT ErrNonRetriable (which would indicate a
// structured HTTP response was received), and that the error message contains at
// least one of the expected connectivity-failure keywords.
func ValidateSimilarityUnreachableError(err error) error {
	if err == nil {
		return errors.New("expected a transport error for unreachable similarity API, but got nil")
	}
	if errors.Is(err, ErrNonRetriable) {
		return fmt.Errorf("expected transport error, got structured API error (ErrNonRetriable): %w", err)
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
	if statusCode != httpStatusServiceUnavailable {
		return fmt.Errorf("expected HTTP 503 for empty index, got %d — body: %s", statusCode, rawBody)
	}
	code, message, _, err := parseSimilarityErrorBody(rawBody)
	if err != nil {
		return err
	}
	if code != "VECTOR_STORE_NOT_READY" {
		return fmt.Errorf("expected error code VECTOR_STORE_NOT_READY, got %q — body: %s", code, rawBody)
	}
	if !strings.Contains(message, "Ingest documents first") {
		return fmt.Errorf("expected message to contain %q, got %q", "Ingest documents first", message)
	}

	return nil
}
