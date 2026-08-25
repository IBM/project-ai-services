package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// LinkedServiceEndpoint holds the information needed to call a downstream service
// (e.g. Digitize) during credential propagation: the service's reachable URL and
// the application it belongs to.
type LinkedServiceEndpoint struct {
	// ServiceID is the DB UUID of the linked service.
	ServiceID uuid.UUID
	// ApplicationID is the DB UUID of the application that owns the service.
	ApplicationID uuid.UUID
	// ApplicationName is the display name of the application.
	ApplicationName string
	// URL is the first api-type endpoint URL for this service (empty when none registered).
	URL string
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
	// RemoveAllDependenciesForService removes all dependencies for a specific service.
	RemoveAllDependenciesForService(ctx context.Context, serviceID uuid.UUID) error
	// GetLinkedServiceEndpoints traverses service_dependencies → services → applications for
	// all rows matching dependencyID + dependencyType, returning the application context and
	// the first api-type endpoint URL for each linked service.
	// Used by the credential propagation and sync-status fetch flows to resolve downstream
	// service base URLs in one query rather than N individual lookups.
	GetLinkedServiceEndpoints(ctx context.Context, dependencyID uuid.UUID, dependencyType models.DependencyType) ([]LinkedServiceEndpoint, error)
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
// the application identity and the first "api"-typed endpoint URL per linked service.
// A single query is issued regardless of how many services are linked.
func (r *serviceDependencyRepo) GetLinkedServiceEndpoints(ctx context.Context, dependencyID uuid.UUID, dependencyType models.DependencyType) ([]LinkedServiceEndpoint, error) {
	query := `
		SELECT sd.service_id, a.id, a.name, s.endpoints
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

	var results []LinkedServiceEndpoint
	for rows.Next() {
		var (
			ep            LinkedServiceEndpoint
			endpointsJSON []byte
		)

		if err := rows.Scan(&ep.ServiceID, &ep.ApplicationID, &ep.ApplicationName, &endpointsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan linked service endpoint row: %w", err)
		}

		ep.URL = extractAPIEndpointURL(endpointsJSON)
		results = append(results, ep)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating linked service endpoints: %w", err)
	}

	return results, nil
}

// extractAPIEndpointURL parses a JSONB endpoints array (shape: [{"type":"...","url":"..."},...])
// and returns the URL of the first entry whose "type" is "api".
// Both Podman and OpenShift deployers register the Digitize backend URL with type "api"
// (Podman route annotation "4000:digitize-backend-<slug>:api"; OpenShift route label
// "ai-services.io/endpoint-type: api"). This is the base URL used to call
// Digitize's /v1/connectors endpoint during credential propagation and sync-status fetch.
// Returns an empty string when the array is empty, malformed, or contains no "api" entry.
func extractAPIEndpointURL(endpointsJSON []byte) string {
	if len(endpointsJSON) == 0 {
		return ""
	}

	var endpoints []map[string]any
	if err := json.Unmarshal(endpointsJSON, &endpoints); err != nil {
		return ""
	}

	for _, ep := range endpoints {
		if t, ok := ep["type"].(string); ok && t == "api" {
			if u, ok := ep["url"].(string); ok {
				return u
			}
		}
	}

	return ""
}

// Made with Bob
