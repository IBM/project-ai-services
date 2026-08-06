package errors

import (
	"testing"
)

func TestUsageError(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		args     []interface{}
		expected string
	}{
		{
			name:     "simple message",
			msg:      "invalid argument",
			args:     nil,
			expected: "invalid argument",
		},
		{
			name:     "formatted message",
			msg:      "invalid argument: %s",
			args:     []interface{}{"--test"},
			expected: "invalid argument: --test",
		},
		{
			name:     "multiple format args",
			msg:      "expected %d arguments, got %d",
			args:     []interface{}{2, 3},
			expected: "expected 2 arguments, got 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewUsageError(tt.msg, tt.args...)
			if err.Error() != tt.expected {
				t.Errorf("UsageError.Error() = %q, want %q", err.Error(), tt.expected)
			}
			if err.Message != tt.expected {
				t.Errorf("UsageError.Message = %q, want %q", err.Message, tt.expected)
			}
		})
	}
}

func TestAuthenticationError(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		args     []interface{}
		expected string
	}{
		{
			name:     "simple auth error",
			msg:      "authentication failed",
			args:     nil,
			expected: "authentication failed",
		},
		{
			name:     "auth error with details",
			msg:      "authentication failed for user: %s",
			args:     []interface{}{"testuser"},
			expected: "authentication failed for user: testuser",
		},
		{
			name:     "auth error with code",
			msg:      "authentication failed with code %d: %s",
			args:     []interface{}{401, "unauthorized"},
			expected: "authentication failed with code 401: unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAuthenticationError(tt.msg, tt.args...)
			if err.Error() != tt.expected {
				t.Errorf("AuthenticationError.Error() = %q, want %q", err.Error(), tt.expected)
			}
			if err.Message != tt.expected {
				t.Errorf("AuthenticationError.Message = %q, want %q", err.Message, tt.expected)
			}
		})
	}
}

func TestConfigurationError(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		args     []interface{}
		expected string
	}{
		{
			name:     "simple config error",
			msg:      "missing configuration",
			args:     nil,
			expected: "missing configuration",
		},
		{
			name:     "config error with field",
			msg:      "invalid configuration field: %s",
			args:     []interface{}{"api_key"},
			expected: "invalid configuration field: api_key",
		},
		{
			name:     "config error with path",
			msg:      "cannot read config file %s: %s",
			args:     []interface{}{"/path/to/config", "permission denied"},
			expected: "cannot read config file /path/to/config: permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewConfigurationError(tt.msg, tt.args...)
			if err.Error() != tt.expected {
				t.Errorf("ConfigurationError.Error() = %q, want %q", err.Error(), tt.expected)
			}
			if err.Message != tt.expected {
				t.Errorf("ConfigurationError.Message = %q, want %q", err.Message, tt.expected)
			}
		})
	}
}

func TestAPIError(t *testing.T) {
	tests := []struct {
		name        string
		msg         string
		statusCode  int
		response    string
		expectedMsg string
	}{
		{
			name:        "simple API error",
			msg:         "API request failed",
			statusCode:  500,
			response:    "Internal Server Error",
			expectedMsg: "API request failed",
		},
		{
			name:        "unauthorized error",
			msg:         "Unauthorized access",
			statusCode:  401,
			response:    `{"error": "invalid token"}`,
			expectedMsg: "Unauthorized access",
		},
		{
			name:        "not found error",
			msg:         "Resource not found",
			statusCode:  404,
			response:    "The requested resource does not exist",
			expectedMsg: "Resource not found",
		},
		{
			name:        "rate limit error",
			msg:         "Rate limit exceeded",
			statusCode:  429,
			response:    `{"retry_after": 60}`,
			expectedMsg: "Rate limit exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAPIError(tt.msg, tt.statusCode, tt.response)

			if err.Error() != tt.expectedMsg {
				t.Errorf("APIError.Error() = %q, want %q", err.Error(), tt.expectedMsg)
			}

			if err.Message != tt.msg {
				t.Errorf("APIError.Message = %q, want %q", err.Message, tt.msg)
			}

			if err.StatusCode != tt.statusCode {
				t.Errorf("APIError.StatusCode = %d, want %d", err.StatusCode, tt.statusCode)
			}

			if err.Response != tt.response {
				t.Errorf("APIError.Response = %q, want %q", err.Response, tt.response)
			}
		})
	}
}

func TestErrorInterface(t *testing.T) {
	t.Run("UsageError implements error", func(t *testing.T) {
		var err error = NewUsageError("test")
		if _, ok := err.(*UsageError); !ok {
			t.Error("UsageError does not implement error interface properly")
		}
	})

	t.Run("AuthenticationError implements error", func(t *testing.T) {
		var err error = NewAuthenticationError("test")
		if _, ok := err.(*AuthenticationError); !ok {
			t.Error("AuthenticationError does not implement error interface properly")
		}
	})

	t.Run("ConfigurationError implements error", func(t *testing.T) {
		var err error = NewConfigurationError("test")
		if _, ok := err.(*ConfigurationError); !ok {
			t.Error("ConfigurationError does not implement error interface properly")
		}
	})

	t.Run("APIError implements error", func(t *testing.T) {
		var err error = NewAPIError("test", 500, "response")
		if _, ok := err.(*APIError); !ok {
			t.Error("APIError does not implement error interface properly")
		}
	})
}

func TestErrorFormatting(t *testing.T) {
	t.Run("simple format", func(t *testing.T) {
		err := NewUsageError("test error")
		expected := "test error"
		if err.Error() != expected {
			t.Errorf("Error formatting failed: got %q, want %q", err.Error(), expected)
		}
	})

	t.Run("format with args", func(t *testing.T) {
		err := NewUsageError("test %s", "arg1")
		expected := "test arg1"
		if err.Error() != expected {
			t.Errorf("Error formatting with args failed: got %q, want %q", err.Error(), expected)
		}
	})

	t.Run("format with multiple args", func(t *testing.T) {
		err := NewUsageError("test %s and %d", "arg1", 42)
		expected := "test arg1 and 42"
		if err.Error() != expected {
			t.Errorf("Error formatting with multiple args failed: got %q, want %q", err.Error(), expected)
		}
	})

	t.Run("format with percent literal", func(t *testing.T) {
		err := NewUsageError("test 100%% complete")
		expected := "test 100% complete"
		if err.Error() != expected {
			t.Errorf("Error formatting with percent literal failed: got %q, want %q", err.Error(), expected)
		}
	})
}
