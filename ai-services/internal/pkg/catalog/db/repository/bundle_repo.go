package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// ErrNotFound is returned by Update when no row matches the given id.
var ErrNotFound = errors.New("catalog bundle not found")

// BundleFilters defines optional pagination inputs for querying catalog bundles.
type BundleFilters struct {
	Limit  int // Number of records to return (for pagination).
	Offset int // Number of records to skip (for pagination).
}

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

	// Update applies only the non-nil fields in upd to the row identified by id.
	// Returns an error if no fields are set.
	Update(ctx context.Context, id uuid.UUID, upd models.BundleUpdate) error

	// Delete permanently removes the row.
	Delete(ctx context.Context, id uuid.UUID) error

	// GetCount returns the total number of bundle rows.
	GetCount(ctx context.Context) (int, error)

	// GetAll returns bundle rows ordered by created_at DESC, applying the pagination in filters.
	GetAll(ctx context.Context, filters *BundleFilters) ([]models.CatalogBundle, error)
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

const selectCols = "id, name, status, size_bytes, catalog_type, catalog_id, version, error, created_by, created_at, updated_at"

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
		if errors.Is(err, pgx.ErrNoRows) {
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to get active catalog bundle: %w", err)
	}

	return b, nil
}

// Update applies only the non-nil fields in upd to the row identified by id.
// Returns an error if upd is empty (no fields set).
func (r *bundleRepo) Update(ctx context.Context, id uuid.UUID, upd models.BundleUpdate) error {
	var setClauses []string
	var args []any
	i := 1

	if upd.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", i))
		args = append(args, *upd.Status)
		i++
	}
	if upd.Version != nil {
		setClauses = append(setClauses, fmt.Sprintf("version = $%d", i))
		args = append(args, *upd.Version)
		i++
	}
	if upd.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", i))
		args = append(args, sql.NullString{String: *upd.Name, Valid: *upd.Name != ""})
		i++
	}
	if upd.SizeBytes != nil {
		setClauses = append(setClauses, fmt.Sprintf("size_bytes = $%d", i))
		args = append(args, *upd.SizeBytes)
		i++
	}
	if upd.Error != nil {
		setClauses = append(setClauses, fmt.Sprintf("error = $%d", i))
		args = append(args, sql.NullString{String: *upd.Error, Valid: *upd.Error != ""})
		i++
	}

	if len(setClauses) == 0 {
		return fmt.Errorf("Update called with no fields to update")
	}

	query := fmt.Sprintf(
		"UPDATE catalog_bundles SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "),
		i,
	)
	args = append(args, id)

	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update catalog bundle: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
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

// GetCount returns the total number of bundle rows.
func (r *bundleRepo) GetCount(ctx context.Context) (int, error) {
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM catalog_bundles`).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to get catalog bundle count: %w", err)
	}

	return count, nil
}

// GetAll returns bundle rows ordered by created_at DESC, applying pagination from filters.
func (r *bundleRepo) GetAll(ctx context.Context, filters *BundleFilters) ([]models.CatalogBundle, error) {
	query := `SELECT ` + selectCols + ` FROM catalog_bundles ORDER BY created_at DESC`
	args := []any{}

	if filters != nil {
		if filters.Limit > 0 {
			args = append(args, filters.Limit)
			query += fmt.Sprintf(" LIMIT $%d", len(args))
		}
		if filters.Offset > 0 {
			args = append(args, filters.Offset)
			query += fmt.Sprintf(" OFFSET $%d", len(args))
		}
	}

	rows, err := r.pool.Query(ctx, query, args...)
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
