package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// AuthCodeRepository stores one-time authorization codes (hashes only).
type AuthCodeRepository interface {
	// Insert stores a new code and removes rows that expired more than retention ago.
	Insert(ctx context.Context, c *models.AuthCode, retention time.Duration) error
	// Consume atomically marks an unused, unexpired code as used and returns it.
	// It returns ErrAuthCodeNotFound for an unknown, used or expired code.
	Consume(ctx context.Context, codeHash string) (*models.AuthCode, error)
	// WasUsed reports whether a code hash exists with used_at set. Used only to log replays.
	WasUsed(ctx context.Context, codeHash string) (bool, error)
}

// ErrAuthCodeNotFound is returned when a code is unknown, already used, or expired.
var ErrAuthCodeNotFound = errors.New("authorization code not found, used or expired")

type authCodeRepo struct {
	pool *pgxpool.Pool
}

// NewAuthCodeRepository creates an AuthCodeRepository backed by Postgres.
func NewAuthCodeRepository(pool *pgxpool.Pool) AuthCodeRepository {
	return &authCodeRepo{pool: pool}
}

// Insert implements AuthCodeRepository.
func (r *authCodeRepo) Insert(ctx context.Context, c *models.AuthCode, retention time.Duration) error {
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM auth_codes WHERE expires_at < NOW() - make_interval(secs => $1)`, retention.Seconds()); err != nil {
		return fmt.Errorf("failed to clean up auth codes: %w", err)
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO auth_codes (code_hash, user_id, idp, service, redirect_uri, code_challenge, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.CodeHash, c.UserID, c.IDP, c.Service, c.RedirectURI, c.CodeChallenge, c.ExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to insert auth code: %w", err)
	}

	return nil
}

// Consume implements AuthCodeRepository. The single UPDATE makes consumption atomic
// across replicas: only one concurrent caller can match used_at IS NULL.
func (r *authCodeRepo) Consume(ctx context.Context, codeHash string) (*models.AuthCode, error) {
	var c models.AuthCode
	err := r.pool.QueryRow(ctx, `
		UPDATE auth_codes SET used_at = NOW()
		WHERE code_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		RETURNING code_hash, user_id, idp, service, redirect_uri, code_challenge, created_at, expires_at, used_at`,
		codeHash,
	).Scan(&c.CodeHash, &c.UserID, &c.IDP, &c.Service, &c.RedirectURI, &c.CodeChallenge, &c.CreatedAt, &c.ExpiresAt, &c.UsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAuthCodeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume auth code: %w", err)
	}

	return &c, nil
}

// WasUsed implements AuthCodeRepository.
func (r *authCodeRepo) WasUsed(ctx context.Context, codeHash string) (bool, error) {
	var used bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM auth_codes WHERE code_hash = $1 AND used_at IS NOT NULL)`, codeHash).Scan(&used)
	if err != nil {
		return false, fmt.Errorf("failed to check auth code: %w", err)
	}

	return used, nil
}

// RedirectTargetRepository reads the registered UI endpoints of deployed services.
type RedirectTargetRepository interface {
	// UIEndpoints returns the URL of every endpoint of type "ui" across all deployed
	// instances of the service template catalogID (for example "digitize").
	UIEndpoints(ctx context.Context, catalogID string) ([]string, error)
}

type redirectTargetRepo struct {
	pool *pgxpool.Pool
}

// NewRedirectTargetRepository creates a RedirectTargetRepository backed by Postgres.
func NewRedirectTargetRepository(pool *pgxpool.Pool) RedirectTargetRepository {
	return &redirectTargetRepo{pool: pool}
}

// UIEndpoints implements RedirectTargetRepository.
func (r *redirectTargetRepo) UIEndpoints(ctx context.Context, catalogID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT endpoints FROM services WHERE catalog_id = $1 AND endpoints IS NOT NULL`, catalogID)
	if err != nil {
		return nil, fmt.Errorf("failed to query service endpoints: %w", err)
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("failed to scan service endpoints: %w", err)
		}
		var eps []map[string]any
		if err := json.Unmarshal(raw, &eps); err != nil {
			return nil, fmt.Errorf("failed to unmarshal service endpoints: %w", err)
		}
		for _, ep := range eps {
			t, _ := ep["type"].(string)
			u, _ := ep["url"].(string)
			if t == "ui" && u != "" {
				urls = append(urls, u)
			}
		}
	}

	return urls, rows.Err()
}
