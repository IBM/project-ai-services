// Package repository provides database repository implementations for the catalog service.
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

// ApplicationRepository defines the interface for application data operations.
type ApplicationRepository interface {
	// GetAll retrieves all applications from the database.
	GetAll(ctx context.Context) ([]models.Application, error)
	// GetByID retrieves an application by ID with its associated services.
	GetByID(ctx context.Context, id uuid.UUID) (*models.Application, error)
	// GetByName retrieves an application by name with its associated services.
	GetByName(ctx context.Context, name string) (*models.Application, error)
	// Insert creates a new application in the database.
	Insert(ctx context.Context, app *models.Application) error
	// UpdateDeploymentName updates the deployment name (name field) of an application.
	UpdateDeploymentName(ctx context.Context, id uuid.UUID, name string) error
	// Delete removes an application from the database.
	Delete(ctx context.Context, id uuid.UUID) error
}

// applicationRepo implements ApplicationRepository using pgx.
type applicationRepo struct {
	pool *pgxpool.Pool
}

// NewApplicationRepository creates a new ApplicationRepository instance.
func NewApplicationRepository(pool *pgxpool.Pool) ApplicationRepository {
	return &applicationRepo{pool: pool}
}

// GetAll retrieves all applications from the database.
func (r *applicationRepo) GetAll(ctx context.Context) ([]models.Application, error) {
	query := `
		SELECT id, name, template, status, message, created_by, created_at, updated_at
		FROM applications
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query applications: %w", err)
	}
	defer rows.Close()

	var applications []models.Application
	for rows.Next() {
		var app models.Application
		var message sql.NullString

		err := rows.Scan(
			&app.ID,
			&app.Name,
			&app.Template,
			&app.Status,
			&message,
			&app.CreatedBy,
			&app.CreatedAt,
			&app.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan application: %w", err)
		}

		if message.Valid {
			app.Message = message.String
		}

		applications = append(applications, app)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating applications: %w", err)
	}

	return applications, nil
}

// GetByID retrieves an application by ID with its associated services using JOIN.
func (r *applicationRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Application, error) {
	// below query combines applications and services tables
	// Returns application data + service data in each row
	query := `
		SELECT
			a.id, a.name, a.template, a.status, a.message, a.created_by, a.created_at, a.updated_at,
			s.id, s.app_id, s.type, s.status, s.endpoints, s.version, s.created_at, s.updated_at
		FROM applications a
		LEFT JOIN services s ON a.id = s.app_id
		WHERE a.id = $1
		ORDER BY s.created_at
	`

	rows, err := r.pool.Query(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query application: %w", err)
	}
	defer rows.Close()

	var app *models.Application

	// Each row contains: application data (same in all rows) + one service's data (different per row)
	for rows.Next() {
		// Create application object
		if app == nil {
			app = &models.Application{}
		}

		// Use Null types for optional fields
		var (
			message         sql.NullString // Application message (optional field)
			serviceID       uuid.NullUUID  // Service ID (NULL only during app creation before services added)
			serviceAppID    uuid.NullUUID  // Service app_id (NULL only during app creation)
			serviceType     sql.NullString // Service type (NULL only during app creation)
			serviceStatus   sql.NullString // Service status (NULL only during app creation)
			serviceEndpoint []byte         // Service endpoints JSONB (NULL only during app creation)
			serviceVersion  sql.NullString // Service version (optional field)
			serviceCreated  sql.NullTime   // Service created_at (NULL only during app creation)
			serviceUpdated  sql.NullTime   // Service updated_at (NULL only during app creation)
		)

		// Scan both application and service data from the current row
		err := rows.Scan(
			// Application columns
			&app.ID,
			&app.Name,
			&app.Template,
			&app.Status,
			&message,
			&app.CreatedBy,
			&app.CreatedAt,
			&app.UpdatedAt,
			// Service columns
			&serviceID,
			&serviceAppID,
			&serviceType,
			&serviceStatus,
			&serviceEndpoint,
			&serviceVersion,
			&serviceCreated,
			&serviceUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan application with services: %w", err)
		}

		// Handle optional application message
		if message.Valid {
			app.Message = message.String
		}

		// Add service to the application's services slice if service data exists
		// serviceID.Valid is false when LEFT JOIN finds no matching service
		if serviceID.Valid {
			service := models.Service{
				ID:        serviceID.UUID,
				AppID:     serviceAppID.UUID,
				Type:      serviceType.String,
				Status:    models.ApplicationStatus(serviceStatus.String),
				CreatedAt: serviceCreated.Time,
				UpdatedAt: serviceUpdated.Time,
			}

			// Handle optional service version
			if serviceVersion.Valid {
				service.Version = serviceVersion.String
			}

			// Unmarshal JSONB endpoints if present
			if len(serviceEndpoint) > 0 {
				var endpoints map[string]any
				if err := json.Unmarshal(serviceEndpoint, &endpoints); err != nil {
					return nil, fmt.Errorf("failed to unmarshal service endpoints: %w", err)
				}
				service.Endpoints = endpoints
			}

			// Append this service to the application's services array
			app.Services = append(app.Services, service)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating application rows: %w", err)
	}

	if app == nil {
		return nil, pgx.ErrNoRows
	}

	return app, nil
}

// GetByName retrieves an application by name with its associated services.
func (r *applicationRepo) GetByName(ctx context.Context, name string) (*models.Application, error) {
	query := `
		SELECT 
			a.id, a.name, a.template, a.status, a.message, a.created_by, a.created_at, a.updated_at,
			s.id, s.app_id, s.type, s.status, s.endpoints, s.version, s.created_at, s.updated_at
		FROM applications a
		LEFT JOIN services s ON a.id = s.app_id
		WHERE a.name = $1
		ORDER BY s.created_at
	`

	rows, err := r.pool.Query(ctx, query, name)
	if err != nil {
		return nil, fmt.Errorf("failed to query application: %w", err)
	}
	defer rows.Close()

	var app *models.Application
	for rows.Next() {
		if app == nil {
			app = &models.Application{}
		}

		var (
			message         sql.NullString
			serviceID       uuid.NullUUID
			serviceAppID    uuid.NullUUID
			serviceType     sql.NullString
			serviceStatus   sql.NullString
			serviceEndpoint []byte
			serviceVersion  sql.NullString
			serviceCreated  sql.NullTime
			serviceUpdated  sql.NullTime
		)

		err := rows.Scan(
			&app.ID,
			&app.Name,
			&app.Template,
			&app.Status,
			&message,
			&app.CreatedBy,
			&app.CreatedAt,
			&app.UpdatedAt,
			&serviceID,
			&serviceAppID,
			&serviceType,
			&serviceStatus,
			&serviceEndpoint,
			&serviceVersion,
			&serviceCreated,
			&serviceUpdated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan application with services: %w", err)
		}

		if message.Valid {
			app.Message = message.String
		}

		// Add service if it exists
		if serviceID.Valid {
			service := models.Service{
				ID:        serviceID.UUID,
				AppID:     serviceAppID.UUID,
				Type:      serviceType.String,
				Status:    models.ApplicationStatus(serviceStatus.String),
				CreatedAt: serviceCreated.Time,
				UpdatedAt: serviceUpdated.Time,
			}

			if serviceVersion.Valid {
				service.Version = serviceVersion.String
			}

			if len(serviceEndpoint) > 0 {
				var endpoints map[string]any
				if err := json.Unmarshal(serviceEndpoint, &endpoints); err != nil {
					return nil, fmt.Errorf("failed to unmarshal service endpoints: %w", err)
				}
				service.Endpoints = endpoints
			}

			app.Services = append(app.Services, service)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating application rows: %w", err)
	}

	if app == nil {
		return nil, pgx.ErrNoRows
	}

	return app, nil
}

// Insert creates a new application in the database.
func (r *applicationRepo) Insert(ctx context.Context, app *models.Application) error {
	query := `
		INSERT INTO applications (id, name, template, status, message, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`

	// Generate UUID if not provided
	if app.ID == uuid.Nil {
		app.ID = uuid.New()
	}

	err := r.pool.QueryRow(
		ctx,
		query,
		app.ID,
		app.Name,
		app.Template,
		app.Status,
		sql.NullString{String: app.Message, Valid: app.Message != ""},
		app.CreatedBy,
	).Scan(&app.CreatedAt, &app.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert application: %w", err)
	}

	return nil
}

// UpdateDeploymentName updates the deployment name (name field) of an application.
func (r *applicationRepo) UpdateDeploymentName(ctx context.Context, id uuid.UUID, name string) error {
	query := `
		UPDATE applications
		SET name = $1, updated_at = NOW()
		WHERE id = $2
	`

	result, err := r.pool.Exec(ctx, query, name, id)
	if err != nil {
		return fmt.Errorf("failed to update application name: %w", err)
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

// Delete removes an application from the database.
// Due to CASCADE constraint, associated services will be automatically deleted.
func (r *applicationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM applications WHERE id = $1`

	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete application: %w", err)
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

// Made with Bob
