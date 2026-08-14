package authenticator

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"

	"github.com/project-ai-services/mcp/internal/errors"
)

// CLIAuthenticator authenticates using the IBM Cloud CLI
type CLIAuthenticator struct {
	command string
}

// NewCLIAuthenticator creates a new CLI authenticator
func NewCLIAuthenticator() *CLIAuthenticator {
	return &CLIAuthenticator{
		command: "ibmcloud iam oauth-tokens -o json",
	}
}

// GetBearerToken returns a bearer token from the IBM Cloud CLI
func (a *CLIAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	// Execute the CLI command
	// #nosec G204 - command is hardcoded in NewCLIAuthenticator, not user input
	cmd := exec.CommandContext(ctx, "sh", "-c", a.command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return "", errors.NewAuthenticationError("Failed to run command: %s\n\n%s",
			a.command, a.redactOutput(string(output)))
	}

	// Check if command was successful
	if cmd.ProcessState.ExitCode() != 0 {
		return "", errors.NewAuthenticationError(
			"IBM Cloud CLI unavailable or not logged in or failed unexpectedly.\n\n%s\n\nTo debug, try running: %s",
			a.redactOutput(string(output)), a.command)
	}

	// Parse JSON output
	var envelope struct {
		IAMToken string `json:"iam_token"`
	}

	if err := json.Unmarshal(output, &envelope); err != nil {
		return "", errors.NewAuthenticationError(
			"IBM Cloud CLI output was not valid JSON.\n\nExpected JSON structure like: {\"iam_token\":\"Bearer <REDACTED>\"}\n\nGot: %s\n\n%v",
			a.redactOutput(string(output)), err)
	}

	// Validate the token format
	if envelope.IAMToken == "" || !strings.HasPrefix(envelope.IAMToken, "Bearer ") {
		return "", errors.NewAuthenticationError(
			"Could not extract IAM token from IBM Cloud CLI output.\n\nExpected JSON structure like: {\"iam_token\":\"Bearer <REDACTED>\"}\n\nGot: %s",
			a.redactOutput(string(output)))
	}

	// Remove "Bearer " prefix
	token := strings.TrimPrefix(envelope.IAMToken, "Bearer ")
	return strings.TrimSpace(token), nil
}

// IsPassthrough returns false for CLI authentication
func (a *CLIAuthenticator) IsPassthrough() bool {
	return false
}

// GetType returns the authenticator type
func (a *CLIAuthenticator) GetType() string {
	return string(AuthTypeCLI)
}

// redactOutput redacts sensitive information from CLI output
func (a *CLIAuthenticator) redactOutput(output string) string {
	// Redact common patterns for tokens
	patterns := []string{
		`Bearer\s+[A-Za-z0-9._-]+`,                  // Bearer tokens
		`"iam_token":\s*"Bearer\s+[A-Za-z0-9._-]+"`, // IAM token in JSON
		`[A-Za-z0-9_-]{40,}`,                        // Long alphanumeric strings
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		output = re.ReplaceAllString(output, "<REDACTED>")
	}

	return output
}
