package authenticator

import (
	"context"
)

// Authenticator defines the interface for authentication methods
type Authenticator interface {
	// GetBearerToken returns a valid bearer token
	GetBearerToken(ctx context.Context) (string, error)

	// IsPassthrough returns true if this is a passthrough authenticator
	IsPassthrough() bool

	// GetType returns the type of authenticator
	GetType() string
}

// AuthType represents the type of authentication
type AuthType string

const (
	AuthTypeAPIKey      AuthType = "api-key"
	AuthTypeEnv         AuthType = "env"
	AuthTypePassthrough AuthType = "passthrough"
)
