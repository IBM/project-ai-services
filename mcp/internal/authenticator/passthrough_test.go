package authenticator

import (
	"context"
	"testing"
)

func TestPassthroughAuthenticator(t *testing.T) {
	auth := NewPassthroughAuthenticator()

	t.Run("GetBearerToken returns error", func(t *testing.T) {
		ctx := context.Background()
		token, err := auth.GetBearerToken(ctx)

		if err == nil {
			t.Error("GetBearerToken() should return an error for passthrough auth")
		}

		if token != "" {
			t.Errorf("GetBearerToken() returned non-empty token: %q", token)
		}

		expectedError := "Passthrough authentication requires the client to provide the Authorization header"
		if err != nil && err.Error() != expectedError {
			t.Errorf("GetBearerToken() error = %q, want %q", err.Error(), expectedError)
		}
	})

	t.Run("IsPassthrough returns true", func(t *testing.T) {
		if !auth.IsPassthrough() {
			t.Error("IsPassthrough() = false, want true")
		}
	})

	t.Run("GetType returns correct type", func(t *testing.T) {
		expected := string(AuthTypePassthrough)
		if auth.GetType() != expected {
			t.Errorf("GetType() = %q, want %q", auth.GetType(), expected)
		}
	})
}

func TestNewPassthroughAuthenticator(t *testing.T) {
	auth := NewPassthroughAuthenticator()

	if auth == nil {
		t.Fatal("NewPassthroughAuthenticator() returned nil")
	}

	// Verify it implements the Authenticator interface
	var _ Authenticator = auth
}

func TestPassthroughAuthenticatorWithContext(t *testing.T) {
	auth := NewPassthroughAuthenticator()

	t.Run("with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := auth.GetBearerToken(ctx)
		if err == nil {
			t.Error("GetBearerToken() should return an error")
		}
	})

	t.Run("with timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()

		_, err := auth.GetBearerToken(ctx)
		if err == nil {
			t.Error("GetBearerToken() should return an error")
		}
	})
}
