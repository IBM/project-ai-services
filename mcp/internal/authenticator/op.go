package authenticator

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/project-ai-services/mcp/internal/errors"
)

// OPAuthenticator authenticates using 1Password CLI
type OPAuthenticator struct {
	reference    string
	apiKeyAuth   *APIKeyAuthenticator
	cachedAPIKey string
}

// NewOPAuthenticator creates a new 1Password authenticator
func NewOPAuthenticator(reference string) *OPAuthenticator {
	return &OPAuthenticator{
		reference: reference,
	}
}

// GetBearerToken returns a bearer token using an API key from 1Password
func (a *OPAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	// Retrieve the API key from 1Password
	apiKey, err := a.getAPIKeyFromOP(ctx)
	if err != nil {
		return "", err
	}

	// Create or update the API key authenticator if needed
	if a.apiKeyAuth == nil || a.cachedAPIKey != apiKey {
		a.apiKeyAuth = NewAPIKeyAuthenticator(apiKey)
		a.cachedAPIKey = apiKey
	}

	return a.apiKeyAuth.GetBearerToken(ctx)
}

// IsPassthrough returns false for 1Password authentication
func (a *OPAuthenticator) IsPassthrough() bool {
	return false
}

// GetType returns the authenticator type
func (a *OPAuthenticator) GetType() string {
	return string(AuthTypeOP)
}

// getAPIKeyFromOP retrieves the API key from 1Password using the op CLI
func (a *OPAuthenticator) getAPIKeyFromOP(ctx context.Context) (string, error) {
	// Validate the reference format to prevent command injection
	// Valid 1Password references: op://vault/item/field or op://vault/item[/section]/field
	if err := validateOPReference(a.reference); err != nil {
		return "", errors.NewAuthenticationError("Invalid 1Password reference format: %v", err)
	}

	// Use 1Password CLI to read the secret
	// #nosec G204 - reference is validated above to prevent injection
	cmd := exec.CommandContext(ctx, "op", "read", a.reference)
	output, err := cmd.Output()

	if err != nil {
		return "", errors.NewAuthenticationError(
			"Failed to retrieve API key from 1Password reference %s: %v\n\nMake sure:\n1. 1Password CLI is installed and authenticated\n2. The reference %s exists and is accessible",
			a.reference, err, a.reference)
	}

	apiKey := strings.TrimSpace(string(output))
	if apiKey == "" {
		return "", errors.NewAuthenticationError(
			"1Password reference %s returned an empty value", a.reference)
	}

	return apiKey, nil
}

// validateOPReference validates that a string is a valid 1Password reference format
func validateOPReference(ref string) error {
	// 1Password references must start with "op://" and contain valid characters
	// Format: op://vault/item[/section]/field
	pattern := `^op://[a-zA-Z0-9_\-]+/[a-zA-Z0-9_\-]+(/[a-zA-Z0-9_\-]+)?/[a-zA-Z0-9_\-]+$`
	matched, err := regexp.MatchString(pattern, ref)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("reference must match format: op://vault/item[/section]/field")
	}
	return nil
}
