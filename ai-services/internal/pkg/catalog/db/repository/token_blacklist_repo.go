// Package repository provides database repository implementations for the catalog service.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// TokenBlacklistRepository defines the interface for token blacklist data operations.
type TokenBlacklistRepository interface {
	// Add adds a token hash to the blacklist with its expiry time.
	Add(ctx context.Context, tokenHash string, tokenType models.TokenType, expiresAt time.Time) error
	// Contains checks if a token hash exists in the blacklist and is not expired.
	Contains(ctx context.Context, tokenHash string, tokenType models.TokenType) (bool, error)
	// CleanupExpired removes all expired tokens from the blacklist.
	CleanupExpired(ctx context.Context) error
}

// tokenBlacklistRepo implements TokenBlacklistRepository using pgx.
type tokenBlacklistRepo struct {
	pool *pgxpool.Pool
}

// NewTokenBlacklistRepository creates a new TokenBlacklistRepository instance.
func NewTokenBlacklistRepository(pool *pgxpool.Pool) TokenBlacklistRepository {
	return &tokenBlacklistRepo{pool: pool}
}

// Add adds a token hash to the blacklist with its expiry time.
// Uses ON CONFLICT DO NOTHING to handle duplicate entries gracefully.
func (r *tokenBlacklistRepo) Add(ctx context.Context, tokenHash string, tokenType models.TokenType, expiresAt time.Time) error {
	query := `
		INSERT INTO tokens_blacklist (token_hash, token_type, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (token_hash) DO NOTHING
	`

	_, err := r.pool.Exec(ctx, query, tokenHash, tokenType, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to add token to blacklist: %w", err)
	}

	return nil
}

// Contains checks if a token hash exists in the blacklist and is not expired.
// Returns true if the token is blacklisted and still valid (not expired).
func (r *tokenBlacklistRepo) Contains(ctx context.Context, tokenHash string, tokenType models.TokenType) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 
			FROM tokens_blacklist 
			WHERE token_hash = $1 
			AND token_type = $2 
			AND expires_at > NOW()
		)
	`

	var exists bool
	err := r.pool.QueryRow(ctx, query, tokenHash, tokenType).Scan(&exists)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to check token blacklist: %w", err)
	}

	return exists, nil
}

// CleanupExpired removes all expired tokens from the blacklist.
func (r *tokenBlacklistRepo) CleanupExpired(ctx context.Context) error {
	query := `
		DELETE FROM tokens_blacklist 
		WHERE expires_at < NOW()
	`

	_, err := r.pool.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	return nil
}

// Made with Bob
