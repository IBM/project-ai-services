package models

import (
	"time"

	"github.com/google/uuid"
)

// ServiceCredential is the catalog's record of a per-deployment credential that one service
// (the caller, for example "chat") trades for short-lived tokens for another service.
// Only the SHA-256 hash of the secret is stored; the secret itself is never persisted.
type ServiceCredential struct {
	ID           uuid.UUID
	Caller       string
	DeploymentID string
	SecretHash   string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	RevokedAt    *time.Time
}
