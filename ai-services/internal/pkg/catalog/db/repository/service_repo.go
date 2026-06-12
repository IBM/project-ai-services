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

// ServiceRepository defines the interface for service data operations.
type ServiceRepository interface {
	// Insert creates a new service in the database.
	Insert(ctx context.Context, service *models.Service) error
	// Delete removes a service from the database.
	Delete(ctx context.Context, id uuid.UUID) error
	// GetByAppID retrieves all services for a specific application.
	GetByAppID(ctx context.Context, appID uuid.UUID) ([]models.Service, error)
	// Update updates a service in the database.
	Update(ctx context.Context, service *models.Service) error
	// UpdateStatus updates only the status and message of a service.
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.ServiceStatus, message string) error
	// UpdateEndpoints updates only the endpoints of a service.
	UpdateEndpoints(ctx context.Context, id uuid.UUID, endpoints []map[string]any) error
}

// serviceRepo implements ServiceRepository using pgx.
type serviceRepo struct {
	pool *pgxpool.Pool
}

// NewServiceRepository creates a new ServiceRepository instance.
func NewServiceRepository(pool *pgxpool.Pool) ServiceRepository {
	return &serviceRepo{pool: pool}
}

// Insert creates a new service in the database.
func (r *serviceRepo) Insert(ctx context.Context, service *models.Service) error {
	query := `
		INSERT INTO services (id, app_id, catalog_id, status, message, endpoints, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`

	// Generate UUID if not provided
	if service.ID == uuid.Nil {
		service.ID = uuid.New()
	}

	// Marshal endpoints to JSONB
	var endpointsJSON []byte
	var err error
	if service.Endpoints != nil {
		endpointsJSON, err = json.Marshal(service.Endpoints)
		if err != nil {
			return fmt.Errorf("failed to marshal endpoints: %w", err)
		}
	}

	err = r.pool.QueryRow(
		ctx,
		query,
		service.ID,
		service.AppID,
		service.CatalogID,
		service.Status,
		sql.NullString{String: service.Message, Valid: service.Message != ""},
		endpointsJSON,
		sql.NullString{String: service.Version, Valid: service.Version != ""},
	).Scan(&service.CreatedAt, &service.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert service: %w", err)
	}

	return nil
}

// Delete removes a service from the database.
func (r *serviceRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM services WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	return nil
}

// scanService scans a service row and unmarshals JSON fields.
func scanService(rows pgx.Rows) (*models.Service, error) {
	var (
		service        models.Service
		endpointsJSON  []byte
		serviceVersion sql.NullString
		message        sql.NullString
	)

	err := rows.Scan(
		&service.ID,
		&service.AppID,
		&service.CatalogID,
		&service.Status,
		&message,
		&endpointsJSON,
		&serviceVersion,
		&service.CreatedAt,
		&service.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan service: %w", err)
	}

	if serviceVersion.Valid {
		service.Version = serviceVersion.String
	}

	if message.Valid {
		service.Message = message.String
	}

	if len(endpointsJSON) > 0 {
		var endpoints []map[string]any
		if err := json.Unmarshal(endpointsJSON, &endpoints); err != nil {
			return nil, fmt.Errorf("failed to unmarshal service endpoints: %w", err)
		}
		service.Endpoints = endpoints
	}

	return &service, nil
}

// GetByAppID retrieves all services for a specific application.
func (r *serviceRepo) GetByAppID(ctx context.Context, appID uuid.UUID) ([]models.Service, error) {
	query := `
		SELECT id, app_id, catalog_id, status, message, endpoints, version, created_at, updated_at
		FROM services
		WHERE app_id = $1
		ORDER BY created_at
	`

	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, fmt.Errorf("failed to query services: %w", err)
	}
	defer rows.Close()

	var services []models.Service
	for rows.Next() {
		service, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		services = append(services, *service)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating services: %w", err)
	}

	return services, nil
}

// Update updates a service in the database.
func (r *serviceRepo) Update(ctx context.Context, service *models.Service) error {
	query := `
		UPDATE services
		SET type = $1, status = $2, endpoints = $3, version = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING updated_at
	`

	// Marshal endpoints to JSONB
	var endpointsJSON []byte
	var err error
	if service.Endpoints != nil {
		endpointsJSON, err = json.Marshal(service.Endpoints)
		if err != nil {
			return fmt.Errorf("failed to marshal endpoints: %w", err)
		}
	}

	err = r.pool.QueryRow(
		ctx,
		query,
		service.CatalogID,
		service.Status,
		endpointsJSON,
		sql.NullString{String: service.Version, Valid: service.Version != ""},
		service.ID,
	).Scan(&service.UpdatedAt)

	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}

		return fmt.Errorf("failed to update service: %w", err)
	}

	return nil
}

// UpdateStatus updates only the status and message of a service.
func (r *serviceRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status models.ServiceStatus, message string) error {
	query := `
		UPDATE services
		SET status = $1, message = $2, updated_at = NOW()
		WHERE id = $3
	`

	_, err := r.pool.Exec(ctx, query, status, sql.NullString{String: message, Valid: message != ""}, id)
	if err != nil {
		return fmt.Errorf("failed to update service status: %w", err)
	}

	return nil
}

// UpdateEndpoints updates only the endpoints of a service.
func (r *serviceRepo) UpdateEndpoints(ctx context.Context, id uuid.UUID, endpoints []map[string]any) error {
	query := `
		UPDATE services
		SET endpoints = $1, updated_at = NOW()
		WHERE id = $2
	`

	// Marshal endpoints to JSONB
	var endpointsJSON []byte
	var err error
	if endpoints != nil {
		endpointsJSON, err = json.Marshal(endpoints)
		if err != nil {
			return fmt.Errorf("failed to marshal endpoints: %w", err)
		}
	}

	_, err = r.pool.Exec(ctx, query, endpointsJSON, id)
	if err != nil {
		return fmt.Errorf("failed to update service endpoints: %w", err)
	}

	return nil
}

// Made with Bob
