package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// ComponentRepository defines the interface for component data operations.
type ComponentRepository interface {
	// Insert creates a new component in the database.
	Insert(ctx context.Context, component *models.Component) error
	// GetByID retrieves a component by ID.
	GetByID(ctx context.Context, id uuid.UUID) (*models.Component, error)
	// GetAll retrieves all components from the database.
	GetAll(ctx context.Context) ([]models.Component, error)
	// GetByType retrieves all components of a specific type.
	GetByType(ctx context.Context, componentType string) ([]models.Component, error)
	// Update updates a component in the database.
	Update(ctx context.Context, component *models.Component) error
	// Delete removes a component from the database.
	Delete(ctx context.Context, id uuid.UUID) error
}

// componentRepo implements ComponentRepository using pgx.
type componentRepo struct {
	pool *pgxpool.Pool
}

// NewComponentRepository creates a new ComponentRepository instance.
func NewComponentRepository(pool *pgxpool.Pool) ComponentRepository {
	return &componentRepo{pool: pool}
}

// Insert creates a new component in the database.
func (r *componentRepo) Insert(ctx context.Context, component *models.Component) error {
	query := `
		INSERT INTO components (id, type, provider, endpoints, version, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`

	// Generate UUID if not provided
	if component.ID == uuid.Nil {
		component.ID = uuid.New()
	}

	// Marshal endpoints to JSONB
	var endpointsJSON []byte
	var err error
	if component.Endpoints != nil {
		endpointsJSON, err = json.Marshal(component.Endpoints)
		if err != nil {
			return fmt.Errorf("failed to marshal endpoints: %w", err)
		}
	}

	// Marshal metadata to JSONB
	var metadataJSON []byte
	if component.Metadata != nil {
		metadataJSON, err = json.Marshal(component.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	err = r.pool.QueryRow(
		ctx,
		query,
		component.ID,
		component.Type,
		component.Provider,
		endpointsJSON,
		sql.NullString{String: component.Version, Valid: component.Version != ""},
		metadataJSON,
	).Scan(&component.CreatedAt, &component.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert component: %w", err)
	}

	return nil
}

// GetByID retrieves a component by ID.
func (r *componentRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Component, error) {
	query := `
		SELECT id, type, provider, endpoints, version, metadata, created_at, updated_at
		FROM components
		WHERE id = $1
	`

	var (
		component     models.Component
		endpointsJSON []byte
		metadataJSON  []byte
		version       sql.NullString
	)

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&component.ID,
		&component.Type,
		&component.Provider,
		&endpointsJSON,
		&version,
		&metadataJSON,
		&component.CreatedAt,
		&component.UpdatedAt,
	)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, pgx.ErrNoRows
		}

		return nil, fmt.Errorf("failed to get component: %w", err)
	}

	if version.Valid {
		component.Version = version.String
	}

	if len(endpointsJSON) > 0 {
		var endpoints map[string]any
		if err := json.Unmarshal(endpointsJSON, &endpoints); err != nil {
			return nil, fmt.Errorf("failed to unmarshal endpoints: %w", err)
		}
		component.Endpoints = endpoints
	}

	if len(metadataJSON) > 0 {
		var metadata map[string]any
		if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
		component.Metadata = metadata
	}

	return &component, nil
}

// GetAll retrieves all components from the database.
func (r *componentRepo) GetAll(ctx context.Context) ([]models.Component, error) {
	query := `
		SELECT id, type, provider, endpoints, version, metadata, created_at, updated_at
		FROM components
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query components: %w", err)
	}
	defer rows.Close()

	var components []models.Component
	for rows.Next() {
		var (
			component     models.Component
			endpointsJSON []byte
			metadataJSON  []byte
			version       sql.NullString
		)

		err := rows.Scan(&component.ID, &component.Type, &component.Provider, &endpointsJSON,
			&version, &metadataJSON, &component.CreatedAt, &component.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan component: %w", err)
		}

		if version.Valid {
			component.Version = version.String
		}

		if len(endpointsJSON) > 0 {
			var endpoints map[string]any
			if err := json.Unmarshal(endpointsJSON, &endpoints); err != nil {
				return nil, fmt.Errorf("failed to unmarshal endpoints: %w", err)
			}
			component.Endpoints = endpoints
		}

		if len(metadataJSON) > 0 {
			var metadata map[string]any
			if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
			component.Metadata = metadata
		}

		components = append(components, component)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating components: %w", err)
	}

	return components, nil
}

// GetByType retrieves all components of a specific type.
func (r *componentRepo) GetByType(ctx context.Context, componentType string) ([]models.Component, error) {
	query := `
		SELECT id, type, provider, endpoints, version, metadata, created_at, updated_at
		FROM components
		WHERE type = $1
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query, componentType)
	if err != nil {
		return nil, fmt.Errorf("failed to query components by type: %w", err)
	}
	defer rows.Close()

	var components []models.Component
	for rows.Next() {
		var (
			component     models.Component
			endpointsJSON []byte
			metadataJSON  []byte
			version       sql.NullString
		)

		err := rows.Scan(&component.ID, &component.Type, &component.Provider, &endpointsJSON,
			&version, &metadataJSON, &component.CreatedAt, &component.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan component: %w", err)
		}

		if version.Valid {
			component.Version = version.String
		}

		if len(endpointsJSON) > 0 {
			var endpoints map[string]any
			if err := json.Unmarshal(endpointsJSON, &endpoints); err != nil {
				return nil, fmt.Errorf("failed to unmarshal endpoints: %w", err)
			}
			component.Endpoints = endpoints
		}

		if len(metadataJSON) > 0 {
			var metadata map[string]any
			if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
				return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
			}
			component.Metadata = metadata
		}

		components = append(components, component)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating components: %w", err)
	}

	return components, nil
}

// Update updates a component in the database.
func (r *componentRepo) Update(ctx context.Context, component *models.Component) error {
	query := `
		UPDATE components
		SET type = $1, provider = $2, endpoints = $3, version = $4, metadata = $5, updated_at = NOW()
		WHERE id = $6
		RETURNING updated_at
	`

	// Marshal endpoints to JSONB
	var endpointsJSON []byte
	var err error
	if component.Endpoints != nil {
		endpointsJSON, err = json.Marshal(component.Endpoints)
		if err != nil {
			return fmt.Errorf("failed to marshal endpoints: %w", err)
		}
	}

	// Marshal metadata to JSONB
	var metadataJSON []byte
	if component.Metadata != nil {
		metadataJSON, err = json.Marshal(component.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
	}

	err = r.pool.QueryRow(
		ctx,
		query,
		component.Type,
		component.Provider,
		endpointsJSON,
		sql.NullString{String: component.Version, Valid: component.Version != ""},
		metadataJSON,
		component.ID,
	).Scan(&component.UpdatedAt)

	if err != nil {
		if err == pgx.ErrNoRows {
			return pgx.ErrNoRows
		}

		return fmt.Errorf("failed to update component: %w", err)
	}

	return nil
}

// Delete removes a component from the database.
func (r *componentRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM components WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete component: %w", err)
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

// Made with Bob
