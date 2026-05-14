package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// ServiceDependencyRepository defines the interface for service dependency data operations.
type ServiceDependencyRepository interface {
	// AddDependency adds a dependency relationship between a service and another entity (service or component).
	AddDependency(ctx context.Context, dependency *models.ServiceDependency) error
	// RemoveDependency removes a specific dependency relationship.
	RemoveDependency(ctx context.Context, serviceID, dependencyID uuid.UUID) error
	// GetDependenciesByServiceID retrieves all dependencies for a specific service.
	GetDependenciesByServiceID(ctx context.Context, serviceID uuid.UUID) ([]models.ServiceDependency, error)
	// GetServicesByDependency retrieves all services that depend on a specific entity (service or component).
	GetServicesByDependency(ctx context.Context, dependencyID uuid.UUID, dependencyType models.DependencyType) ([]uuid.UUID, error)
	// RemoveAllDependenciesForService removes all dependencies for a specific service.
	RemoveAllDependenciesForService(ctx context.Context, serviceID uuid.UUID) error
}

// serviceDependencyRepo implements ServiceDependencyRepository using pgx.
type serviceDependencyRepo struct {
	pool *pgxpool.Pool
}

// NewServiceDependencyRepository creates a new ServiceDependencyRepository instance.
func NewServiceDependencyRepository(pool *pgxpool.Pool) ServiceDependencyRepository {
	return &serviceDependencyRepo{pool: pool}
}

// AddDependency adds a dependency relationship between a service and another entity.
// Uses ON CONFLICT DO NOTHING to handle duplicate entries gracefully.
func (r *serviceDependencyRepo) AddDependency(ctx context.Context, dependency *models.ServiceDependency) error {
	query := `
		INSERT INTO service_dependencies (service_id, dependency_id, dependency_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (service_id, dependency_id) DO NOTHING
	`

	_, err := r.pool.Exec(ctx, query, dependency.ServiceID, dependency.DependencyID, dependency.DependencyType)
	if err != nil {
		return fmt.Errorf("failed to add service dependency: %w", err)
	}

	return nil
}

// RemoveDependency removes a specific dependency relationship.
func (r *serviceDependencyRepo) RemoveDependency(ctx context.Context, serviceID, dependencyID uuid.UUID) error {
	query := `
		DELETE FROM service_dependencies
		WHERE service_id = $1 AND dependency_id = $2
	`

	result, err := r.pool.Exec(ctx, query, serviceID, dependencyID)
	if err != nil {
		return fmt.Errorf("failed to remove service dependency: %w", err)
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

// GetDependenciesByServiceID retrieves all dependencies for a specific service.
func (r *serviceDependencyRepo) GetDependenciesByServiceID(ctx context.Context, serviceID uuid.UUID) ([]models.ServiceDependency, error) {
	query := `
		SELECT service_id, dependency_id, dependency_type
		FROM service_dependencies
		WHERE service_id = $1
	`

	rows, err := r.pool.Query(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to query service dependencies: %w", err)
	}
	defer rows.Close()

	var dependencies []models.ServiceDependency
	for rows.Next() {
		var dep models.ServiceDependency
		err := rows.Scan(&dep.ServiceID, &dep.DependencyID, &dep.DependencyType)
		if err != nil {
			return nil, fmt.Errorf("failed to scan service dependency: %w", err)
		}
		dependencies = append(dependencies, dep)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating service dependencies: %w", err)
	}

	return dependencies, nil
}

// GetServicesByDependency retrieves all services that depend on a specific entity.
// This is useful for finding which services would be affected if a component or service is removed.
func (r *serviceDependencyRepo) GetServicesByDependency(ctx context.Context, dependencyID uuid.UUID, dependencyType models.DependencyType) ([]uuid.UUID, error) {
	query := `
		SELECT service_id
		FROM service_dependencies
		WHERE dependency_id = $1 AND dependency_type = $2
	`

	rows, err := r.pool.Query(ctx, query, dependencyID, dependencyType)
	if err != nil {
		return nil, fmt.Errorf("failed to query services by dependency: %w", err)
	}
	defer rows.Close()

	var serviceIDs []uuid.UUID
	for rows.Next() {
		var serviceID uuid.UUID
		err := rows.Scan(&serviceID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan service ID: %w", err)
		}
		serviceIDs = append(serviceIDs, serviceID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating service IDs: %w", err)
	}

	return serviceIDs, nil
}

// RemoveAllDependenciesForService removes all dependencies for a specific service.
// This is useful when deleting a service or resetting its dependencies.
func (r *serviceDependencyRepo) RemoveAllDependenciesForService(ctx context.Context, serviceID uuid.UUID) error {
	query := `
		DELETE FROM service_dependencies
		WHERE service_id = $1
	`

	_, err := r.pool.Exec(ctx, query, serviceID)
	if err != nil {
		return fmt.Errorf("failed to remove all dependencies for service: %w", err)
	}

	return nil
}

// Made with Bob
