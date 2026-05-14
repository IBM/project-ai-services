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

// scannedServiceFields holds the raw scanned fields from a service row.
type scannedServiceFields struct {
	id       uuid.NullUUID
	appID    uuid.NullUUID
	typ      sql.NullString
	status   sql.NullString
	endpoint []byte
	version  sql.NullString
	created  sql.NullTime
	updated  sql.NullTime
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

// toService converts scanned fields to a Service model.
// Returns nil if the service ID is not valid (LEFT JOIN found no matching service).
func (s *scannedServiceFields) toService() (*models.Service, error) {
	if !s.id.Valid {
		return nil, nil
	}

	service := &models.Service{
		ID:        s.id.UUID,
		AppID:     s.appID.UUID,
		Type:      s.typ.String,
		Status:    models.ApplicationStatus(s.status.String),
		CreatedAt: s.created.Time,
		UpdatedAt: s.updated.Time,
	}

	if s.version.Valid {
		service.Version = s.version.String
	}

	if len(s.endpoint) > 0 {
		var endpoints map[string]any
		if err := json.Unmarshal(s.endpoint, &endpoints); err != nil {
			return nil, fmt.Errorf("failed to unmarshal service endpoints: %w", err)
		}
		service.Endpoints = endpoints
	}

	return service, nil
}

// scanApplicationWithService scans one row from the application+services JOIN query.
func scanApplicationWithService(rows pgx.Rows, app *models.Application) (*models.Service, error) {
	var (
		message sql.NullString
		svc     scannedServiceFields
	)

	err := rows.Scan(
		&app.ID, &app.Name, &app.Template, &app.Status,
		&message, &app.CreatedBy, &app.CreatedAt, &app.UpdatedAt,
		&svc.id, &svc.appID, &svc.typ, &svc.status,
		&svc.endpoint, &svc.version, &svc.created, &svc.updated,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan application with services: %w", err)
	}

	if message.Valid {
		app.Message = message.String
	}

	return svc.toService()
}

// collectApplication iterates rows from a JOIN query into a single Application with its services.
func collectApplication(rows pgx.Rows) (*models.Application, error) {
	var app *models.Application

	for rows.Next() {
		if app == nil {
			app = &models.Application{}
		}

		service, err := scanApplicationWithService(rows, app)
		if err != nil {
			return nil, err
		}

		if service != nil {
			app.Services = append(app.Services, *service)
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

// GetByID retrieves an application by ID with its associated services using JOIN.
func (r *applicationRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Application, error) {
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

	return collectApplication(rows)
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

	return collectApplication(rows)
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
