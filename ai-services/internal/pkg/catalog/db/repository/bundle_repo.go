package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// BundleRepository defines the interface for catalog_bundles data operations.
type BundleRepository interface {
	// Insert creates a new bundle row with status 'processing'.
	// The DB generates the UUID via gen_random_uuid(); Insert populates b.ID, b.CreatedAt, b.UpdatedAt.
	Insert(ctx context.Context, b *models.CatalogBundle) error

	// GetByID retrieves a single bundle row by its UUID primary key.
	// Returns (nil, nil) when not found.
	GetByID(ctx context.Context, id uuid.UUID) (*models.CatalogBundle, error)

	// GetActiveByCatalogID returns the single active row for a (catalog_type, catalog_id) pair.
	// Returns (nil, nil) when no active row exists.
	GetActiveByCatalogID(ctx context.Context, catalogType, catalogID string) (*models.CatalogBundle, error)

	// Activate sets status='active', version, name, and size_bytes for an existing row.
	Activate(ctx context.Context, id uuid.UUID, version, name string, sizeBytes int64) error

	// UpdateStatus transitions a row to the given status.
	// errMsg is stored in the error column when status is 'failed'; it is cleared for all other statuses.
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.BundleStatus, errMsg string) error

	// Delete permanently removes the row.
	Delete(ctx context.Context, id uuid.UUID) error

	// ListAll returns all bundle rows ordered by created_at DESC.
	ListAll(ctx context.Context) ([]models.CatalogBundle, error)
}

// bundleRepo implements BundleRepository using pgx.
type bundleRepo struct {
	pool *pgxpool.Pool
}

// NewBundleRepository creates a new BundleRepository backed by the given connection pool.
func NewBundleRepository(pool *pgxpool.Pool) BundleRepository {
	return &bundleRepo{pool: pool}
}

// scanBundle scans a single catalog_bundles row into a CatalogBundle struct.
func scanBundle(scan func(dest ...any) error) (*models.CatalogBundle, error) {
	var (
		b         models.CatalogBundle
		name      sql.NullString
		sizeBytes sql.NullInt64
		errCol    sql.NullString
		createdBy sql.NullString
	)

	err := scan(
		&b.ID,
		&name,
		&b.Status,
		&sizeBytes,
		&b.CatalogType,
		&b.CatalogID,
		&b.Version,
		&errCol,
		&createdBy,
		&b.CreatedAt,
		&b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if name.Valid {
		b.Name = name.String
	}

	if sizeBytes.Valid {
		b.SizeBytes = &sizeBytes.Int64
	}

	if errCol.Valid {
		b.Error = errCol.String
	}

	if createdBy.Valid {
		b.CreatedBy = createdBy.String
	}

	return &b, nil
}

const selectCols = `
	id, name, status, size_bytes, catalog_type, catalog_id, version, error, created_by, created_at, updated_at
`

// Insert inserts a new row with status 'processing' and populates b.ID, b.CreatedAt, b.UpdatedAt.
func (r *bundleRepo) Insert(ctx context.Context, b *models.CatalogBundle) error {
	query := `
		INSERT INTO catalog_bundles (name, catalog_type, catalog_id, version, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`

	err := r.pool.QueryRow(ctx, query,
		sql.NullString{String: b.Name, Valid: b.Name != ""},
		b.CatalogType,
		b.CatalogID,
		b.Version,
		sql.NullString{String: b.CreatedBy, Valid: b.CreatedBy != ""},
	).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert catalog bundle: %w", err)
	}

	return nil
}

// GetByID retrieves a single bundle row by UUID. Returns (nil, nil) when not found.
func (r *bundleRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.CatalogBundle, error) {
	query := `SELECT ` + selectCols + ` FROM catalog_bundles WHERE id = $1`

	row := r.pool.QueryRow(ctx, query, id)

	b, err := scanBundle(row.Scan)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to get catalog bundle by id: %w", err)
	}

	return b, nil
}

// GetActiveByCatalogID returns the active row for the given (catalog_type, catalog_id) pair.
// Returns (nil, nil) when no active row exists.
func (r *bundleRepo) GetActiveByCatalogID(ctx context.Context, catalogType, catalogID string) (*models.CatalogBundle, error) {
	query := `SELECT ` + selectCols + ` FROM catalog_bundles WHERE catalog_type = $1 AND catalog_id = $2 AND status = 'active'`

	row := r.pool.QueryRow(ctx, query, catalogType, catalogID)

	b, err := scanBundle(row.Scan)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to get active catalog bundle: %w", err)
	}

	return b, nil
}

// Activate sets status='active', version, name, and size_bytes for the given row.
func (r *bundleRepo) Activate(ctx context.Context, id uuid.UUID, version, name string, sizeBytes int64) error {
	query := `
		UPDATE catalog_bundles
		SET status = 'active', version = $1, name = $2, size_bytes = $3, error = NULL
		WHERE id = $4
	`

	_, err := r.pool.Exec(ctx, query,
		version,
		sql.NullString{String: name, Valid: name != ""},
		sizeBytes,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to activate catalog bundle: %w", err)
	}

	return nil
}

// UpdateStatus transitions a row to the given status.
// For 'failed', errMsg is stored in the error column; for all other statuses it is cleared.
func (r *bundleRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status models.BundleStatus, errMsg string) error {
	query := `UPDATE catalog_bundles SET status = $1, error = $2 WHERE id = $3`

	_, err := r.pool.Exec(ctx, query,
		status,
		sql.NullString{String: errMsg, Valid: errMsg != ""},
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to update catalog bundle status: %w", err)
	}

	return nil
}

// Delete permanently removes the row.
func (r *bundleRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM catalog_bundles WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete catalog bundle: %w", err)
	}

	return nil
}

// ListAll returns all bundle rows ordered by created_at DESC.
func (r *bundleRepo) ListAll(ctx context.Context) ([]models.CatalogBundle, error) {
	query := `SELECT ` + selectCols + ` FROM catalog_bundles ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list catalog bundles: %w", err)
	}
	defer rows.Close()

	var bundles []models.CatalogBundle

	for rows.Next() {
		b, err := scanBundle(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("failed to scan catalog bundle: %w", err)
		}

		bundles = append(bundles, *b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating catalog bundles: %w", err)
	}

	return bundles, nil
}

// isNotFound returns true when the error represents a "no rows" result.
func isNotFound(err error) bool {
	return err != nil && err.Error() == "no rows in result set"
}
