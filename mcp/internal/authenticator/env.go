package authenticator

import (
	"context"
	"os"

	"github.com/project-ai-services/mcp/internal/errors"
)

// EnvAuthenticator authenticates using an API key from environment variable
type EnvAuthenticator struct {
	varName    string
	apiKeyAuth *APIKeyAuthenticator
}

// NewEnvAuthenticator creates a new environment variable authenticator
func NewEnvAuthenticator(varName string) (*EnvAuthenticator, error) {
	apiKey := os.Getenv(varName)
	if apiKey == "" {
		return nil, errors.NewAuthenticationError("Environment variable %s is not set or empty", varName)
	}

	return &EnvAuthenticator{
		varName:    varName,
		apiKeyAuth: NewAPIKeyAuthenticator(apiKey),
	}, nil
}

// GetBearerToken returns a bearer token using the API key from environment
func (a *EnvAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	// Re-check the environment variable in case it changed
	apiKey := os.Getenv(a.varName)
	if apiKey == "" {
		return "", errors.NewAuthenticationError("Environment variable %s is not set or empty", a.varName)
	}

	// Update the API key if it changed
	if a.apiKeyAuth.apiKey != apiKey {
		a.apiKeyAuth = NewAPIKeyAuthenticator(apiKey)
	}

	return a.apiKeyAuth.GetBearerToken(ctx)
}

// IsPassthrough returns false for environment authentication
func (a *EnvAuthenticator) IsPassthrough() bool {
	return false
}

// GetType returns the authenticator type
func (a *EnvAuthenticator) GetType() string {
	return string(AuthTypeEnv)
}
