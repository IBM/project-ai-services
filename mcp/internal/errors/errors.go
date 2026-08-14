package errors

import (
	"fmt"
)

// UsageError represents an error in CLI usage
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string {
	return e.Message
}

// NewUsageError creates a new usage error
func NewUsageError(msg string, args ...interface{}) *UsageError {
	return &UsageError{
		Message: fmt.Sprintf(msg, args...),
	}
}

// AuthenticationError represents an authentication error
type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string {
	return e.Message
}

// NewAuthenticationError creates a new authentication error
func NewAuthenticationError(msg string, args ...interface{}) *AuthenticationError {
	return &AuthenticationError{
		Message: fmt.Sprintf(msg, args...),
	}
}

// ConfigurationError represents a configuration error
type ConfigurationError struct {
	Message string
}

func (e *ConfigurationError) Error() string {
	return e.Message
}

// NewConfigurationError creates a new configuration error
func NewConfigurationError(msg string, args ...interface{}) *ConfigurationError {
	return &ConfigurationError{
		Message: fmt.Sprintf(msg, args...),
	}
}

// APIError represents an API call error
type APIError struct {
	Message    string
	StatusCode int
	Response   string
}

func (e *APIError) Error() string {
	return e.Message
}

// NewAPIError creates a new API error
func NewAPIError(msg string, statusCode int, response string) *APIError {
	return &APIError{
		Message:    msg,
		StatusCode: statusCode,
		Response:   response,
	}
}
