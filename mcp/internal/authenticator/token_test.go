package authenticator

import (
	"context"
	"testing"
)

func TestNewTokenAuthenticator(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain token",
			input:    "my-access-token",
			expected: "my-access-token",
		},
		{
			name:     "token with Bearer prefix",
			input:    "Bearer my-access-token",
			expected: "my-access-token",
		},
		{
			name:     "token with Bearer prefix and extra spaces",
			input:    "Bearer   my-access-token   ",
			expected: "my-access-token",
		},
		{
			name:     "token with only spaces",
			input:    "   my-access-token   ",
			expected: "my-access-token",
		},
		{
			name:     "empty token",
			input:    "",
			expected: "",
		},
		{
			name:     "Bearer only",
			input:    "Bearer",
			expected: "",
		},
		{
			name:     "Bearer with spaces only",
			input:    "Bearer   ",
			expected: "",
		},
		{
			name:     "case sensitive Bearer",
			input:    "bearer my-token",
			expected: "bearer my-token",
		},
		{
			name:     "JWT-like token",
			input:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.test",
			expected: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.test",
		},
		{
			name:     "JWT-like token with Bearer",
			input:    "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.test",
			expected: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewTokenAuthenticator(tt.input)

			if auth == nil {
				t.Fatal("NewTokenAuthenticator() returned nil")
			}

			if auth.token != tt.expected {
				t.Errorf("token = %q, want %q", auth.token, tt.expected)
			}

			// Verify it implements the Authenticator interface
			var _ Authenticator = auth
		})
	}
}

func TestTokenAuthenticator_GetBearerToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "valid token",
			token: "my-access-token-12345",
		},
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "JWT token",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewTokenAuthenticator(tt.token)
			ctx := context.Background()

			token, err := auth.GetBearerToken(ctx)

			if err != nil {
				t.Errorf("GetBearerToken() error = %v, want nil", err)
			}

			if token != tt.token {
				t.Errorf("GetBearerToken() = %q, want %q", token, tt.token)
			}
		})
	}
}

func TestTokenAuthenticator_GetBearerTokenWithContext(t *testing.T) {
	auth := NewTokenAuthenticator("test-token")

	t.Run("with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		token, err := auth.GetBearerToken(ctx)

		// Token authenticator doesn't use context, so it should still return the token
		if err != nil {
			t.Errorf("GetBearerToken() error = %v, want nil", err)
		}

		if token != "test-token" {
			t.Errorf("GetBearerToken() = %q, want %q", token, "test-token")
		}
	})

	t.Run("with timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()

		token, err := auth.GetBearerToken(ctx)

		// Token authenticator doesn't use context, so it should still return the token
		if err != nil {
			t.Errorf("GetBearerToken() error = %v, want nil", err)
		}

		if token != "test-token" {
			t.Errorf("GetBearerToken() = %q, want %q", token, "test-token")
		}
	})
}

func TestTokenAuthenticator_IsPassthrough(t *testing.T) {
	auth := NewTokenAuthenticator("test-token")

	if auth.IsPassthrough() {
		t.Error("IsPassthrough() = true, want false")
	}
}

func TestTokenAuthenticator_GetType(t *testing.T) {
	auth := NewTokenAuthenticator("test-token")

	expected := string(AuthTypeToken)
	if auth.GetType() != expected {
		t.Errorf("GetType() = %q, want %q", auth.GetType(), expected)
	}
}

func TestTokenAuthenticator_MultipleCalls(t *testing.T) {
	token := "consistent-token-12345"
	auth := NewTokenAuthenticator(token)
	ctx := context.Background()

	// Call GetBearerToken multiple times to ensure consistency
	for i := 0; i < 3; i++ {
		result, err := auth.GetBearerToken(ctx)

		if err != nil {
			t.Errorf("Call %d: GetBearerToken() error = %v, want nil", i+1, err)
		}

		if result != token {
			t.Errorf("Call %d: GetBearerToken() = %q, want %q", i+1, result, token)
		}
	}
}

func TestTokenAuthenticator_BearerPrefixHandling(t *testing.T) {
	tests := []struct {
		name                string
		inputToken          string
		expectedStoredToken string
	}{
		{
			name:                "Bearer prefix removed",
			inputToken:          "Bearer abc123",
			expectedStoredToken: "abc123",
		},
		{
			name:                "Multiple Bearer prefixes",
			inputToken:          "Bearer Bearer token",
			expectedStoredToken: "Bearer token", // Only first "Bearer " is removed
		},
		{
			name:                "Bearer in middle",
			inputToken:          "abc Bearer def",
			expectedStoredToken: "abc Bearer def", // Only prefix "Bearer " is removed
		},
		{
			name:                "Bearer case sensitive",
			inputToken:          "bearer token",
			expectedStoredToken: "bearer token", // Case sensitive, no removal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewTokenAuthenticator(tt.inputToken)

			if auth.token != tt.expectedStoredToken {
				t.Errorf("Stored token = %q, want %q", auth.token, tt.expectedStoredToken)
			}

			ctx := context.Background()
			token, err := auth.GetBearerToken(ctx)

			if err != nil {
				t.Errorf("GetBearerToken() error = %v, want nil", err)
			}

			if token != tt.expectedStoredToken {
				t.Errorf("GetBearerToken() = %q, want %q", token, tt.expectedStoredToken)
			}
		})
	}
}
