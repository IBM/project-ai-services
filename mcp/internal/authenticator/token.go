package authenticator

import (
	"context"
	"strings"
)

// TokenAuthenticator authenticates using a pre-provided token
type TokenAuthenticator struct {
	token string
}

// NewTokenAuthenticator creates a new token authenticator
func NewTokenAuthenticator(token string) *TokenAuthenticator {
	// Remove "Bearer " prefix if present
	token = strings.TrimPrefix(token, "Bearer")
	token = strings.TrimSpace(token)

	return &TokenAuthenticator{
		token: token,
	}
}

// GetBearerToken returns the pre-configured token
func (a *TokenAuthenticator) GetBearerToken(ctx context.Context) (string, error) {
	return a.token, nil
}

// IsPassthrough returns false for token authentication
func (a *TokenAuthenticator) IsPassthrough() bool {
	return false
}

// GetType returns the authenticator type
func (a *TokenAuthenticator) GetType() string {
	return string(AuthTypeToken)
}
