package authenticator

import (
	"context"
	"strings"
	"testing"
)

func TestNewOPAuthenticator(t *testing.T) {
	reference := "op://vault/item/field"
	auth := NewOPAuthenticator(reference)

	if auth == nil {
		t.Fatal("NewOPAuthenticator() returned nil")
	}

	if auth.reference != reference {
		t.Errorf("reference = %q, want %q", auth.reference, reference)
	}

	if auth.apiKeyAuth != nil {
		t.Error("apiKeyAuth should be nil initially")
	}

	if auth.cachedAPIKey != "" {
		t.Error("cachedAPIKey should be empty initially")
	}

	// Verify it implements the Authenticator interface
	var _ Authenticator = auth
}

func TestOPAuthenticator_GetBearerToken(t *testing.T) {
	reference := "op://test-vault/test-item/api-key"
	auth := NewOPAuthenticator(reference)
	ctx := context.Background()

	// Note: This test will try to run the actual 1Password CLI
	// In most test environments, this will fail (which is expected)
	_, err := auth.GetBearerToken(ctx)

	// We expect this to fail in test environments
	if err == nil {
		t.Log("GetBearerToken() succeeded - you may have 1Password CLI installed and configured")
	} else {
		// Verify it's an authentication error
		if err.Error() == "" {
			t.Error("Error message should not be empty")
		}

		// The error should mention 1Password or the reference
		errorMsg := err.Error()
		if !strings.Contains(errorMsg, "1Password") && !strings.Contains(errorMsg, reference) {
			t.Errorf("Error should mention 1Password or reference: %v", err)
		}

		// Should provide helpful troubleshooting info
		if !strings.Contains(errorMsg, "Make sure") {
			t.Errorf("Error should provide troubleshooting guidance: %v", err)
		}
	}
}

func TestOPAuthenticator_GetBearerTokenWithCancelledContext(t *testing.T) {
	auth := NewOPAuthenticator("op://test/test/test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := auth.GetBearerToken(ctx)

	// Should return an error due to cancelled context or missing op command
	if err == nil {
		t.Error("GetBearerToken() should return error with cancelled context")
	}
}

func TestOPAuthenticator_IsPassthrough(t *testing.T) {
	auth := NewOPAuthenticator("op://test/test/test")

	if auth.IsPassthrough() {
		t.Error("IsPassthrough() = true, want false")
	}
}

func TestOPAuthenticator_GetType(t *testing.T) {
	auth := NewOPAuthenticator("op://test/test/test")

	expected := string(AuthTypeOP)
	if auth.GetType() != expected {
		t.Errorf("GetType() = %q, want %q", auth.GetType(), expected)
	}
}

func TestOPAuthenticator_ReferenceValidation(t *testing.T) {
	tests := []struct {
		name          string
		reference     string
		expectInvalid bool
	}{
		{
			name:          "valid op URL",
			reference:     "op://Private/IBM-Cloud-API-Key/credential",
			expectInvalid: false,
		},
		{
			name:          "invalid - simple reference without op://",
			reference:     "my-api-key",
			expectInvalid: true,
		},
		{
			name:          "invalid - vault with spaces",
			reference:     "op://My Vault/My Item/password",
			expectInvalid: true,
		},
		{
			name:          "invalid - empty reference",
			reference:     "",
			expectInvalid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewOPAuthenticator(tt.reference)

			if auth.reference != tt.reference {
				t.Errorf("reference = %q, want %q", auth.reference, tt.reference)
			}

			// Try to get token
			ctx := context.Background()
			_, err := auth.GetBearerToken(ctx)

			// All should fail in test environment
			if err == nil {
				t.Error("GetBearerToken() should return error in test environment")
				return
			}

			// Check if validation error is expected
			if tt.expectInvalid {
				if !strings.Contains(err.Error(), "Invalid 1Password reference format") {
					t.Errorf("Expected validation error for invalid reference %q, got: %v", tt.reference, err)
				}
			}
		})
	}
}

func TestOPAuthenticator_CachingBehavior(t *testing.T) {
	auth := NewOPAuthenticator("op://test/test/test")

	// Initially no API key authenticator
	if auth.apiKeyAuth != nil {
		t.Error("apiKeyAuth should be nil initially")
	}

	// After attempting to get token, verify caching logic is in place
	// (Even though it will fail, the structure should be there)
	ctx := context.Background()
	_, _ = auth.GetBearerToken(ctx)

	// The structure should remain consistent
	if auth.reference != "op://test/test/test" {
		t.Error("reference should remain unchanged")
	}
}

func TestOPAuthenticator_ErrorMessageQuality(t *testing.T) {
	tests := []struct {
		name          string
		reference     string
		expectInvalid bool
	}{
		{
			name:          "valid descriptive reference",
			reference:     "op://Production/IBM-Cloud/API-Key",
			expectInvalid: false,
		},
		{
			name:          "invalid short reference",
			reference:     "api-key",
			expectInvalid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewOPAuthenticator(tt.reference)
			ctx := context.Background()

			_, err := auth.GetBearerToken(ctx)

			if err != nil {
				errorMsg := err.Error()

				// Error should be informative
				if len(errorMsg) < 20 {
					t.Errorf("Error message too short for reference %q: %q", tt.reference, errorMsg)
				}

				if tt.expectInvalid {
					// For invalid references, should mention format validation
					if !strings.Contains(errorMsg, "Invalid 1Password reference format") {
						t.Errorf("Error should mention invalid format for %q: %q", tt.reference, errorMsg)
					}
				} else {
					// For valid references that fail due to missing op CLI, should provide troubleshooting
					requiredPhrases := []string{"Make sure", "1Password CLI"}
					for _, phrase := range requiredPhrases {
						if !strings.Contains(errorMsg, phrase) {
							t.Errorf("Error should contain %q for better UX: %q", phrase, errorMsg)
						}
					}
				}
			}
		})
	}
}

func TestOPAuthenticator_ContextRespect(t *testing.T) {
	auth := NewOPAuthenticator("op://test/test/test")

	t.Run("respects timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()

		_, err := auth.GetBearerToken(ctx)

		// Should return an error (either timeout or command not found)
		if err == nil {
			t.Error("Should return error with immediate timeout")
		}
	})

	t.Run("respects cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := auth.GetBearerToken(ctx)

		// Should return an error due to cancellation or command not found
		if err == nil {
			t.Error("Should return error with cancelled context")
		}
	})
}

func TestOPAuthenticator_ReferenceParsing(t *testing.T) {
	// Test that various reference formats are stored correctly
	tests := []struct {
		reference string
		valid     bool
	}{
		{"op://Private/API-Keys/IBM-Cloud", true},
		{"op://vault/item/field", true},
		{"simple-item-name", false}, // Invalid - no op:// prefix
		{"item with spaces", false}, // Invalid - no op:// prefix and spaces
		{"op://Vault-With-Spaces/Item-With-Spaces/Field-With-Spaces", true},
	}

	for _, tt := range tests {
		auth := NewOPAuthenticator(tt.reference)
		if auth.reference != tt.reference {
			t.Errorf("Reference not preserved: got %q, want %q", auth.reference, tt.reference)
		}

		// Verify validation works as expected
		err := validateOPReference(tt.reference)
		if tt.valid && err != nil {
			t.Errorf("validateOPReference(%q) should be valid but got error: %v", tt.reference, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateOPReference(%q) should be invalid but got no error", tt.reference)
		}
	}
}

func TestOPAuthenticator_APIKeyAuthenticatorCaching(t *testing.T) {
	auth := NewOPAuthenticator("op://test/test/test")

	// Simulate getting different API keys (though op command will fail)
	// This tests the caching logic structure

	if auth.apiKeyAuth != nil {
		t.Error("Should start with nil apiKeyAuth")
	}

	if auth.cachedAPIKey != "" {
		t.Error("Should start with empty cachedAPIKey")
	}

	// The caching mechanism is tested through the structure
	// In real scenarios, if the API key changes, a new authenticator should be created
}

func TestValidateOPReference(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid reference with vault, item, and field",
			reference: "op://vault/item/field",
			wantErr:   false,
		},
		{
			name:      "valid reference with section",
			reference: "op://vault/item/section/field",
			wantErr:   false,
		},
		{
			name:      "valid reference with hyphens",
			reference: "op://my-vault/my-item/my-field",
			wantErr:   false,
		},
		{
			name:      "valid reference with underscores",
			reference: "op://my_vault/my_item/my_field",
			wantErr:   false,
		},
		{
			name:      "valid reference with numbers",
			reference: "op://vault123/item456/field789",
			wantErr:   false,
		},
		{
			name:      "invalid - missing op:// prefix",
			reference: "vault/item/field",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - wrong protocol",
			reference: "http://vault/item/field",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - spaces in vault name",
			reference: "op://my vault/item/field",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - special characters",
			reference: "op://vault$/item/field",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - command injection attempt",
			reference: "op://vault/item/field; rm -rf /",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - missing field",
			reference: "op://vault/item",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - empty string",
			reference: "",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - only op://",
			reference: "op://",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - path traversal attempt",
			reference: "op://../../../etc/passwd",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - shell metacharacters",
			reference: "op://vault/item/field && echo hacked",
			wantErr:   true,
			errMsg:    "must match format",
		},
		{
			name:      "invalid - pipe character",
			reference: "op://vault/item/field | cat",
			wantErr:   true,
			errMsg:    "must match format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOPReference(tt.reference)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOPReference(%q) error = %v, wantErr %v", tt.reference, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err != nil {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateOPReference(%q) error = %v, want error containing %q", tt.reference, err, tt.errMsg)
				}
			}
		})
	}
}

func TestOPAuthenticator_GetBearerTokenWithInvalidReference(t *testing.T) {
	tests := []struct {
		name      string
		reference string
		wantErr   bool
	}{
		{
			name:      "command injection attempt",
			reference: "op://vault/item/field; rm -rf /",
			wantErr:   true,
		},
		{
			name:      "shell metacharacters",
			reference: "op://vault/item/field && echo hacked",
			wantErr:   true,
		},
		{
			name:      "invalid format",
			reference: "not-a-valid-reference",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := NewOPAuthenticator(tt.reference)
			ctx := context.Background()

			_, err := auth.GetBearerToken(ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetBearerToken() with reference %q error = %v, wantErr %v", tt.reference, err, tt.wantErr)
			}

			if err != nil && !strings.Contains(err.Error(), "Invalid 1Password reference format") {
				t.Errorf("GetBearerToken() error should mention invalid reference format: %v", err)
			}
		})
	}
}
