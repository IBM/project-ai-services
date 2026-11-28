package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// ErrServiceCredentialNotFound is returned when a credential is unknown, revoked or expired.
var ErrServiceCredentialNotFound = errors.New("service credential not found, revoked or expired")

// ServiceCredentialRepository stores service credentials (hashes only).
type ServiceCredentialRepository interface {
	// Insert stores a new credential and removes rows that expired more than retention ago.
	Insert(ctx context.Context, c *models.ServiceCredential, retention time.Duration) error
	// GetValid returns the credential with this ID, or ErrServiceCredentialNotFound when it
	// is unknown, revoked or expired.
	GetValid(ctx context.Context, id uuid.UUID) (*models.ServiceCredential, error)
	// RevokeByDeployment revokes every active credential of the deployment. It returns how
	// many it revoked.
	RevokeByDeployment(ctx context.Context, deploymentID string) (int64, error)
}

type serviceCredentialRepo struct {
	pool *pgxpool.Pool
}

// NewServiceCredentialRepository creates a ServiceCredentialRepository backed by Postgres.
func NewServiceCredentialRepository(pool *pgxpool.Pool) ServiceCredentialRepository {
	return &serviceCredentialRepo{pool: pool}
}

// Insert implements ServiceCredentialRepository.
func (r *serviceCredentialRepo) Insert(ctx context.Context, c *models.ServiceCredential, retention time.Duration) error {
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM service_credentials WHERE expires_at < NOW() - make_interval(secs => $1)`, retention.Seconds()); err != nil {
		return fmt.Errorf("failed to clean up service credentials: %w", err)
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO service_credentials (id, caller, deployment_id, secret_hash, grants, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		c.ID, c.Caller, c.DeploymentID, c.SecretHash, c.Grants, c.ExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to insert service credential: %w", err)
	}

	return nil
}

// GetValid implements ServiceCredentialRepository.
func (r *serviceCredentialRepo) GetValid(ctx context.Context, id uuid.UUID) (*models.ServiceCredential, error) {
	var c models.ServiceCredential
	err := r.pool.QueryRow(ctx, `
		SELECT id, caller, deployment_id, secret_hash, grants, created_at, expires_at, revoked_at
		FROM service_credentials
		WHERE id = $1 AND revoked_at IS NULL AND expires_at > NOW()`, id,
	).Scan(&c.ID, &c.Caller, &c.DeploymentID, &c.SecretHash, &c.Grants, &c.CreatedAt, &c.ExpiresAt, &c.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrServiceCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read service credential: %w", err)
	}

	return &c, nil
}

// RevokeByDeployment implements ServiceCredentialRepository.
func (r *serviceCredentialRepo) RevokeByDeployment(ctx context.Context, deploymentID string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE service_credentials SET revoked_at = NOW()
		WHERE deployment_id = $1 AND revoked_at IS NULL`, deploymentID)
	if err != nil {
		return 0, fmt.Errorf("failed to revoke service credentials of deployment: %w", err)
	}

	return tag.RowsAffected(), nil
}
