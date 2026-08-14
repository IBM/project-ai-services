package authenticator

import (
	"context"
	"os"
	"testing"
)

func TestEnvAuthenticator(t *testing.T) {
	// Save and restore environment
	originalValue := os.Getenv("TEST_API_KEY")
	defer func() {
		if originalValue != "" {
			os.Setenv("TEST_API_KEY", originalValue)
		} else {
			os.Unsetenv("TEST_API_KEY")
		}
	}()

	t.Run("NewEnvAuthenticator with set variable", func(t *testing.T) {
		testKey := "test-api-key-12345"
		os.Setenv("TEST_API_KEY", testKey)

		auth, err := NewEnvAuthenticator("TEST_API_KEY")
		if err != nil {
			t.Fatalf("NewEnvAuthenticator() error = %v", err)
		}

		if auth == nil {
			t.Fatal("NewEnvAuthenticator() returned nil")
		}

		if auth.varName != "TEST_API_KEY" {
			t.Errorf("varName = %q, want %q", auth.varName, "TEST_API_KEY")
		}

		if auth.apiKeyAuth == nil {
			t.Error("apiKeyAuth is nil")
		}

		if auth.apiKeyAuth.apiKey != testKey {
			t.Errorf("apiKeyAuth.apiKey = %q, want %q", auth.apiKeyAuth.apiKey, testKey)
		}
	})

	t.Run("NewEnvAuthenticator with unset variable", func(t *testing.T) {
		os.Unsetenv("TEST_UNSET_VAR")

		auth, err := NewEnvAuthenticator("TEST_UNSET_VAR")
		if err == nil {
			t.Error("NewEnvAuthenticator() should return error for unset variable")
		}

		if auth != nil {
			t.Error("NewEnvAuthenticator() should return nil for unset variable")
		}

		expectedError := "Environment variable TEST_UNSET_VAR is not set or empty"
		if err != nil && err.Error() != expectedError {
			t.Errorf("Error = %q, want %q", err.Error(), expectedError)
		}
	})

	t.Run("NewEnvAuthenticator with empty variable", func(t *testing.T) {
		os.Setenv("TEST_EMPTY_VAR", "")

		auth, err := NewEnvAuthenticator("TEST_EMPTY_VAR")
		if err == nil {
			t.Error("NewEnvAuthenticator() should return error for empty variable")
		}

		if auth != nil {
			t.Error("NewEnvAuthenticator() should return nil for empty variable")
		}
	})

	t.Run("GetBearerToken with valid env", func(t *testing.T) {
		os.Setenv("TEST_API_KEY", "test-key")

		auth, err := NewEnvAuthenticator("TEST_API_KEY")
		if err != nil {
			t.Fatalf("NewEnvAuthenticator() error = %v", err)
		}

		// Note: This will actually try to authenticate with IBM Cloud
		// In a real test environment, we would mock the HTTP client
		// For now, we just verify it doesn't panic
		ctx := context.Background()
		_, _ = auth.GetBearerToken(ctx)
	})

	t.Run("GetBearerToken when env var is removed", func(t *testing.T) {
		os.Setenv("TEST_API_KEY", "initial-key")

		auth, err := NewEnvAuthenticator("TEST_API_KEY")
		if err != nil {
			t.Fatalf("NewEnvAuthenticator() error = %v", err)
		}

		// Remove the environment variable
		os.Unsetenv("TEST_API_KEY")

		ctx := context.Background()
		_, err = auth.GetBearerToken(ctx)
		if err == nil {
			t.Error("GetBearerToken() should return error when env var is removed")
		}
	})

	t.Run("GetBearerToken when env var changes", func(t *testing.T) {
		os.Setenv("TEST_API_KEY", "initial-key")

		auth, err := NewEnvAuthenticator("TEST_API_KEY")
		if err != nil {
			t.Fatalf("NewEnvAuthenticator() error = %v", err)
		}

		// Change the environment variable
		os.Setenv("TEST_API_KEY", "new-key")

		ctx := context.Background()
		_, _ = auth.GetBearerToken(ctx)

		// Verify the API key was updated
		if auth.apiKeyAuth.apiKey != "new-key" {
			t.Errorf("API key not updated: got %q, want %q", auth.apiKeyAuth.apiKey, "new-key")
		}
	})

	t.Run("IsPassthrough returns false", func(t *testing.T) {
		os.Setenv("TEST_API_KEY", "test-key")

		auth, err := NewEnvAuthenticator("TEST_API_KEY")
		if err != nil {
			t.Fatalf("NewEnvAuthenticator() error = %v", err)
		}

		if auth.IsPassthrough() {
			t.Error("IsPassthrough() = true, want false")
		}
	})

	t.Run("GetType returns correct type", func(t *testing.T) {
		os.Setenv("TEST_API_KEY", "test-key")

		auth, err := NewEnvAuthenticator("TEST_API_KEY")
		if err != nil {
			t.Fatalf("NewEnvAuthenticator() error = %v", err)
		}

		expected := string(AuthTypeEnv)
		if auth.GetType() != expected {
			t.Errorf("GetType() = %q, want %q", auth.GetType(), expected)
		}
	})
}

func TestEnvAuthenticatorInterface(t *testing.T) {
	os.Setenv("TEST_API_KEY", "test-key")
	defer os.Unsetenv("TEST_API_KEY")

	auth, err := NewEnvAuthenticator("TEST_API_KEY")
	if err != nil {
		t.Fatalf("NewEnvAuthenticator() error = %v", err)
	}

	// Verify it implements the Authenticator interface
	var _ Authenticator = auth
}
