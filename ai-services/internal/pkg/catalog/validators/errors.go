package validators

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

// newError builds an API-facing error carrying the HTTP status the handler should return.
// Anything that is not a *ValidationError is treated as an internal (500) failure.
func newError(code int, format string, args ...any) *ValidationError {
	return &ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// BadRequest returns an error that maps to HTTP 400.
func BadRequest(format string, args ...any) *ValidationError {
	return newError(http.StatusBadRequest, format, args...)
}

// NotFound returns an error that maps to HTTP 404.
func NotFound(format string, args ...any) *ValidationError {
	return newError(http.StatusNotFound, format, args...)
}

// Forbidden returns an error that maps to HTTP 403.
func Forbidden(format string, args ...any) *ValidationError {
	return newError(http.StatusForbidden, format, args...)
}

// Conflict returns an error that maps to HTTP 409.
func Conflict(format string, args ...any) *ValidationError {
	return newError(http.StatusConflict, format, args...)
}

// BadGateway returns an error that maps to HTTP 502.
func BadGateway(format string, args ...any) *ValidationError {
	return newError(http.StatusBadGateway, format, args...)
}

// Unprocessable returns an error that maps to HTTP 422.
func Unprocessable(format string, args ...any) *ValidationError {
	return newError(http.StatusUnprocessableEntity, format, args...)
}

// SanitizeProbeError trims raw technical stack traces, connection internals,
// or JSON responses from a LiteLLM health probe into a short, human-readable message for the UI.
func SanitizeProbeError(raw string) string {
	if raw == "" {
		return ""
	}

	raw = extractJSONEndpointError(raw)

	if isConnectionRefused(raw) {
		return "Pod does not exist or connection refused"
	}

	if isTimeout(raw) {
		return "Connection to model endpoint timed out"
	}

	if isAuthFailure(raw) {
		return "Authentication failed: invalid API credentials"
	}

	if tbIdx := strings.Index(raw, "\n"); tbIdx != -1 {
		raw = raw[:tbIdx]
	}

	return fmt.Sprintf("Health probe failed: %s", strings.TrimSpace(raw))
}

func extractJSONEndpointError(raw string) string {
	idx := strings.Index(raw, "{")
	if idx == -1 {
		return raw
	}

	var healthResp catalogclient.LiteLLMHealthResponse
	if err := json.Unmarshal([]byte(raw[idx:]), &healthResp); err == nil && len(healthResp.UnhealthyEndpoints) > 0 {
		return healthResp.UnhealthyEndpoints[0].Error
	}

	return raw
}

func isConnectionRefused(msg string) bool {
	return strings.Contains(msg, "ConnectionRefused") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "Cannot connect to host") ||
		strings.Contains(msg, "Connect call failed")
}

func isTimeout(msg string) bool {
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "Client.Timeout exceeded") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "Timeout")
}

func isAuthFailure(msg string) bool {
	return strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "AuthenticationError") ||
		strings.Contains(msg, "401") ||
		strings.Contains(msg, "Unauthorized")
}
