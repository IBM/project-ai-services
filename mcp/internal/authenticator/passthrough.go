package authenticator

import (
	"context"

	"github.com/project-ai-services/ai-services-mcp/internal/errors"
)

// PassthroughAuthenticator uses authorization provided by the client
type PassthroughAuthenticator struct{}

// NewPassthroughAuthenticator creates a new passthrough authenticator
func NewPassthroughAuthenticator() *PassthroughAuthenticator {
	return &PassthroughAuthenticator{}
}

// GetBearerToken returns an error since passthrough auth requires client-provided tokens
func (a *PassthroughAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	return "", errors.NewAuthenticationError(
		"Passthrough authentication requires the client to provide the Authorization header")
}

// IsPassthrough returns true for passthrough authentication
func (a *PassthroughAuthenticator) IsPassthrough() bool {
	return true
}

// GetType returns the authenticator type
func (a *PassthroughAuthenticator) GetType() string {
	return string(AuthTypePassthrough)
}
