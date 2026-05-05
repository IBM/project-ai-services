package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// TokenBlacklist defines the interface for managing revoked tokens. It allows adding tokens to the blacklist
// with their expiry times and checking if a token is currently blacklisted.
type TokenBlacklist interface {
	Add(token string, tokenType string, exp time.Time)
	Contains(token string, tokenType string) bool
	Stop()
}

// HashToken creates a SHA-256 hash of the token for secure storage.
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// DBTokenBlacklist is a database-backed implementation of TokenBlacklist.
// It stores revoked tokens in PostgreSQL with SHA-256 hashing for security.
// Suitable for multi-instance deployments.
type DBTokenBlacklist struct {
	repo   repository.TokenBlacklistRepository
	stopCh chan struct{}
}

// NewDBTokenBlacklist creates a new database-backed token blacklist and starts the cleanup goroutine.
func NewDBTokenBlacklist(repo repository.TokenBlacklistRepository) *DBTokenBlacklist {
	b := &DBTokenBlacklist{
		repo:   repo,
		stopCh: make(chan struct{}),
	}
	go b.gc()
	return b
}

// Add adds a token to the blacklist with its expiry time.
// The token is hashed using SHA-256 before storage for security.
func (b *DBTokenBlacklist) Add(token string, tokenType string, exp time.Time) {
	ctx := context.Background()
	tokenHash := HashToken(token)

	if err := b.repo.Add(ctx, tokenHash, models.TokenType(tokenType), exp); err != nil {
		logger.Errorf("failed to add token to blacklist: %v", err)
	}
}

// Contains checks if the provided token is in the blacklist and has not yet expired.
func (b *DBTokenBlacklist) Contains(token string, tokenType string) bool {
	ctx := context.Background()
	tokenHash := HashToken(token)

	exists, err := b.repo.Contains(ctx, tokenHash, models.TokenType(tokenType))
	if err != nil {
		logger.Errorf("failed to check token blacklist: %v", err)
		return false
	}
	return exists
}

// Stop signals the cleanup goroutine to stop.
func (b *DBTokenBlacklist) Stop() {
	close(b.stopCh)
}

// gc runs periodically to clean up expired tokens from the database.
func (b *DBTokenBlacklist) gc() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			ctx := context.Background()
			if err := b.repo.CleanupExpired(ctx); err != nil {
				logger.Errorf("failed to cleanup expired tokens: %v", err)
			}
		}
	}
}

// Made with Bob
