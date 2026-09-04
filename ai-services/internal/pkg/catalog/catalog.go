package catalog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	texttemplate "text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"go.yaml.in/yaml/v3"
)

// bundleStorageRoot is the well-known mount path for the dedicated catalog-bundles
// volume — identical on Podman and OpenShift. Must match archive.go's constant.
const bundleStorageRoot = "/data/catalog-bundles"

// catalogItem represents a cached catalog item with its metadata and path.
type catalogItem struct {
	Path         string // Application path (e.g., "embedding/vllm-cpu")
	Architecture *types.Architecture
	Service      *types.Service
	Component    *types.Component
	Connector    *types.Connector
	// itemFS is the filesystem from which this item was loaded.
	// Embedded items carry &assets.CatalogFS; bundle items carry an os.DirFS.
	itemFS fs.FS
}

// CatalogProvider provides access to catalog items.
// It is safe for concurrent use; all reads are protected by a read lock and
// Reload acquires the write lock while rebuilding the items map.
type CatalogProvider struct {
	mu         sync.RWMutex
	items      map[string]*catalogItem
	bundleRepo dbrepo.BundleRepository // nil on CLI / test paths — skips bundle loading
}

// NewCatalogProvider creates a new CatalogProvider, loading all embedded items and
// any active customer-created bundles from the DB.
//
// bundleRepo may be nil (CLI / test paths) — in that case only embedded items are loaded.
func NewCatalogProvider(bundleRepo dbrepo.BundleRepository) (*CatalogProvider, error) {
	p := &CatalogProvider{bundleRepo: bundleRepo}

	items := make(map[string]*catalogItem)
	if err := p.loadEmbeddedItems(context.Background(), items); err != nil {
		return nil, err
	}
	if bundleRepo != nil {
		if err := p.loadBundleItems(context.Background(), items); err != nil {
			return nil, err
		}
	}

	p.items = items

	return p, nil
}

// Reload rebuilds the items map from scratch under the write lock.
// It re-walks the embedded FS and, when bundleRepo is non-nil, re-queries all
// active bundles from the DB. Called synchronously after every successful
// ProcessBundle, ReplaceBundle, and DeleteBundle operation.
func (p *CatalogProvider) Reload(ctx context.Context) error {
	items := make(map[string]*catalogItem)

	if err := p.loadEmbeddedItems(ctx, items); err != nil {
		return err
	}
	if p.bundleRepo != nil {
		if err := p.loadBundleItems(ctx, items); err != nil {
			return err
		}
	}

	p.mu.Lock()
	p.items = items
	p.mu.Unlock()

	return nil
}

// loadEmbeddedItems walks assets.CatalogFS and populates items with all embedded catalog entries.
func (p *CatalogProvider) loadEmbeddedItems(ctx context.Context, items map[string]*catalogItem) error {
	err := fs.WalkDir(&assets.CatalogFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || filepath.Base(path) != "metadata.yaml" {
			return nil
		}

		return processMetadataFile(ctx, path, items)
	})

	if err != nil {
		return fmt.Errorf("failed to walk catalog filesystem: %w", err)
	}

	return nil
}

// loadBundleItems queries the DB for all active bundles and loads each one via os.DirFS.
// The on-disk path mirrors bundleDirPath: <bundleStorageRoot>/<catalog_type>s/<catalog_id>-<version>.
// Bundle items overwrite the same-keyed embedded item if their catalog_id collides.
func (p *CatalogProvider) loadBundleItems(ctx context.Context, items map[string]*catalogItem) error {
	bundles, err := p.bundleRepo.GetAll(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to list bundles for catalog reload: %w", err)
	}

	for i := range bundles {
		b := &bundles[i]
		if string(b.Status) != "active" {
			continue
		}

		// Derive the on-disk path: /data/catalog-bundles/<catalog_type>s/<catalog_id>-<version>/
		// e.g. service → services/my-service-1.0.0/, component → components/llm--prov-1.0.0/
		bundleDir := filepath.Join(bundleStorageRoot, b.CatalogType+"s", b.CatalogID+"-"+b.Version)
		bundleFS := os.DirFS(bundleDir)

		data, readErr := fs.ReadFile(bundleFS, "metadata.yaml")
		if readErr != nil {
			logger.ErrorfCtx(ctx, "bundle %s: failed to read metadata.yaml from %s: %v", b.ID, bundleDir, readErr)

			continue
		}

		// Map catalog_type "service"/"component" → "services"/"components" to match
		// the embedded-FS dispatch keys used in parseAndStoreMetadata.
		catalogType := b.CatalogType + "s"
		if err := parseAndStoreMetadataWithFS(ctx, catalogType, "metadata.yaml", "", bundleFS, data, items); err != nil {
			logger.ErrorfCtx(ctx, "bundle %s: failed to parse metadata: %v", b.ID, err)
		}
	}

	return nil
}

// processMetadataFile processes a single metadata.yaml file from the embedded CatalogFS.
func processMetadataFile(ctx context.Context, path string, items map[string]*catalogItem) error {
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
		logger.DebugfCtx(ctx, "failed to read metadata at %s: %v", path, readErr)

		return nil
	}

	appPath := filepath.Dir(path)

	return parseAndStoreMetadataWithFS(ctx, catalogType, path, appPath, &assets.CatalogFS, data, items)
}

// parseAndStoreMetadataWithFS parses metadata and stores it in the items map.
// itemFS is &assets.CatalogFS for embedded items and an os.DirFS for bundle items.
func parseAndStoreMetadataWithFS(ctx context.Context, catalogType, path, appPath string, itemFS fs.FS, data []byte, items map[string]*catalogItem) error {
	switch catalogType {
	case constants.CatalogTypeArchitectures:
		return parseArchitecture(ctx, path, appPath, itemFS, data, items)
	case constants.CatalogTypeServices:
		return parseService(ctx, path, appPath, itemFS, data, items)
	case constants.CatalogTypeComponents:
		return parseComponent(ctx, path, appPath, itemFS, data, items)
	case constants.CatalogTypeConnectors:
		return parseConnector(ctx, path, appPath, itemFS, data, items)
	}

	return nil
}

// isValidMetadataPath checks if the metadata file path is valid for the catalog type.
func isValidMetadataPath(catalogType string, pathLength int) bool {
	switch catalogType {
	case constants.CatalogTypeArchitectures, constants.CatalogTypeServices:
		return pathLength == constants.MinPathPartsForArchOrService
	case constants.CatalogTypeComponents, constants.CatalogTypeConnectors:
		return pathLength == constants.MinPathPartsForComponent
	default:
		return false
	}
}

// parseArchitecture parses and stores an architecture.
func parseArchitecture(ctx context.Context, path, appPath string, itemFS fs.FS, data []byte, items map[string]*catalogItem) error {
	var arch types.Architecture
	if unmarshalErr := yaml.Unmarshal(data, &arch); unmarshalErr != nil {
		logger.DebugfCtx(ctx, "failed to parse architecture at %s: %v", path, unmarshalErr)

		return nil
	}

	items[arch.ID] = &catalogItem{
		Path:         appPath,
		Architecture: &arch,
		itemFS:       itemFS,
	}

	return nil
}

// parseService parses and stores a service.
func parseService(ctx context.Context, path, appPath string, itemFS fs.FS, data []byte, items map[string]*catalogItem) error {
	var svc types.Service
	if unmarshalErr := yaml.Unmarshal(data, &svc); unmarshalErr != nil {
		logger.DebugfCtx(ctx, "failed to parse service at %s: %v", path, unmarshalErr)

		return nil
	}

	items[svc.ID] = &catalogItem{
		Path:    appPath,
		Service: &svc,
		itemFS:  itemFS,
	}

	return nil
}

// parseComponent parses and stores a component.
func parseComponent(ctx context.Context, path, appPath string, itemFS fs.FS, data []byte, items map[string]*catalogItem) error {
	var comp types.Component
	if unmarshalErr := yaml.Unmarshal(data, &comp); unmarshalErr != nil {
		logger.DebugfCtx(ctx, "failed to parse component at %s: %v", path, unmarshalErr)

		return nil
	}

	// Use composite key for components: {component_type}/{id}
	// This allows same ID across different component types
	componentKey := fmt.Sprintf("%s/%s", comp.ComponentType, comp.ID)
	items[componentKey] = &catalogItem{
		Path:      appPath,
		Component: &comp,
		itemFS:    itemFS,
	}

	return nil
}

// parseConnector parses and stores a connector.
func parseConnector(ctx context.Context, path, appPath string, itemFS fs.FS, data []byte, items map[string]*catalogItem) error {
	var conn types.Connector
	if unmarshalErr := yaml.Unmarshal(data, &conn); unmarshalErr != nil {
		logger.DebugfCtx(ctx, "failed to parse connector at %s: %v", path, unmarshalErr)

		return nil
	}

	// Use composite key for connectors: {connector_type}/{id}
	// This allows same ID across different connector types
	connectorKey := fmt.Sprintf("%s/%s", conn.ConnectorType, conn.ID)
	items[connectorKey] = &catalogItem{
		Path:      appPath,
		Connector: &conn,
		itemFS:    itemFS,
	}

	return nil
}

// getItem returns the catalogItem for the given key, holding a read lock.
func (p *CatalogProvider) getItem(key string) (*catalogItem, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	item, ok := p.items[key]

	return item, ok
}

// allItems returns a shallow copy of the items map, holding a read lock.
func (p *CatalogProvider) allItems() map[string]*catalogItem {
	p.mu.RLock()
	defer p.mu.RUnlock()
	snapshot := make(map[string]*catalogItem, len(p.items))
	for k, v := range p.items {
		snapshot[k] = v
	}

	return snapshot
}

// LoadArchitecture loads an architecture by ID from cache.
func (p *CatalogProvider) LoadArchitecture(id string) (*types.Architecture, error) {
	item, ok := p.getItem(id)
	if !ok || item.Architecture == nil {
		return nil, fmt.Errorf("architecture '%s' not found", id)
	}

	return item.Architecture, nil
}

// LoadService loads a service by ID from cache.
func (p *CatalogProvider) LoadService(id string) (*types.Service, error) {
	item, ok := p.getItem(id)
	if !ok || item.Service == nil {
		return nil, fmt.Errorf("service '%s' not found", id)
	}

	return item.Service, nil
}

// LoadComponent loads a component by component type and ID from cache.
// componentType examples: "embedding", "llm", "reranker", "vector_db".
func (p *CatalogProvider) LoadComponent(componentType, id string) (*types.Component, error) {
	componentKey := fmt.Sprintf("%s/%s", componentType, id)
	item, ok := p.getItem(componentKey)
	if !ok || item.Component == nil {
		return nil, fmt.Errorf("component '%s/%s' not found", componentType, id)
	}

	return item.Component, nil
}

// LoadConnector loads a connector by connector type and ID from cache.
// connectorType examples: "datasource".
func (p *CatalogProvider) LoadConnector(connectorType, id string) (*types.Connector, error) {
	connectorKey := fmt.Sprintf("%s/%s", connectorType, id)
	item, ok := p.getItem(connectorKey)
	if !ok || item.Connector == nil {
		return nil, fmt.Errorf("connector '%s/%s' not found", connectorType, id)
	}

	return item.Connector, nil
}

// GetCatalogItemPath returns the application path for a given ID.
// This is useful for loading templates and other resources.
func (p *CatalogProvider) GetCatalogItemPath(id string) (string, error) {
	item, ok := p.getItem(id)
	if !ok {
		return "", fmt.Errorf("item '%s' not found", id)
	}

	return item.Path, nil
}

// GetItemFS returns the filesystem for the catalog item identified by key.
// Every item carries a non-nil itemFS: &assets.CatalogFS for embedded items,
// os.DirFS for bundle items.
func (p *CatalogProvider) GetItemFS(key string) (fs.FS, error) {
	item, ok := p.getItem(key)
	if !ok {
		return nil, fmt.Errorf("item '%s' not found", key)
	}

	return item.itemFS, nil
}

// ToServiceSummary converts a Service to ServiceSummary.
func ToServiceSummary(service *types.Service) types.ServiceSummary {
	return types.ServiceSummary{
		ID:            service.ID,
		Name:          service.Name,
		Description:   service.Description,
		CertifiedBy:   service.CertifiedBy,
		Architectures: service.Architectures,
		Standalone:    service.Standalone,
		Dependencies:  service.Dependencies,
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
	snapshot := p.allItems()
	architectures := make([]types.Architecture, 0)
	for _, item := range snapshot {
		if item.Architecture != nil {
			architectures = append(architectures, *item.Architecture)
		}
	}

	return architectures, nil
}

// ListServices lists all available services from cache.
func (p *CatalogProvider) ListServices() ([]types.Service, error) {
	snapshot := p.allItems()
	services := make([]types.Service, 0)
	for _, item := range snapshot {
		if item.Service != nil {
			services = append(services, *item.Service)
		}
	}

	return services, nil
}

// ListComponents lists all available components from cache.
func (p *CatalogProvider) ListComponents() ([]types.Component, error) {
	snapshot := p.allItems()
	components := make([]types.Component, 0)
	for _, item := range snapshot {
		if item.Component != nil {
			components = append(components, *item.Component)
		}
	}

	return components, nil
}

// ListConnectors lists all connectors for a given connector type from cache.
// Returns an error when the type is not registered.
func (p *CatalogProvider) ListConnectors(connectorType string) ([]*types.Connector, error) {
	snapshot := p.allItems()
	result := make([]*types.Connector, 0)
	found := false

	for key, item := range snapshot {
		if item.Connector == nil {
			continue
		}
		if strings.HasPrefix(key, connectorType+"/") {
			result = append(result, item.Connector)
			found = true
		}
	}

	if !found {
		return nil, fmt.Errorf("connector type %q not found", connectorType)
	}

	return result, nil
}

// ListAllConnectors lists every connector across all registered connector types from cache.
func (p *CatalogProvider) ListAllConnectors() []*types.Connector {
	snapshot := p.allItems()
	result := make([]*types.Connector, 0)
	for _, item := range snapshot {
		if item.Connector != nil {
			result = append(result, item.Connector)
		}
	}

	return result
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

// ConnectorExists checks if a connector exists.
func (p *CatalogProvider) ConnectorExists(connectorType, id string) bool {
	_, err := p.LoadConnector(connectorType, id)

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

// LoadServiceValues loads the values.yaml for a service with optional parameter overrides.
// Returns a map of values that can be used for template rendering.
func (p *CatalogProvider) LoadServiceValues(serviceID string, argParams map[string]string) (map[string]any, error) {
	// Verify service exists and get its path from catalog
	_, err := p.LoadService(serviceID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}

	// Get service path from catalog (uses cached path from metadata loading)
	servicePath, err := p.GetCatalogItemPath(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service path: %w", err)
	}

	// Get runtime
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)

	// Read values.yaml using the item's own filesystem (embedded or bundle os.DirFS)
	itemFS, err := p.GetItemFS(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	valuesPath := filepath.Join(servicePath, runtimeStr, "values.yaml")
	valuesData, err := fs.ReadFile(itemFS, valuesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read values.yaml at %s: %w", valuesPath, err)
	}

	// Process @generate annotations for dynamic value generation before parsing
	processedData, err := utils.ProcessGenerateAnnotationsFromYAML(valuesData)
	if err != nil {
		return nil, fmt.Errorf("failed to process generate annotations: %w", err)
	}

	// Parse values
	values := make(map[string]any)
	if err := yaml.Unmarshal(processedData, &values); err != nil {
		return nil, fmt.Errorf("failed to parse values.yaml: %w", err)
	}

	// Apply argParams overrides if provided
	for key, val := range argParams {
		utils.SetNestedValue(values, key, val)
	}

	return values, nil
}

// LoadComponentValues loads the values.yaml for a component with optional parameter overrides.
// Returns a map of values that can be used for template rendering.
func (p *CatalogProvider) LoadComponentValues(componentType, providerID string, argParams map[string]string) (map[string]any, error) {
	// Verify component exists and get its path from catalog
	_, err := p.LoadComponent(componentType, providerID)
	if err != nil {
		return nil, fmt.Errorf("component not found: %w", err)
	}

	// The catalog stores components with key "<component_type>/<id>"
	componentKey := fmt.Sprintf("%s/%s", componentType, providerID)
	componentPath, err := p.GetCatalogItemPath(componentKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get component path: %w", err)
	}

	// Get runtime
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)

	// Read values.yaml using the item's own filesystem (embedded or bundle os.DirFS)
	itemFS, err := p.GetItemFS(componentKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	valuesPath := filepath.Join(componentPath, runtimeStr, "values.yaml")
	valuesData, err := fs.ReadFile(itemFS, valuesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read values.yaml at %s: %w", valuesPath, err)
	}

	// Process @generate annotations for dynamic value generation before parsing
	processedData, err := utils.ProcessGenerateAnnotationsFromYAML(valuesData)
	if err != nil {
		return nil, fmt.Errorf("failed to process generate annotations: %w", err)
	}

	// Parse values
	values := make(map[string]any)
	if err := yaml.Unmarshal(processedData, &values); err != nil {
		return nil, fmt.Errorf("failed to parse values.yaml: %w", err)
	}

	// Apply argParams overrides if provided
	for key, val := range argParams {
		utils.SetNestedValue(values, key, val)
	}

	return values, nil
}

// LoadComponentRuntimeMetadata loads runtime-specific metadata for a component.
// This includes PodTemplateExecutions and other runtime configuration.
// runtimeType selects the runtime subdirectory (e.g. "podman" or "openshift").
// Pass string(vars.RuntimeFactory.GetRuntimeType()) when targeting the local runtime.
func (p *CatalogProvider) LoadComponentRuntimeMetadata(componentType, providerID, runtimeType string) (*clitemplates.AppMetadata, error) {
	// Get component path from catalog
	componentKey := fmt.Sprintf("%s/%s", componentType, providerID)
	componentPath, err := p.GetCatalogItemPath(componentKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get component path: %w", err)
	}

	// Build catalog path with runtime
	catalogPath := filepath.Join(componentPath, runtimeType)

	// Read metadata.yaml using the item's own filesystem
	itemFS, err := p.GetItemFS(componentKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	metadataPath := filepath.Join(catalogPath, "metadata.yaml")
	metadataData, err := fs.ReadFile(itemFS, metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read runtime metadata %s: %w", metadataPath, err)
	}

	var metadata clitemplates.AppMetadata
	if err := yaml.Unmarshal(metadataData, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse runtime metadata: %w", err)
	}

	return &metadata, nil
}

// LoadComponentTemplates loads all pod templates for a component.
// Returns a map of template name to parsed template.
func (p *CatalogProvider) LoadComponentTemplates(componentType, providerID string) (map[string]*texttemplate.Template, error) {
	// Get component path from catalog
	componentKey := fmt.Sprintf("%s/%s", componentType, providerID)
	componentPath, err := p.GetCatalogItemPath(componentKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get component path: %w", err)
	}

	// Get runtime
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)

	// Build catalog path with runtime
	catalogPath := filepath.Join(componentPath, runtimeStr, "templates")

	// Load all template files using the item's own filesystem
	itemFS, err := p.GetItemFS(componentKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	templates := make(map[string]*texttemplate.Template)

	err = fs.WalkDir(itemFS, catalogPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Only process .tmpl and .yaml.tmpl files
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		// Read template file
		templateData, err := fs.ReadFile(itemFS, path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		// Parse template
		templateName := filepath.Base(path)
		tmpl, err := texttemplate.New(templateName).Parse(string(templateData))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", templateName, err)
		}

		templates[templateName] = tmpl

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to load component templates: %w", err)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no templates found in %s", catalogPath)
	}

	return templates, nil
}

// LoadServiceRuntimeMetadata loads runtime-specific metadata for a service.
// This includes PodTemplateExecutions and other runtime configuration.
// runtimeType selects the runtime subdirectory (e.g. "podman" or "openshift").
// Pass string(vars.RuntimeFactory.GetRuntimeType()) when targeting the local runtime.
func (p *CatalogProvider) LoadServiceRuntimeMetadata(serviceID, runtimeType string) (*clitemplates.AppMetadata, error) {
	// Get service path from catalog
	servicePath, err := p.GetCatalogItemPath(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service path: %w", err)
	}

	// Build catalog path with runtime
	catalogPath := filepath.Join(servicePath, runtimeType)

	// Read metadata.yaml using the item's own filesystem
	itemFS, err := p.GetItemFS(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	metadataPath := filepath.Join(catalogPath, "metadata.yaml")
	metadataData, err := fs.ReadFile(itemFS, metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read runtime metadata %s: %w", metadataPath, err)
	}

	var metadata clitemplates.AppMetadata
	if err := yaml.Unmarshal(metadataData, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse runtime metadata: %w", err)
	}

	return &metadata, nil
}

// LoadServiceTemplates loads all pod templates for a service.
// Returns a map of template name to parsed template.
func (p *CatalogProvider) LoadServiceTemplates(serviceID string) (map[string]*texttemplate.Template, error) {
	// Get service path from catalog
	servicePath, err := p.GetCatalogItemPath(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service path: %w", err)
	}

	// Get runtime
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)

	// Build catalog path with runtime
	catalogPath := filepath.Join(servicePath, runtimeStr, "templates")

	// Load all template files using the item's own filesystem
	itemFS, err := p.GetItemFS(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	templates := make(map[string]*texttemplate.Template)

	err = fs.WalkDir(itemFS, catalogPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Only process .tmpl and .yaml.tmpl files
		if !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		// Read template file
		templateData, err := fs.ReadFile(itemFS, path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		// Parse template
		templateName := filepath.Base(path)
		tmpl, err := texttemplate.New(templateName).Parse(string(templateData))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", templateName, err)
		}

		templates[templateName] = tmpl

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to load service templates: %w", err)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no templates found in %s", catalogPath)
	}

	return templates, nil
}

// LoadServicesMD loads all steps md files for a service.
// Returns a map of template name to parsed template.
func (p *CatalogProvider) LoadServicesMD(serviceID string) (map[string]*texttemplate.Template, error) {
	// Get service path from catalog
	servicePath, err := p.GetCatalogItemPath(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service path: %w", err)
	}

	// Get runtime
	runtime := vars.RuntimeFactory.GetRuntimeType()
	runtimeStr := string(runtime)

	// Build catalog path with runtime
	catalogPath := filepath.Join(servicePath, runtimeStr, "steps")

	// Load all md files using the item's own filesystem
	itemFS, err := p.GetItemFS(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	templates := make(map[string]*texttemplate.Template)

	err = fs.WalkDir(itemFS, catalogPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Only process .md files
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		// Read template file
		templateData, err := fs.ReadFile(itemFS, path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		// Parse template
		templateName := filepath.Base(path)
		tmpl, err := texttemplate.New(templateName).Parse(string(templateData))
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", templateName, err)
		}

		templates[templateName] = tmpl

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to load service md files: %w", err)
	}

	if len(templates) == 0 {
		return nil, fmt.Errorf("no md files found in %s", catalogPath)
	}

	return templates, nil
}

// GetServiceSteps returns the raw contents of every file under <runtime>/steps/ for
// the given service, keyed by filename (e.g. "info.md", "next.md", "vars_file.yaml").
// runtime must be a valid RuntimeType ("podman" or "openshift").
// Both embedded and custom bundle services are supported.
// Returns ErrCatalogItemNotFound when the service ID is unknown.
func (p *CatalogProvider) GetServiceSteps(serviceID string, runtime runtimeTypes.RuntimeType) (map[string][]byte, error) {
	if !p.ServiceExists(serviceID) {
		return nil, fmt.Errorf("%w: service '%s'", ErrCatalogItemNotFound, serviceID)
	}

	servicePath, err := p.GetCatalogItemPath(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service path: %w", err)
	}

	stepsPath := filepath.Join(servicePath, string(runtime), "steps")

	itemFS, err := p.GetItemFS(serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get item filesystem: %w", err)
	}

	// do early return if steps folder is not present
	if _, err = fs.Stat(itemFS, stepsPath); errors.Is(err, fs.ErrNotExist) {
		return map[string][]byte{}, nil
	}

	files := make(map[string][]byte)

	err = fs.WalkDir(itemFS, stepsPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		data, err := fs.ReadFile(itemFS, path)
		if err != nil {
			return fmt.Errorf("failed to read steps file %s: %w", path, err)
		}

		files[d.Name()] = data

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk steps directory: %w", err)
	}

	return files, nil
}

// Made with Bob
