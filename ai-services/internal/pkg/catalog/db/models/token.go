// Package models defines the data models for the catalog database.
package models

import (
	"time"
)

// TokenType represents the type of token (access or refresh).
type TokenType = string

// TokenBlacklist represents a blacklisted token in the database.
// Tokens are stored as SHA-256 hashes for security.
type TokenBlacklist struct {
	ID        int       `json:"id"`
	TokenHash string    `json:"token_hash"` // SHA-256 hash of the JWT token (64-character hex string)
	TokenType TokenType `json:"token_type"` // Token type: "access" or "refresh"
	ExpiresAt time.Time `json:"expires_at"` // Token expiry timestamp
}

// Made with Bob
