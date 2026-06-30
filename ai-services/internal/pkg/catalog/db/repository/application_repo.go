package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// ApplicationFilters defines optional filters for querying applications.
type ApplicationFilters struct {
	DeploymentType string // Optional: filter by deployment_type ("architectures" or "services")
	CatalogID      string // Optional: filter by catalog_id (e.g., "rag", "chat", "digitize")
	Limit          int    // Optional: number of records to return (for pagination)
	Offset         int    // Optional: number of records to skip (for pagination)
}

// ApplicationRepository defines the interface for application data operations.
type ApplicationRepository interface {
	// GetAll retrieves all applications from the database with optional filters and pagination.
	GetAll(ctx context.Context, filters *ApplicationFilters) ([]models.Application, error)
	// GetCount returns the total count of applications matching the filters.
	GetCount(ctx context.Context, filters *ApplicationFilters) (int, error)
	// GetByID retrieves an application by ID with its associated services.
	GetByID(ctx context.Context, id uuid.UUID) (*models.Application, error)
	// GetByName retrieves an application by name with its associated services.
	GetByName(ctx context.Context, name string) (*models.Application, error)
	// Insert creates a new application in the database.
	Insert(ctx context.Context, app *models.Application) error
	// UpdateDeploymentName updates the deployment name (name field) of an application.
	UpdateDeploymentName(ctx context.Context, id uuid.UUID, name string) error
	// UpdateStatus updates the status and message of an application.
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.ApplicationStatus, message string) error
	// Delete removes an application from the database.
	Delete(ctx context.Context, id uuid.UUID) error
}

// applicationRepo implements ApplicationRepository using pgx.
type applicationRepo struct {
	pool *pgxpool.Pool
}

// scannedServiceFields holds the raw scanned fields from a service row.
type scannedServiceFields struct {
	id        uuid.UUID
	appID     uuid.UUID
	catalogID string
	status    string
	message   sql.NullString
	endpoint  []byte
	version   string
	created   sql.NullTime
	updated   sql.NullTime
}

// NewApplicationRepository creates a new ApplicationRepository instance.
func NewApplicationRepository(pool *pgxpool.Pool) ApplicationRepository {
	return &applicationRepo{pool: pool}
}

// GetAll retrieves all applications from the database with optional filters.
// Includes associated services for the paginated application set.
func (r *applicationRepo) GetAll(ctx context.Context, filters *ApplicationFilters) ([]models.Application, error) {
	query, args := r.buildGetAllQuery(filters)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query applications: %w", err)
	}
	defer rows.Close()

	return r.scanApplicationsWithServices(rows)
}

// buildGetAllQuery constructs the SQL query and arguments for GetAll.
func (r *applicationRepo) buildGetAllQuery(filters *ApplicationFilters) (string, []interface{}) {
	args := []interface{}{}
	whereClauses := []string{}

	// Build WHERE clause dynamically based on provided filters
	if filters != nil {
		if filters.DeploymentType != "" {
			whereClauses = append(whereClauses, fmt.Sprintf("a.deployment_type = $%d", len(args)+1))
			args = append(args, filters.DeploymentType)
		}

		if filters.CatalogID != "" {
			whereClauses = append(whereClauses, fmt.Sprintf("a.catalog_id = $%d", len(args)+1))
			args = append(args, filters.CatalogID)
		}
	}

	query := `
		WITH paged_applications AS (
			SELECT
				a.id, a.name, a.catalog_id, a.deployment_type, a.status, a.message, a.version, a.created_by, a.created_at, a.updated_at
			FROM applications a
	`

	// Add WHERE clause if any filters are present
	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	query += " ORDER BY a.created_at DESC"

	// Add pagination if provided
	if filters != nil {
		if filters.Limit > 0 {
			query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
			args = append(args, filters.Limit)
		}
		if filters.Offset > 0 {
			query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
			args = append(args, filters.Offset)
		}
	}

	query += `
		)
		SELECT
			a.id, a.name, a.catalog_id, a.deployment_type, a.status, a.message, a.version, a.created_by, a.created_at, a.updated_at,
			s.id, s.app_id, s.catalog_id, s.status, s.message, s.endpoints, s.version, s.created_at, s.updated_at
		FROM paged_applications a
		INNER JOIN services s ON a.id = s.app_id
		ORDER BY a.created_at DESC, s.created_at ASC
	`

	return query, args
}

// scanApplicationsWithServices scans rows into Application structs with their services.
func (r *applicationRepo) scanApplicationsWithServices(rows pgx.Rows) ([]models.Application, error) {
	appMap := make(map[uuid.UUID]*models.Application)
	var appOrder []uuid.UUID

	for rows.Next() {
		var (
			app     models.Application
			message sql.NullString
			svc     scannedServiceFields
		)

		err := rows.Scan(
			&app.ID, &app.Name, &app.CatalogID, &app.DeploymentType, &app.Status,
			&message, &app.Version, &app.CreatedBy, &app.CreatedAt, &app.UpdatedAt,
			&svc.id, &svc.appID, &svc.catalogID, &svc.status, &svc.message,
			&svc.endpoint, &svc.version, &svc.created, &svc.updated,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan application with services: %w", err)
		}

		appID := app.ID

		if message.Valid {
			app.Message = message.String
		}

		// If this is a new application, add it to the map
		if _, exists := appMap[appID]; !exists {
			appMap[appID] = &app
			appOrder = append(appOrder, appID)
		}

		// Add service to the application
		service, err := svc.toService()
		if err != nil {
			return nil, fmt.Errorf("failed to convert service: %w", err)
		}
		appMap[appID].Services = append(appMap[appID].Services, *service)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating applications: %w", err)
	}

	// Convert map to slice in original order
	applications := make([]models.Application, 0, len(appOrder))
	for _, appID := range appOrder {
		applications = append(applications, *appMap[appID])
	}

	return applications, nil
}

// GetCount returns the total count of applications matching the filters.
// This is used for pagination metadata.
func (r *applicationRepo) GetCount(ctx context.Context, filters *ApplicationFilters) (int, error) {
	query := `SELECT COUNT(DISTINCT a.id) FROM applications a`
	args := []interface{}{}
	argIndex := 1
	whereAdded := false

	// Add deployment_type filter if provided
	if filters != nil && filters.DeploymentType != "" {
		query += fmt.Sprintf(" WHERE a.deployment_type = $%d", argIndex)
		args = append(args, filters.DeploymentType)
		argIndex++
		whereAdded = true
	}

	// Add catalog_id filter if provided
	if filters != nil && filters.CatalogID != "" {
		if whereAdded {
			query += fmt.Sprintf(" AND a.catalog_id = $%d", argIndex)
		} else {
			query += fmt.Sprintf(" WHERE a.catalog_id = $%d", argIndex)
		}
		args = append(args, filters.CatalogID)
	}

	var count int
	err := r.pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get application count: %w", err)
	}

	return count, nil
}

// toService converts scanned fields to a Service model.
func (s *scannedServiceFields) toService() (*models.Service, error) {
	service := &models.Service{
		ID:        s.id,
		AppID:     s.appID,
		CatalogID: s.catalogID,
		Status:    models.ServiceStatus(s.status),
		Version:   s.version,
		CreatedAt: s.created.Time,
		UpdatedAt: s.updated.Time,
	}

	if s.message.Valid {
		service.Message = s.message.String
	}

	if len(s.endpoint) > 0 {
		var endpoints []map[string]any
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
		&app.ID, &app.Name, &app.CatalogID, &app.DeploymentType, &app.Status,
		&message, &app.Version, &app.CreatedBy, &app.CreatedAt, &app.UpdatedAt,
		&svc.id, &svc.appID, &svc.catalogID, &svc.status, &svc.message,
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

		app.Services = append(app.Services, *service)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating application rows: %w", err)
	}

	if app == nil {
		return nil, nil
	}

	return app, nil
}

// GetByID retrieves an application by ID with its associated services using JOIN.
func (r *applicationRepo) GetByID(ctx context.Context, id uuid.UUID) (*models.Application, error) {
	query := `
		SELECT
			a.id, a.name, a.catalog_id, a.deployment_type, a.status, a.message, a.version, a.created_by, a.created_at, a.updated_at,
			s.id, s.app_id, s.catalog_id, s.status, s.message, s.endpoints, s.version, s.created_at, s.updated_at
		FROM applications a
		INNER JOIN services s ON a.id = s.app_id
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
			a.id, a.name, a.catalog_id, a.deployment_type, a.status, a.message, a.version, a.created_by, a.created_at, a.updated_at,
			s.id, s.app_id, s.catalog_id, s.status, s.message, s.endpoints, s.version, s.created_at, s.updated_at
		FROM applications a
		LEFT JOIN services s ON a.id = s.app_id
		WHERE LOWER(a.name) = LOWER($1)
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
		INSERT INTO applications (id, name, catalog_id, deployment_type, status, message, version, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
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
		app.CatalogID,
		app.DeploymentType,
		app.Status,
		sql.NullString{String: app.Message, Valid: app.Message != ""},
		sql.NullString{String: app.Version, Valid: app.Version != ""},
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

	_, err := r.pool.Exec(ctx, query, name, id)
	if err != nil {
		return fmt.Errorf("failed to update application name: %w", err)
	}

	return nil
}

// UpdateStatus updates the status and message of an application.
func (r *applicationRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status models.ApplicationStatus, message string) error {
	query := `
		UPDATE applications
		SET status = $1, message = $2, updated_at = NOW()
		WHERE id = $3
	`

	_, err := r.pool.Exec(ctx, query, status, sql.NullString{String: message, Valid: message != ""}, id)
	if err != nil {
		return fmt.Errorf("failed to update application status: %w", err)
	}

	return nil
}

// Delete removes an application from the database.
// Due to CASCADE constraint, associated services will be automatically deleted.
func (r *applicationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM applications WHERE id = $1`

	_, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete application: %w", err)
	}

	return nil
}

// Made with Bob
