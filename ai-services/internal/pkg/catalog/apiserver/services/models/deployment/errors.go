package deployment

import (
	"fmt"
	"net/http"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// newError builds an API-facing error carrying the HTTP status the handler should return.
// Anything that is not a *validators.ValidationError is treated as an internal (500) failure.
func newError(code int, format string, args ...any) *validators.ValidationError {
	return &validators.ValidationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// BadRequest returns an error that maps to HTTP 400.
func BadRequest(format string, args ...any) *validators.ValidationError {
	return newError(http.StatusBadRequest, format, args...)
}

// NotFound returns an error that maps to HTTP 404.
func NotFound(format string, args ...any) *validators.ValidationError {
	return newError(http.StatusNotFound, format, args...)
}

// Conflict returns an error that maps to HTTP 409.
func Conflict(format string, args ...any) *validators.ValidationError {
	return newError(http.StatusConflict, format, args...)
}

// Unprocessable returns an error that maps to HTTP 422.
func Unprocessable(format string, args ...any) *validators.ValidationError {
	return newError(http.StatusUnprocessableEntity, format, args...)
}
