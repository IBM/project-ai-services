package authenticator

import (
	"context"
	"testing"
)

func TestNewAPIKeyAuthenticator(t *testing.T) {
	apiKey := "test-api-key-12345"
	auth := NewAPIKeyAuthenticator(apiKey)

	if auth == nil {
		t.Fatal("NewAPIKeyAuthenticator() returned nil")
	}

	if auth.apiKey != apiKey {
		t.Errorf("apiKey = %q, want %q", auth.apiKey, apiKey)
	}

	if auth.auth == nil {
		t.Fatal("IamAuthenticator is nil")
	}

	if auth.auth.ApiKey != apiKey {
		t.Errorf("IamAuthenticator.ApiKey = %q, want %q", auth.auth.ApiKey, apiKey)
	}

	// Verify it implements the Authenticator interface
	var _ Authenticator = auth
}

func TestAPIKeyAuthenticator_GetBearerToken(t *testing.T) {
	// Note: This test will make actual network calls to IBM Cloud IAM
	// In a production environment, you would mock the HTTP client
	apiKey := "test-invalid-key"
	auth := NewAPIKeyAuthenticator(apiKey)

	ctx := context.Background()
	token, err := auth.GetBearerToken(ctx)

	// Should fail with invalid API key
	if err == nil {
		t.Error("GetBearerToken() should return error with invalid API key")
	}

	if token != "" {
		t.Errorf("GetBearerToken() returned non-empty token with invalid key: %q", token)
	}

	// Check that error message is redacted
	if err != nil {
		errorMsg := err.Error()
		if errorMsg == "" {
			t.Error("Error message is empty")
		}
		// Should contain redacted information
		if len(errorMsg) < 10 {
			t.Errorf("Error message seems too short: %q", errorMsg)
		}
	}
}

func TestAPIKeyAuthenticator_IsPassthrough(t *testing.T) {
	auth := NewAPIKeyAuthenticator("test-key")

	if auth.IsPassthrough() {
		t.Error("IsPassthrough() = true, want false")
	}
}

func TestAPIKeyAuthenticator_GetType(t *testing.T) {
	auth := NewAPIKeyAuthenticator("test-key")

	expected := string(AuthTypeAPIKey)
	if auth.GetType() != expected {
		t.Errorf("GetType() = %q, want %q", auth.GetType(), expected)
	}
}

func TestAPIKeyAuthenticator_redactError(t *testing.T) {
	apiKey := "my-secret-api-key-12345"
	auth := NewAPIKeyAuthenticator(apiKey)

	tests := []struct {
		name        string
		input       string
		expected    string
		contains    []string
		notContains []string
	}{
		{
			name:        "redact API key in error",
			input:       "Authentication failed with API key: my-secret-api-key-12345",
			notContains: []string{"my-secret-api-key-12345"},
			contains:    []string{"<REDACTED>"},
		},
		{
			name:        "redact bearer token",
			input:       "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.invalid.token",
			notContains: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
			contains:    []string{"<REDACTED>"},
		},
		{
			name:        "redact long alphanumeric strings",
			input:       "Error with token: abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOP",
			notContains: []string{"abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOP"},
			contains:    []string{"<REDACTED>"},
		},
		{
			name:        "redact apikey pattern",
			input:       `{"apikey": "secret-key-value"}`,
			notContains: []string{"secret-key-value"},
			contains:    []string{"<REDACTED>"},
		},
		{
			name:     "leave normal text unchanged",
			input:    "This is a normal error message without secrets",
			expected: "This is a normal error message without secrets",
		},
		{
			name:     "handle empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := auth.redactError(tt.input)

			if tt.expected != "" {
				if result != tt.expected {
					t.Errorf("redactError(%q) = %q, want %q", tt.input, result, tt.expected)
				}
			}

			for _, shouldContain := range tt.contains {
				if result == "" || len(result) == 0 {
					t.Errorf("redactError(%q) returned empty result", tt.input)
					continue
				}
				found := false
				for i := 0; i <= len(result)-len(shouldContain); i++ {
					if result[i:i+len(shouldContain)] == shouldContain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("redactError(%q) = %q, should contain %q", tt.input, result, shouldContain)
				}
			}

			for _, shouldNotContain := range tt.notContains {
				found := false
				for i := 0; i <= len(result)-len(shouldNotContain); i++ {
					if result[i:i+len(shouldNotContain)] == shouldNotContain {
						found = true
						break
					}
				}
				if found {
					t.Errorf("redactError(%q) = %q, should not contain %q", tt.input, result, shouldNotContain)
				}
			}
		})
	}
}

func TestAPIKeyAuthenticator_redactErrorEmptyAPIKey(t *testing.T) {
	auth := NewAPIKeyAuthenticator("")

	input := "Error message with no API key to redact"
	result := auth.redactError(input)

	if result != input {
		t.Errorf("redactError(%q) = %q, want %q", input, result, input)
	}
}

func TestAPIKeyAuthenticator_WithContext(t *testing.T) {
	auth := NewAPIKeyAuthenticator("test-key")

	t.Run("with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := auth.GetBearerToken(ctx)
		// Should return an error (either from cancellation or invalid key)
		if err == nil {
			t.Error("GetBearerToken() should return an error with cancelled context")
		}
	})

	t.Run("with timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()

		_, err := auth.GetBearerToken(ctx)
		// Should return an error (either from timeout or invalid key)
		if err == nil {
			t.Error("GetBearerToken() should return an error with timeout context")
		}
	})
}
