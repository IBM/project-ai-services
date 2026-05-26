package deployment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/params"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	k8syaml "sigs.k8s.io/yaml"
)

// DeploymentPlanner plans the deployment of applications by:
// 1. Collecting parameters for each service and component
// 2. Deduplicating components (same type + provider + params = single deployment)
// 3. Creating deployment plan with shared components.
type DeploymentPlanner struct {
	catalogProvider *catalog.CatalogProvider
	componentRepo   repository.ComponentRepository
	paramBuilder    *params.ParamBuilder
}

// NewDeploymentPlanner creates a new deployment planner.
func NewDeploymentPlanner(
	provider *catalog.CatalogProvider,
	componentRepo repository.ComponentRepository,
) *DeploymentPlanner {
	return &DeploymentPlanner{
		catalogProvider: provider,
		componentRepo:   componentRepo,
		paramBuilder:    params.NewParamBuilder(provider),
	}
}

// Type aliases for deployment plan types.
type (
	DeploymentPlan = types.DeploymentPlan
	ComponentPlan  = types.ComponentPlan
	ServicePlan    = types.ServicePlan
)

// PlanDeployment creates a deployment plan for an application (architecture or standalone service).
func (p *DeploymentPlanner) PlanDeployment(
	ctx context.Context,
	req apimodels.CreateApplicationRequest,
	runtimeType string,
) (*DeploymentPlan, error) {
	// First, determine if this is an architecture or standalone service
	isArchitecture := false
	_, archErr := p.catalogProvider.LoadArchitecture(req.CatalogID)
	if archErr == nil {
		isArchitecture = true
	} else {
		// Try loading as service
		_, svcErr := p.catalogProvider.LoadService(req.CatalogID)
		if svcErr != nil {
			return nil, fmt.Errorf("catalog_id '%s' not found as architecture or service", req.CatalogID)
		}
	}

	// Create deployment plan
	plan := &DeploymentPlan{
		ApplicationID:   uuid.New(),
		ApplicationName: req.Name,
		CatalogID:       req.CatalogID,
		IsArchitecture:  isArchitecture,
		Components:      make(map[string]*ComponentPlan),
		Services:        make(map[string]*ServicePlan),
	}

	// Process each service from request
	for _, svc := range req.Services {
		if err := p.processService(ctx, svc, plan, runtimeType); err != nil {
			return nil, fmt.Errorf("failed to process service '%s': %w", svc.CatalogID, err)
		}
	}

	// Calculate and allocate Spyre cards after all components are planned
	if err := p.calculateAndAllocateSpyreCards(plan); err != nil {
		return nil, fmt.Errorf("failed to allocate Spyre cards: %w", err)
	}

	return plan, nil
}

// processService processes a single service from the request.
func (p *DeploymentPlanner) processService(
	ctx context.Context,
	svc apimodels.Service,
	plan *DeploymentPlan,
	runtimeType string,
) error {
	// Get service path from catalog provider
	servicePath, err := p.catalogProvider.GetCatalogItemPath(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to get service catalog path: %w", err)
	}

	servicePlan := &ServicePlan{
		CatalogID:     svc.CatalogID,
		CatalogPath:   fmt.Sprintf("%s/%s", servicePath, runtimeType),
		Version:       svc.Version,
		ComponentRefs: make([]string, 0),
	}

	// Process each component in the service
	for _, comp := range svc.Components {
		componentHash, err := p.processComponent(comp, svc.CatalogID, plan, runtimeType)
		if err != nil {
			return fmt.Errorf("failed to process component '%s': %w", comp.ComponentType, err)
		}

		// Add component reference to service
		servicePlan.ComponentRefs = append(servicePlan.ComponentRefs, componentHash)
	}

	// Load values using ParamBuilder
	serviceParams, err := p.paramBuilder.BuildServiceParams(ctx, svc, nil)
	if err != nil {
		return fmt.Errorf("failed to build service params: %w", err)
	}

	// Use values from ParamBuilder (already includes component values nested under component_type)
	servicePlan.Values = serviceParams.Values

	// Extract component values from serviceParams.Values and populate ComponentPlan.Values
	for _, compHash := range servicePlan.ComponentRefs {
		compPlan := plan.Components[compHash]
		// Component values are nested under component_type in serviceParams.Values
		if compValues, ok := serviceParams.Values[compPlan.ComponentType].(map[string]any); ok {
			compPlan.Values = compValues
		}
	}

	// Add service to plan
	plan.Services[svc.CatalogID] = servicePlan

	return nil
}

// processComponent processes a single component from the request and returns its hash.
// If the same component configuration already exists, it reuses it.
func (p *DeploymentPlanner) processComponent(
	comp apimodels.Component,
	catalogID string,
	plan *DeploymentPlan,
	runtimeType string,
) (string, error) {
	// Calculate component hash based on type + provider + params
	// This allows deduplication: same config = same deployment
	componentHash := p.calculateComponentHash(
		comp.ComponentType,
		comp.ProviderID,
		comp.Params,
	)

	// Check if this component configuration already exists in the plan
	if existingComp, exists := plan.Components[componentHash]; exists {
		// Component already planned, just add this service to its users
		existingComp.UsedByServices = append(existingComp.UsedByServices, catalogID)

		return componentHash, nil
	}

	// Get component path from catalog provider
	componentKey := fmt.Sprintf("%s/%s", comp.ComponentType, comp.ProviderID)
	componentPath, err := p.catalogProvider.GetCatalogItemPath(componentKey)
	if err != nil {
		return "", fmt.Errorf("failed to get component catalog path: %w", err)
	}

	// Create new component plan
	compPlan := &ComponentPlan{
		Hash:           componentHash,
		ComponentType:  comp.ComponentType,
		ProviderID:     comp.ProviderID,
		CatalogPath:    fmt.Sprintf("%s/%s", componentPath, runtimeType),
		Params:         comp.Params,
		UsedByServices: []string{catalogID},
	}

	// Add to plan
	plan.Components[componentHash] = compPlan

	return componentHash, nil
}

// calculateComponentHash creates a unique hash for a component configuration.
// Components with same type, provider, and params will have the same hash.
func (p *DeploymentPlanner) calculateComponentHash(
	componentType string,
	providerID string,
	params map[string]any,
) string {
	// Create a deterministic string representation
	hashInput := fmt.Sprintf("%s:%s:", componentType, providerID)

	// Sort and add params to ensure consistent hashing
	paramsJSON, _ := json.Marshal(params)
	hashInput += string(paramsJSON)

	// Calculate SHA256 hash
	hash := sha256.Sum256([]byte(hashInput))

	return fmt.Sprintf("%x", hash[:16]) // Use first 16 bytes (32 hex chars)
}

// calculateAndAllocateSpyreCards calculates required Spyre cards and creates allocation pool.
func (p *DeploymentPlanner) calculateAndAllocateSpyreCards(plan *DeploymentPlan) error {
	totalRequired := 0

	// Calculate total required Spyre cards from all components
	for _, comp := range plan.Components {
		required, err := p.getRequiredSpyreCardsForComponent(comp)
		if err != nil {
			return fmt.Errorf("failed to get Spyre card requirements for component %s: %w", comp.ComponentType, err)
		}
		totalRequired += required
		if required > 0 {
			logger.Infof("Component %s/%s requires %d Spyre cards\n", comp.ComponentType, comp.ProviderID, required)
		}
	}

	if totalRequired == 0 {
		logger.Infof("No Spyre cards required for this deployment\n")

		return nil
	}

	logger.Infof("Total Spyre cards required: %d\n", totalRequired)

	// Find available Spyre cards
	pciAddresses, err := helpers.FindFreeSpyreCards()
	if err != nil {
		return fmt.Errorf("failed to find free Spyre cards: %w", err)
	}

	availableCount := len(pciAddresses)
	logger.Infof("Available Spyre cards: %d\n", availableCount)

	// Validate we have enough Spyre cards
	if availableCount < totalRequired {
		return fmt.Errorf("insufficient Spyre cards: required %d, available %d", totalRequired, availableCount)
	}

	// Create pool with available addresses and store in plan
	plan.SpyreCardPool = &types.SpyreCardPool{
		Addresses: pciAddresses,
	}

	return nil
}

// getRequiredSpyreCardsForComponent calculates Spyre cards needed for a component.
func (p *DeploymentPlanner) getRequiredSpyreCardsForComponent(comp *ComponentPlan) (int, error) {
	// Load component templates using catalog provider
	tmpls, err := p.catalogProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return 0, fmt.Errorf("failed to load component templates: %w", err)
	}

	totalSpyreCards := 0

	for templateName, tmpl := range tmpls {
		// Prepare minimal params for rendering
		params := map[string]any{
			"InstanceSlug": "slug", // dummy
			"TemplateID":   "test", // dummy
			"Values":       comp.Params,
			"env":          map[string]map[string]string{},
		}

		// Render template
		var rendered bytes.Buffer
		if err := tmpl.Execute(&rendered, params); err != nil {
			continue
		}

		// Parse rendered YAML to get pod spec
		var podSpec podmodels.PodSpec
		if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
			continue
		}

		// Extract Spyre card requirements from annotations
		spyreCards, _, err := fetchSpyreCardsFromPodAnnotations(podSpec.Annotations)
		if err != nil {
			return 0, err
		}

		totalSpyreCards += spyreCards
		if spyreCards > 0 {
			logger.Infof("Template %s requires %d Spyre cards\n", templateName, spyreCards)
		}
	}

	return totalSpyreCards, nil
}

// fetchSpyreCardsFromPodAnnotations extracts Spyre card requirements from pod annotations.
func fetchSpyreCardsFromPodAnnotations(annotations map[string]string) (int, map[string]int, error) {
	var spyreCards int
	spyreCardContainerMap := map[string]int{}

	spyreCardAnnotationRegex := regexp.MustCompile(`^ai-services\.io\/([A-Za-z0-9][-A-Za-z0-9_.]*)--spyre-cards$`)

	isSpyreCardAnnotation := func(annotation string) (string, bool) {
		matches := spyreCardAnnotationRegex.FindStringSubmatch(annotation)
		if matches == nil {
			return "", false
		}

		return matches[1], true
	}

	for annotationKey, val := range annotations {
		if containerName, ok := isSpyreCardAnnotation(annotationKey); ok {
			valInt, err := strconv.Atoi(val)
			if err != nil {
				return 0, spyreCardContainerMap, fmt.Errorf("failed to convert to int. Provided val: %s is not of int type", val)
			}
			spyreCardContainerMap[containerName] = valInt
			spyreCards += valInt
		}
	}

	return spyreCards, spyreCardContainerMap, nil
}

// Made with Bob
