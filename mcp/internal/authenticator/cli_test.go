package authenticator

import (
	"context"
	"strings"
	"testing"
)

func TestNewCLIAuthenticator(t *testing.T) {
	auth := NewCLIAuthenticator()

	if auth == nil {
		t.Fatal("NewCLIAuthenticator() returned nil")
	}

	expectedCommand := "ibmcloud iam oauth-tokens -o json"
	if auth.command != expectedCommand {
		t.Errorf("command = %q, want %q", auth.command, expectedCommand)
	}

	// Verify it implements the Authenticator interface
	var _ Authenticator = auth
}

func TestCLIAuthenticator_GetBearerToken(t *testing.T) {
	auth := NewCLIAuthenticator()
	ctx := context.Background()

	// Note: This test will try to run the actual ibmcloud CLI command
	// In most test environments, this will fail (which is expected)
	_, err := auth.GetBearerToken(ctx)

	// We expect this to fail in test environments
	if err == nil {
		t.Log("GetBearerToken() succeeded - you may have ibmcloud CLI installed and logged in")
	} else {
		// Verify it's an authentication error
		if err.Error() == "" {
			t.Error("Error message should not be empty")
		}

		// The error should mention the CLI command
		if !strings.Contains(err.Error(), "ibmcloud") {
			t.Errorf("Error should mention ibmcloud CLI: %v", err)
		}
	}
}

func TestCLIAuthenticator_GetBearerTokenWithCancelledContext(t *testing.T) {
	auth := NewCLIAuthenticator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := auth.GetBearerToken(ctx)

	// Should return an error due to cancelled context
	if err == nil {
		t.Error("GetBearerToken() should return error with cancelled context")
	}
}

func TestCLIAuthenticator_IsPassthrough(t *testing.T) {
	auth := NewCLIAuthenticator()

	if auth.IsPassthrough() {
		t.Error("IsPassthrough() = true, want false")
	}
}

func TestCLIAuthenticator_GetType(t *testing.T) {
	auth := NewCLIAuthenticator()

	expected := string(AuthTypeCLI)
	if auth.GetType() != expected {
		t.Errorf("GetType() = %q, want %q", auth.GetType(), expected)
	}
}

func TestCLIAuthenticator_redactOutput(t *testing.T) {
	auth := NewCLIAuthenticator()

	tests := []struct {
		name        string
		input       string
		contains    []string
		notContains []string
	}{
		{
			name:        "redact Bearer token",
			input:       "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.signature",
			contains:    []string{"<REDACTED>"},
			notContains: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:        "redact IAM token in JSON",
			input:       `{"iam_token": "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test"}`,
			contains:    []string{"<REDACTED>"},
			notContains: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:        "redact long alphanumeric strings",
			input:       "Error with token: abcdefghijklmnopqrstuvwxyz1234567890ABCDEF",
			contains:    []string{"<REDACTED>"},
			notContains: []string{"abcdefghijklmnopqrstuvwxyz1234567890ABCDEF"},
		},
		{
			name:  "leave normal text unchanged",
			input: "This is a normal CLI output without secrets",
		},
		{
			name:  "handle empty string",
			input: "",
		},
		{
			name:        "complex CLI output",
			input:       `{"iam_token": "Bearer eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsImtpZCI6IjExIn0.test", "refresh_token": "some-refresh-token"}`,
			contains:    []string{"<REDACTED>"},
			notContains: []string{"eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsImtpZCI6IjExIn0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := auth.redactOutput(tt.input)

			for _, shouldContain := range tt.contains {
				if !strings.Contains(result, shouldContain) {
					t.Errorf("redactOutput(%q) = %q, should contain %q", tt.input, result, shouldContain)
				}
			}

			for _, shouldNotContain := range tt.notContains {
				if strings.Contains(result, shouldNotContain) {
					t.Errorf("redactOutput(%q) = %q, should not contain %q", tt.input, result, shouldNotContain)
				}
			}
		})
	}
}

func TestCLIAuthenticator_redactOutputSpecialCases(t *testing.T) {
	auth := NewCLIAuthenticator()

	t.Run("multiple tokens in output", func(t *testing.T) {
		input := "Bearer token1 and also Bearer token2"
		result := auth.redactOutput(input)

		if strings.Contains(result, "token1") || strings.Contains(result, "token2") {
			t.Errorf("redactOutput should redact all Bearer tokens: %q", result)
		}

		if !strings.Contains(result, "<REDACTED>") {
			t.Errorf("redactOutput should contain <REDACTED>: %q", result)
		}
	})

	t.Run("overlapping patterns", func(t *testing.T) {
		input := `{"iam_token": "Bearer abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOP"}`
		result := auth.redactOutput(input)

		// Should redact the long token
		if strings.Contains(result, "abcdefghijklmnopqrstuvwxyz1234567890ABCDEFGHIJKLMNOP") {
			t.Errorf("redactOutput should redact long token: %q", result)
		}

		if !strings.Contains(result, "<REDACTED>") {
			t.Errorf("redactOutput should contain <REDACTED>: %q", result)
		}
	})
}

func TestCLIAuthenticator_CommandExecutionFlow(t *testing.T) {
	// This test verifies the command execution logic without actually running CLI
	auth := NewCLIAuthenticator()

	// Test command format
	expectedCommand := "ibmcloud iam oauth-tokens -o json"
	if auth.command != expectedCommand {
		t.Errorf("Command should be %q, got %q", expectedCommand, auth.command)
	}

	// Test context usage by using a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	_, err := auth.GetBearerToken(ctx)
	if err == nil {
		t.Error("Should return error with immediate timeout")
	}
}

func TestCLIAuthenticator_ErrorMessages(t *testing.T) {
	auth := NewCLIAuthenticator()
	ctx := context.Background()

	// This will likely fail in test environment
	_, err := auth.GetBearerToken(ctx)

	if err != nil {
		errorMsg := err.Error()

		// Error message should be informative
		if len(errorMsg) < 10 {
			t.Errorf("Error message too short: %q", errorMsg)
		}

		// Should not contain actual secrets (if any were present)
		// This is a basic check - in real scenario, we'd mock the CLI response
		if strings.Contains(errorMsg, "eyJ") { // JWT-like token
			t.Errorf("Error message may contain unredacted token: %q", errorMsg)
		}
	}
}
