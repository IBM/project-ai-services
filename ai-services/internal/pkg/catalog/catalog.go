package catalog

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"go.yaml.in/yaml/v3"
)

// catalogItem represents a cached catalog item with its metadata and path.
type catalogItem struct {
	Path         string // Application path (e.g., "embedding/vllm-cpu")
	Architecture *types.Architecture
	Service      *types.Service
	Component    *types.Component
}

// CatalogProvider provides access to catalog items.
type CatalogProvider struct{}

var (
	sharedItems map[string]*catalogItem
	once        sync.Once
	loadErr     error
)

// NewCatalogProvider creates a new catalog provider instance.
// The shared items map is loaded only once on the first call (thread-safe).
func NewCatalogProvider() (*CatalogProvider, error) {
	once.Do(func() {
		sharedItems = make(map[string]*catalogItem)
		loadErr = loadCatalogItems(sharedItems)
	})

	if loadErr != nil {
		return nil, loadErr
	}

	return &CatalogProvider{}, nil
}

// loadCatalogItems loads all catalog items into the provided map.
func loadCatalogItems(items map[string]*catalogItem) error {
	// Walk the catalog filesystem to find all metadata.yaml files
	err := fs.WalkDir(&assets.CatalogFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || filepath.Base(path) != "metadata.yaml" {
			return nil
		}

		return processMetadataFile(path, items)
	})

	if err != nil {
		return fmt.Errorf("failed to walk catalog filesystem: %w", err)
	}

	return nil
}

// processMetadataFile processes a single metadata.yaml file.
func processMetadataFile(path string, items map[string]*catalogItem) error {
	parts := strings.Split(path, "/")
	if len(parts) < constants.MinPathPartsForArchOrService {
		return nil
	}

	catalogType := parts[0] // "architectures", "services", or "components"

	if !isValidMetadataPath(catalogType, len(parts)) {
		return nil
	}

	data, readErr := assets.CatalogFS.ReadFile(path)
	if readErr != nil {
		logger.Infof("failed to read metadata at %s: %v", path, readErr, logger.VerbosityLevelDebug)

		return nil
	}

	appPath := filepath.Dir(path)

	return parseAndStoreMetadata(catalogType, path, appPath, data, items)
}

// isValidMetadataPath checks if the metadata file path is valid for the catalog type.
func isValidMetadataPath(catalogType string, pathLength int) bool {
	switch catalogType {
	case constants.CatalogTypeArchitectures, constants.CatalogTypeServices:
		return pathLength == constants.MinPathPartsForArchOrService
	case constants.CatalogTypeComponents:
		return pathLength == constants.MinPathPartsForComponent
	default:
		return false
	}
}

// parseAndStoreMetadata parses metadata and stores it in the items map.
func parseAndStoreMetadata(catalogType, path, appPath string, data []byte, items map[string]*catalogItem) error {
	switch catalogType {
	case constants.CatalogTypeArchitectures:
		return parseArchitecture(path, appPath, data, items)
	case constants.CatalogTypeServices:
		return parseService(path, appPath, data, items)
	case constants.CatalogTypeComponents:
		return parseComponent(path, appPath, data, items)
	}

	return nil
}

// parseArchitecture parses and stores an architecture.
func parseArchitecture(path, appPath string, data []byte, items map[string]*catalogItem) error {
	var arch types.Architecture
	if unmarshalErr := yaml.Unmarshal(data, &arch); unmarshalErr != nil {
		logger.Infof("failed to parse architecture at %s: %v", path, unmarshalErr, logger.VerbosityLevelDebug)

		return nil
	}

	items[arch.ID] = &catalogItem{
		Path:         appPath,
		Architecture: &arch,
	}

	return nil
}

// parseService parses and stores a service.
func parseService(path, appPath string, data []byte, items map[string]*catalogItem) error {
	var svc types.Service
	if unmarshalErr := yaml.Unmarshal(data, &svc); unmarshalErr != nil {
		logger.Infof("failed to parse service at %s: %v", path, unmarshalErr, logger.VerbosityLevelDebug)

		return nil
	}

	items[svc.ID] = &catalogItem{
		Path:    appPath,
		Service: &svc,
	}

	return nil
}

// parseComponent parses and stores a component.
func parseComponent(path, appPath string, data []byte, items map[string]*catalogItem) error {
	var comp types.Component
	if unmarshalErr := yaml.Unmarshal(data, &comp); unmarshalErr != nil {
		logger.Infof("failed to parse component at %s: %v", path, unmarshalErr, logger.VerbosityLevelDebug)

		return nil
	}

	// Use composite key for components: {component_type}/{id}
	// This allows same ID across different component types
	componentKey := fmt.Sprintf("%s/%s", comp.ComponentType, comp.ID)
	items[componentKey] = &catalogItem{
		Path:      appPath,
		Component: &comp,
	}

	return nil
}

// LoadArchitecture loads an architecture by ID from cache.
func (p *CatalogProvider) LoadArchitecture(id string) (*types.Architecture, error) {
	item, ok := sharedItems[id]
	if !ok || item.Architecture == nil {
		return nil, fmt.Errorf("architecture '%s' not found", id)
	}

	return item.Architecture, nil
}

// LoadService loads a service by ID from cache.
func (p *CatalogProvider) LoadService(id string) (*types.Service, error) {
	item, ok := sharedItems[id]
	if !ok || item.Service == nil {
		return nil, fmt.Errorf("service '%s' not found", id)
	}

	return item.Service, nil
}

// LoadComponent loads a component by component type and ID from cache.
// componentType examples: "embedding", "llm", "reranker", "vector_db".
func (p *CatalogProvider) LoadComponent(componentType, id string) (*types.Component, error) {
	componentKey := fmt.Sprintf("%s/%s", componentType, id)
	item, ok := sharedItems[componentKey]
	if !ok || item.Component == nil {
		return nil, fmt.Errorf("component '%s/%s' not found", componentType, id)
	}

	return item.Component, nil
}

// GetCatalogItemPath returns the application path for a given ID.
// This is useful for loading templates and other resources.
func (p *CatalogProvider) GetCatalogItemPath(id string) (string, error) {
	item, ok := sharedItems[id]
	if !ok {
		return "", fmt.Errorf("item '%s' not found", id)
	}

	return item.Path, nil
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

// ToComponentSummary converts a Component to ComponentSummary.
func ToComponentSummary(component *types.Component) types.ComponentSummary {
	return types.ComponentSummary{
		ID:            component.ID,
		Name:          component.Name,
		Description:   component.Description,
		ComponentType: component.ComponentType,
	}
}

// ListArchitectures lists all available architectures from cache.
func (p *CatalogProvider) ListArchitectures() ([]types.Architecture, error) {
	architectures := make([]types.Architecture, 0)
	for _, item := range sharedItems {
		if item.Architecture != nil {
			architectures = append(architectures, *item.Architecture)
		}
	}

	return architectures, nil
}

// ListServices lists all available services from cache.
func (p *CatalogProvider) ListServices() ([]types.Service, error) {
	services := make([]types.Service, 0)
	for _, item := range sharedItems {
		if item.Service != nil {
			services = append(services, *item.Service)
		}
	}

	return services, nil
}

// ListComponents lists all available components from cache.
func (p *CatalogProvider) ListComponents() ([]types.Component, error) {
	components := make([]types.Component, 0)
	for _, item := range sharedItems {
		if item.Component != nil {
			components = append(components, *item.Component)
		}
	}

	return components, nil
}

// ListServicesWithRuntime lists all available deployable services
// Runtime parameter kept for API compatibility but not used
// Only returns services where DependencyOnly is false (default).
func (p *CatalogProvider) ListServicesWithRuntime(runtime runtimeTypes.RuntimeType) ([]types.Service, error) {
	return p.ListServices()
}

// ArchitectureExists checks if an architecture exists.
func (p *CatalogProvider) ArchitectureExists(id string) bool {
	_, err := p.LoadArchitecture(id)

	return err == nil
}

// ServiceExists checks if a service exists.
func (p *CatalogProvider) ServiceExists(id string) bool {
	_, err := p.LoadService(id)

	return err == nil
}

// ComponentExists checks if a component exists.
func (p *CatalogProvider) ComponentExists(componentType, id string) bool {
	_, err := p.LoadComponent(componentType, id)

	return err == nil
}

// ResolveServiceDependencies resolves all dependencies for one or more services recursively
// Returns a flat list of all unique service IDs needed (including the services themselves)
// Accepts either service IDs (strings) or ServiceReferences.
func (p *CatalogProvider) ResolveServiceDependencies(services ...interface{}) ([]string, error) {
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

		if err := p.resolveDependenciesRecursive(serviceID, visited, &result); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// resolveDependenciesRecursive performs depth-first traversal of dependencies.
func (p *CatalogProvider) resolveDependenciesRecursive(serviceID string, visited map[string]bool, result *[]string) error {
	// Check for circular dependencies
	if visited[serviceID] {
		return nil
	}

	// Load service metadata
	service, err := p.LoadService(serviceID)
	if err != nil {
		return fmt.Errorf("failed to load service '%s': %w", serviceID, err)
	}

	// Mark as visited
	visited[serviceID] = true

	// Recursively resolve all dependencies (all are required)
	for _, dep := range service.Dependencies {
		if err := p.resolveDependenciesRecursive(dep.ID, visited, result); err != nil {
			return err
		}
	}

	// Add current service to result
	*result = append(*result, serviceID)

	return nil
}

// GetDeploymentOrder returns services grouped into deployment layers.
// Services in the same layer can be deployed in parallel.
func (p *CatalogProvider) GetDeploymentOrder(serviceIDs []string) ([][]string, error) {
	graph, inDegree, err := p.buildDependencyGraph(serviceIDs)
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
func (p *CatalogProvider) buildDependencyGraph(serviceIDs []string) (map[string][]string, map[string]int, error) {
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
		service, err := p.LoadService(svcID)
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
func (p *CatalogProvider) ValidateDependencies(serviceIDs []string) error {
	for _, svcID := range serviceIDs {
		service, err := p.LoadService(svcID)
		if err != nil {
			return fmt.Errorf("service '%s' not found: %w", svcID, err)
		}

		// Check all dependencies (all are required)
		for _, dep := range service.Dependencies {
			if !p.ServiceExists(dep.ID) {
				return fmt.Errorf("service '%s' requires dependency '%s' which does not exist", svcID, dep.ID)
			}
		}
	}

	return nil
}

// Made with Bob
