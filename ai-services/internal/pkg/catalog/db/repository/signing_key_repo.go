package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// signingKeyLockID is the constant key of the transaction-scoped advisory lock that
// serialises creation of the first signing key across catalog replicas.
const signingKeyLockID int64 = 0x41495356_4B455931 // "AISV" "KEY1"

// SigningKeyRepository stores the catalog's service-token signing keys.
type SigningKeyRepository interface {
	// EnsureActive returns the active key. If there is none, it calls newKey and stores
	// the result. Creation is serialised across replicas, so concurrent callers end up
	// with exactly one active key.
	EnsureActive(ctx context.Context, newKey func() (*models.SigningKey, error)) (*models.SigningKey, error)
	// ListPublishable returns every non-retired key plus keys retired less than grace ago.
	ListPublishable(ctx context.Context, grace time.Duration) ([]models.SigningKey, error)
}

type signingKeyRepo struct {
	pool *pgxpool.Pool
}

// NewSigningKeyRepository creates a SigningKeyRepository backed by Postgres.
func NewSigningKeyRepository(pool *pgxpool.Pool) SigningKeyRepository {
	return &signingKeyRepo{pool: pool}
}

const selectActiveKeyQuery = `
	SELECT kid, private_key_enc, public_key_pem, created_at, retired_at
	FROM signing_keys
	WHERE retired_at IS NULL
	ORDER BY created_at DESC
	LIMIT 1
`

func scanSigningKey(row pgx.Row) (*models.SigningKey, error) {
	var k models.SigningKey
	if err := row.Scan(&k.KID, &k.PrivateKeyEnc, &k.PublicKeyPEM, &k.CreatedAt, &k.RetiredAt); err != nil {
		return nil, err
	}

	return &k, nil
}

// EnsureActive implements SigningKeyRepository.
func (r *signingKeyRepo) EnsureActive(ctx context.Context, newKey func() (*models.SigningKey, error)) (*models.SigningKey, error) {
	// Fast path: no lock needed when a key already exists.
	if k, err := scanSigningKey(r.pool.QueryRow(ctx, selectActiveKeyQuery)); err == nil {
		return k, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to read active signing key: %w", err)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin signing key transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, signingKeyLockID); err != nil {
		return nil, fmt.Errorf("failed to take signing key lock: %w", err)
	}

	// Re-check after taking the lock: another replica may have created the key meanwhile.
	k, err := scanSigningKey(tx.QueryRow(ctx, selectActiveKeyQuery))
	if err == nil {
		return k, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed to read active signing key: %w", err)
	}

	created, err := newKey()
	if err != nil {
		return nil, err
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO signing_keys (kid, private_key_enc, public_key_pem) VALUES ($1, $2, $3) RETURNING created_at`,
		created.KID, created.PrivateKeyEnc, created.PublicKeyPEM,
	).Scan(&created.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert signing key: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit signing key: %w", err)
	}

	return created, nil
}

// ListPublishable implements SigningKeyRepository.
func (r *signingKeyRepo) ListPublishable(ctx context.Context, grace time.Duration) ([]models.SigningKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT kid, private_key_enc, public_key_pem, created_at, retired_at
		FROM signing_keys
		WHERE retired_at IS NULL OR retired_at > NOW() - make_interval(secs => $1)
		ORDER BY created_at DESC
	`, grace.Seconds())
	if err != nil {
		return nil, fmt.Errorf("failed to list signing keys: %w", err)
	}
	defer rows.Close()

	var keys []models.SigningKey
	for rows.Next() {
		k, err := scanSigningKey(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan signing key: %w", err)
		}
		keys = append(keys, *k)
	}

	return keys, rows.Err()
}
