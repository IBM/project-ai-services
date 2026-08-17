package authenticator

import (
	"context"
	"regexp"
	"strings"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/project-ai-services/mcp/internal/errors"
)

// APIKeyAuthenticator authenticates using an IBM Cloud API key
type APIKeyAuthenticator struct {
	apiKey string
	auth   *core.IamAuthenticator
}

// NewAPIKeyAuthenticator creates a new API key authenticator
func NewAPIKeyAuthenticator(apiKey string) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{
		apiKey: apiKey,
		auth: &core.IamAuthenticator{
			ApiKey: apiKey,
		},
	}
}

// GetBearerToken returns a bearer token using the API key
func (a *APIKeyAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	token, err := a.auth.RequestToken()
	if err != nil {
		return "", errors.NewAuthenticationError("Could not get token from API key: %v", a.redactError(err.Error()))
	}

	// Extract the access token from the token response
	accessToken := token.AccessToken
	if accessToken == "" {
		return "", errors.NewAuthenticationError("Unexpected problem extracting token")
	}

	return accessToken, nil
}

// IsPassthrough returns false for API key authentication
func (a *APIKeyAuthenticator) IsPassthrough() bool {
	return false
}

// GetType returns the authenticator type
func (a *APIKeyAuthenticator) GetType() string {
	return string(AuthTypeAPIKey)
}

// redactError redacts sensitive information from error messages
func (a *APIKeyAuthenticator) redactError(errorMsg string) string {
	// Redact API key if it appears in the error
	if a.apiKey != "" {
		errorMsg = strings.ReplaceAll(errorMsg, a.apiKey, "<REDACTED>")
	}

	// Redact common patterns for API keys and tokens
	patterns := []string{
		`[A-Za-z0-9_-]{40,}`,                   // Long alphanumeric strings
		`Bearer\s+[A-Za-z0-9._-]+`,             // Bearer tokens
		`apikey["\s]*[:=]["\s]*[A-Za-z0-9_-]+`, // API key patterns
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		errorMsg = re.ReplaceAllString(errorMsg, "<REDACTED>")
	}

	return errorMsg
}
