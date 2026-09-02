package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// LinkedServiceRow is the raw result of the service_dependencies → services → applications
// join. It exposes all fetched columns so consumers can decide what to extract.
// EndpointsJSON is the raw JSONB endpoints array from the services table; consumers are
// responsible for parsing it to extract the URL they need.
type LinkedServiceRow struct {
	// ServiceID is the DB UUID of the linked service.
	ServiceID uuid.UUID
	// ServiceCatalogID is the catalog_id of the service itself (e.g. "digitize").
	ServiceCatalogID string
	// ApplicationID is the DB UUID of the application that owns the service.
	ApplicationID uuid.UUID
	// ApplicationName is the display name of the application.
	ApplicationName string
	// ApplicationCatalogID is the catalog_id of the owning application (e.g. "rag").
	ApplicationCatalogID string
	// ApplicationDeploymentType is the deployment_type of the owning application
	// ("architectures" or "services"), used to resolve the display name from catalog metadata.
	ApplicationDeploymentType string
	// URL is the first api-type endpoint URL for this service (empty when none registered).
	URL string
	// EndpointsJSON is the raw JSONB value of the services.endpoints column.
	// Consumers parse this to extract the endpoint URL relevant to their use case.
	EndpointsJSON json.RawMessage
}

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
	// GetServiceCountByDependency returns the number of services that depend on each ID in
	// dependencyIDs with the given dependencyType, in a single query.
	// IDs with zero dependents are absent from the returned map.
	GetServiceCountByDependency(ctx context.Context, dependencyIDs []uuid.UUID, dependencyType models.DependencyType) (map[uuid.UUID]int, error)
	// RemoveAllDependenciesForService removes all dependencies for a specific service.
	RemoveAllDependenciesForService(ctx context.Context, serviceID uuid.UUID) error
	// GetLinkedServiceEndpoints traverses service_dependencies → services → applications for
	// all rows matching dependencyID + dependencyType, returning the raw DB columns for each
	// linked service. Consumers are responsible for extracting the specific endpoint URL they
	// need from EndpointsJSON.
	GetLinkedServiceEndpoints(ctx context.Context, dependencyID uuid.UUID, dependencyType models.DependencyType) ([]LinkedServiceRow, error)
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

	_, err := r.pool.Exec(ctx, query, serviceID, dependencyID)
	if err != nil {
		return fmt.Errorf("failed to remove service dependency: %w", err)
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

// GetServiceCountByDependency returns, for each connector ID in dependencyIDs, the count of
// services that reference it with the given dependencyType. A single GROUP BY query is issued
// regardless of how many IDs are supplied. IDs with zero dependents are absent from the map.
// Returns an empty (non-nil) map when dependencyIDs is empty.
func (r *serviceDependencyRepo) GetServiceCountByDependency(ctx context.Context, dependencyIDs []uuid.UUID, dependencyType models.DependencyType) (map[uuid.UUID]int, error) {
	counts := make(map[uuid.UUID]int, len(dependencyIDs))
	if len(dependencyIDs) == 0 {
		return counts, nil
	}

	query := `
		SELECT dependency_id, COUNT(*) AS service_count
		FROM service_dependencies
		WHERE dependency_id = ANY($1) AND dependency_type = $2
		GROUP BY dependency_id
	`

	rows, err := r.pool.Query(ctx, query, dependencyIDs, dependencyType)
	if err != nil {
		return nil, fmt.Errorf("failed to count services by dependency: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var depID uuid.UUID
		var count int
		if err := rows.Scan(&depID, &count); err != nil {
			return nil, fmt.Errorf("failed to scan service count: %w", err)
		}
		counts[depID] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating service counts: %w", err)
	}

	return counts, nil
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

// GetLinkedServiceEndpoints joins service_dependencies → services → applications for all
// rows where dependency_id = dependencyID AND dependency_type = dependencyType, returning
// the raw DB columns for each linked service. Consumers parse EndpointsJSON to extract
// the endpoint URL they need. A single query is issued regardless of how many services
// are linked.
func (r *serviceDependencyRepo) GetLinkedServiceEndpoints(ctx context.Context, dependencyID uuid.UUID, dependencyType models.DependencyType) ([]LinkedServiceRow, error) {
	query := `
		SELECT sd.service_id, s.catalog_id, a.id, a.name, a.catalog_id, a.deployment_type, s.endpoints
		FROM service_dependencies sd
		INNER JOIN services     s ON s.id = sd.service_id
		INNER JOIN applications a ON a.id = s.app_id
		WHERE sd.dependency_id   = $1
		  AND sd.dependency_type = $2
	`

	rows, err := r.pool.Query(ctx, query, dependencyID, dependencyType)
	if err != nil {
		return nil, fmt.Errorf("failed to query linked service endpoints: %w", err)
	}
	defer rows.Close()

	var results []LinkedServiceRow
	for rows.Next() {
		var row LinkedServiceRow

		if err := rows.Scan(&row.ServiceID, &row.ServiceCatalogID, &row.ApplicationID, &row.ApplicationName, &row.ApplicationCatalogID, &row.ApplicationDeploymentType, &row.EndpointsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan linked service row: %w", err)
		}

		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating linked service rows: %w", err)
	}

	return results, nil
}

// Made with Bob
