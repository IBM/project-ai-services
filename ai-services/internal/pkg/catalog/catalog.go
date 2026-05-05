package catalog

import (
	"fmt"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

var (
	// architectureProvider handles architecture catalog operations.
	architectureProvider = templates.NewEmbedTemplateProvider(&assets.CatalogFS, "architectures")
	// serviceProvider handles service catalog operations.
	serviceProvider = templates.NewEmbedTemplateProvider(&assets.CatalogFS, "services")
)

// LoadArchitecture loads an architecture by ID.
func LoadArchitecture(id string) (*types.Architecture, error) {
	var arch types.Architecture
	if err := architectureProvider.LoadMetadata(id, false, &arch); err != nil {
		return nil, fmt.Errorf("failed to load architecture '%s': %w", id, err)
	}

	return &arch, nil
}

// LoadService loads a service by ID (base metadata only).
func LoadService(id string) (*types.Service, error) {
	var service types.Service
	if err := serviceProvider.LoadMetadata(id, false, &service); err != nil {
		return nil, fmt.Errorf("failed to load service '%s': %w", id, err)
	}

	return &service, nil
}

// ToServiceSummary converts a Service to ServiceSummary.
func ToServiceSummary(service *types.Service) types.ServiceSummary {
	return types.ServiceSummary{
		ID:            service.ID,
		Name:          service.Name,
		Description:   service.Description,
		CertifiedBy:   service.CertifiedBy,
		Architectures: service.Architectures,
	}
}

// ToArchitectureSummary converts an Architecture to ArchitectureSummary.
func ToArchitectureSummary(arch *types.Architecture) types.ArchitectureSummary {
	// Extract just the service IDs as strings
	services := make([]string, len(arch.Services))
	for i, svc := range arch.Services {
		services[i] = svc.ID
	}

	return types.ArchitectureSummary{
		ID:          arch.ID,
		Name:        arch.Name,
		Description: arch.Description,
		CertifiedBy: arch.CertifiedBy,
		Services:    services,
	}
}

// ListArchitectures lists all available architectures.
func ListArchitectures() ([]types.Architecture, error) {
	archIDs, err := architectureProvider.ListApplications(true)
	if err != nil {
		return nil, fmt.Errorf("failed to list architectures: %w", err)
	}

	architectures := make([]types.Architecture, 0, len(archIDs))
	for _, id := range archIDs {
		arch, err := LoadArchitecture(id)
		if err != nil {
			// Log error but continue with other architectures
			continue
		}
		architectures = append(architectures, *arch)
	}

	return architectures, nil
}

// ListServices lists all available deployable services
// Only returns services where DependencyOnly is false (default).
func ListServices() ([]types.Service, error) {
	serviceIDs, err := serviceProvider.ListApplications(true)
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	var services []types.Service
	for _, id := range serviceIDs {
		service, err := LoadService(id)
		if err != nil {
			// Log error but continue with other services
			continue
		}

		// Only include services that are not dependency-only
		if !service.DependencyOnly {
			services = append(services, *service)
		}
	}

	return services, nil
}

// ListServicesWithRuntime lists all available deployable services
// Runtime parameter kept for API compatibility but not used
// Only returns services where DependencyOnly is false (default).
func ListServicesWithRuntime(runtime runtimeTypes.RuntimeType) ([]types.Service, error) {
	return ListServices()
}

// ArchitectureExists checks if an architecture exists.
func ArchitectureExists(id string) bool {
	_, err := LoadArchitecture(id)

	return err == nil
}

// ServiceExists checks if a service exists.
func ServiceExists(id string) bool {
	_, err := LoadService(id)

	return err == nil
}

// ResolveServiceDependencies resolves all dependencies for one or more services recursively
// Returns a flat list of all unique service IDs needed (including the services themselves)
// Accepts either service IDs (strings) or ServiceReferences.
func ResolveServiceDependencies(services ...interface{}) ([]string, error) {
	visited := make(map[string]bool)
	var result []string

	for _, svc := range services {
		var serviceID string
		switch v := svc.(type) {
		case string:
			serviceID = v
		case types.ServiceReference:
			serviceID = v.ID
		default:
			return nil, fmt.Errorf("invalid service type: %T", svc)
		}

		if err := resolveDependenciesRecursive(serviceID, visited, &result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// resolveDependenciesRecursive performs depth-first traversal of dependencies.
func resolveDependenciesRecursive(serviceID string, visited map[string]bool, result *[]string) error {
	// Check for circular dependencies
	if visited[serviceID] {
		return nil
	}

	// Load service metadata
	service, err := LoadService(serviceID)
	if err != nil {
		return fmt.Errorf("failed to load service '%s': %w", serviceID, err)
	}

	// Mark as visited
	visited[serviceID] = true

	// Recursively resolve all dependencies (all are required)
	for _, dep := range service.Dependencies {
		if err := resolveDependenciesRecursive(dep.ID, visited, result); err != nil {
			return err
		}
	}

	// Add current service to result
	*result = append(*result, serviceID)

	return nil
}

// GetDeploymentOrder returns services grouped into deployment layers.
// Services in the same layer can be deployed in parallel.
func GetDeploymentOrder(serviceIDs []string) ([][]string, error) {
	graph, inDegree, err := buildDependencyGraph(serviceIDs)
	if err != nil {
		return nil, err
	}

	layers := performTopologicalSort(graph, inDegree)

	if err := validateNoCircularDependencies(layers, serviceIDs); err != nil {
		return nil, err
	}

	return layers, nil
}

// buildDependencyGraph creates a dependency graph for the given services.
func buildDependencyGraph(serviceIDs []string) (map[string][]string, map[string]int, error) {
	graph := make(map[string][]string)
	inDegree := make(map[string]int)

	// Initialize all services
	for _, svcID := range serviceIDs {
		if _, exists := graph[svcID]; !exists {
			graph[svcID] = []string{}
			inDegree[svcID] = 0
		}
	}

	// Build edges (dependencies)
	for _, svcID := range serviceIDs {
		service, err := LoadService(svcID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load service '%s': %w", svcID, err)
		}

		for _, dep := range service.Dependencies {
			// Only add edge if dependency is in our service list
			if _, exists := graph[dep.ID]; exists {
				graph[dep.ID] = append(graph[dep.ID], svcID)
				inDegree[svcID]++
			}
		}
	}

	return graph, inDegree, nil
}

// performTopologicalSort performs Kahn's algorithm for topological sorting.
func performTopologicalSort(graph map[string][]string, inDegree map[string]int) [][]string {
	var layers [][]string
	queue := getServicesWithNoDependencies(inDegree)

	for len(queue) > 0 {
		currentLayer := make([]string, len(queue))
		copy(currentLayer, queue)
		layers = append(layers, currentLayer)

		queue = processLayer(queue, graph, inDegree)
	}

	return layers
}

// getServicesWithNoDependencies returns services with no dependencies.
func getServicesWithNoDependencies(inDegree map[string]int) []string {
	var queue []string
	for svcID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, svcID)
		}
	}

	return queue
}

// processLayer processes a layer and returns the next queue.
func processLayer(queue []string, graph map[string][]string, inDegree map[string]int) []string {
	var nextQueue []string
	for _, svcID := range queue {
		for _, dependent := range graph[svcID] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				nextQueue = append(nextQueue, dependent)
			}
		}
	}

	return nextQueue
}

// validateNoCircularDependencies checks for circular dependencies.
func validateNoCircularDependencies(layers [][]string, serviceIDs []string) error {
	processedCount := 0
	for _, layer := range layers {
		processedCount += len(layer)
	}
	if processedCount != len(serviceIDs) {
		return fmt.Errorf("circular dependency detected in services")
	}

	return nil
}

// ValidateDependencies checks if all dependencies for given services exist.
func ValidateDependencies(serviceIDs []string) error {
	for _, svcID := range serviceIDs {
		service, err := LoadService(svcID)
		if err != nil {
			return fmt.Errorf("service '%s' not found: %w", svcID, err)
		}

		// Check all dependencies (all are required)
		for _, dep := range service.Dependencies {
			if !ServiceExists(dep.ID) {
				return fmt.Errorf("service '%s' requires dependency '%s' which does not exist", svcID, dep.ID)
			}
		}
	}

	return nil
}

// Made with Bob
