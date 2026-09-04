package deployment

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/params"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// DeploymentPlanner plans the deployment of applications by:
// 1. Collecting parameters for each service and component
// 2. Deduplicating components (same type + provider + params = single deployment)
// 3. Creating deployment plan with shared components.
type DeploymentPlanner struct {
	catalogProvider   *catalog.CatalogProvider
	componentRepo     repository.ComponentRepository
	paramBuilder      *params.ParamBuilder
	serverRuntimeType string
	runtimeType       string
	// workerRegistry is optional; when set, PlanDeployment validates remote
	// worker metadata (e.g. Caddy config) before any DB records are written.
	workerRegistry stream.WorkerRegistry
}

// NewDeploymentPlanner creates a new deployment planner.
// serverRuntimeType is the runtime the server itself is configured with (e.g.
// "podman" or "openshift") and is used as the default when no worker overrides it.
func NewDeploymentPlanner(
	provider *catalog.CatalogProvider,
	componentRepo repository.ComponentRepository,
	serverRuntimeType string,
) *DeploymentPlanner {
	return &DeploymentPlanner{
		catalogProvider:   provider,
		componentRepo:     componentRepo,
		paramBuilder:      params.NewParamBuilder(provider),
		serverRuntimeType: serverRuntimeType,
	}
}

// WithWorkerRegistry wires the worker registry into the planner so it can
// validate remote worker metadata during PlanDeployment and fail early before
// any DB records are written.
func (p *DeploymentPlanner) WithWorkerRegistry(reg stream.WorkerRegistry) *DeploymentPlanner {
	p.workerRegistry = reg

	return p
}

// WithRuntime returns a runtime-scoped planner copy.
func (p *DeploymentPlanner) WithRuntime(runtimeType string) (*DeploymentPlanner, error) {
	if !runtimeTypes.RuntimeType(runtimeType).Valid() {
		return nil, fmt.Errorf("invalid runtime type: %q", runtimeType)
	}

	scopedProvider, err := p.catalogProvider.WithRuntime(runtimeType)
	if err != nil {
		return nil, fmt.Errorf("failed to scope catalog provider for runtime %q: %w", runtimeType, err)
	}

	scopedParamBuilder, err := p.paramBuilder.WithRuntime(runtimeType)
	if err != nil {
		return nil, fmt.Errorf("failed to scope param builder for runtime %q: %w", runtimeType, err)
	}

	cp := *p
	cp.catalogProvider = scopedProvider
	cp.paramBuilder = scopedParamBuilder
	cp.runtimeType = runtimeType

	return &cp, nil
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
) (*DeploymentPlan, error) {
	runtimeType := p.runtimeType
	if runtimeType == "" {
		// TODO: Remove this fallback once all callers provide a runtime-scoped planner.
		var err error
		runtimeType, err = p.ResolveRuntimeType(ctx, req.WorkerName)
		if err != nil {
			return nil, err
		}
	}

	workerName := req.WorkerName
	if workerName == "" {
		workerName = workerconstants.LocalWorkerName
	}

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
		Version:         req.Version,
		IsArchitecture:  isArchitecture,
		Components:      make(map[string]*ComponentPlan),
		Services:        make(map[string]*ServicePlan),
		WorkerName:      workerName,
		RuntimeType:     runtimeType,
	}

	// Process each service from request
	for _, svc := range req.Services {
		if err := p.processService(ctx, svc, plan); err != nil {
			return nil, fmt.Errorf("failed to process service '%s': %w", svc.CatalogID, err)
		}
	}

	// Calculate and allocate Spyre cards only for local Podman deployments.
	// Remote-worker deployments must not probe local /dev/vfio on the API server.
	if runtimeType == runtimeTypes.RuntimeTypePodman.String() && isLocalWorkerName(workerName) {
		if err := p.calculateAndAllocateSpyreCards(ctx, plan); err != nil {
			return nil, fmt.Errorf("failed to allocate Spyre cards: %w", err)
		}
	}

	return plan, nil
}

// processService processes a single service from the request.
func (p *DeploymentPlanner) processService(
	ctx context.Context,
	svc apimodels.Service,
	plan *DeploymentPlan,
) error {
	// Get service path from catalog provider
	servicePath, err := p.catalogProvider.GetCatalogItemPath(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to get service catalog path: %w", err)
	}

	servicePlan := &ServicePlan{
		CatalogID:     svc.CatalogID,
		CatalogPath:   path.Join(servicePath, plan.RuntimeType),
		Version:       svc.Version,
		ComponentRefs: make([]string, 0),
	}

	// Process each component in the service
	for _, comp := range svc.Components {
		componentHash, err := p.processComponent(comp, svc.CatalogID, plan)
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
) (string, error) {
	// Calculate component hash based on type + provider + params
	// This allows deduplication: same config = same deployment
	componentHash := utils.CalculateComponentHash(
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
		CatalogPath:    path.Join(componentPath, plan.RuntimeType),
		Version:        comp.Version,
		Params:         comp.Params,
		UsedByServices: []string{catalogID},
	}

	// Add to plan
	plan.Components[componentHash] = compPlan

	return componentHash, nil
}

// calculateAndAllocateSpyreCards calculates required Spyre cards and creates allocation pool.
func (p *DeploymentPlanner) calculateAndAllocateSpyreCards(ctx context.Context, plan *DeploymentPlan) error {
	totalRequired := 0

	// Calculate total required Spyre cards from all components
	for _, comp := range plan.Components {
		required, err := p.getRequiredSpyreCardsForComponent(ctx, comp, plan)
		if err != nil {
			return fmt.Errorf("failed to get Spyre card requirements for component %s: %w", comp.ComponentType, err)
		}
		totalRequired += required
		if required > 0 {
			logger.InfofCtx(ctx, "Component %s/%s requires %d Spyre cards\n", comp.ComponentType, comp.ProviderID, required)
		}
	}

	if totalRequired == 0 {
		logger.InfofCtx(ctx, "No Spyre cards required for this deployment\n")

		return nil
	}

	logger.InfofCtx(ctx, "Total Spyre cards required: %d\n", totalRequired)

	// Find available Spyre cards
	pciAddresses, err := helpers.FindFreeSpyreCards(ctx)
	if err != nil {
		return fmt.Errorf("failed to find free Spyre cards: %w", err)
	}

	availableCount := len(pciAddresses)
	logger.InfofCtx(ctx, "Available Spyre cards: %d\n", availableCount)

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
func (p *DeploymentPlanner) getRequiredSpyreCardsForComponent(ctx context.Context, comp *ComponentPlan, plan *DeploymentPlan) (int, error) {
	scopedProvider, err := p.catalogProvider.WithRuntime(plan.RuntimeType)
	if err != nil {
		return 0, fmt.Errorf("failed to scope catalog provider for runtime %q: %w", plan.RuntimeType, err)
	}

	// Load component templates using catalog provider
	tmpls, err := scopedProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return 0, fmt.Errorf("failed to load component templates: %w", err)
	}

	// Use the catalog provider's CollectSpyreCardsFromTemplates function
	// Use comp.Values instead of comp.Params to include defaults from values.yaml
	totalSpyreCards, err := scopedProvider.CollectSpyreCardsFromTemplates(ctx, tmpls, comp.Values)
	if err != nil {
		return 0, fmt.Errorf("failed to collect Spyre cards from templates: %w", err)
	}

	return totalSpyreCards, nil
}

// WorkerDBID returns the database UUID for the named worker by consulting the
// in-memory registry. Returns (uuid.Nil, false) when the worker is not
// connected or the registry is nil (local-only server).
func (p *DeploymentPlanner) WorkerDBID(workerName string) (uuid.UUID, bool) {
	if isLocalWorkerName(workerName) {
		return uuid.Nil, false
	}

	if p.workerRegistry == nil {
		return uuid.Nil, false
	}

	return p.workerRegistry.WorkerID(workerName)
}

// ValidateWorker confirms the named remote worker is connected. Called from
// PlanDeployment before any DB records are written so the Create API can
// return an error immediately on failure.
func (p *DeploymentPlanner) ValidateWorker(ctx context.Context, workerName string) error {
	if isLocalWorkerName(workerName) {
		return nil
	}

	if p.workerRegistry == nil {
		return fmt.Errorf("worker deployment is not configured on this server")
	}

	// TODO: Remove this when remote deployment is by default
	if workerName == "" {
		return nil
	}

	if !p.workerRegistry.IsWorkerConnected(ctx, workerName) {
		return fmt.Errorf("worker %q is not connected", workerName)
	}

	return nil
}

// ResolveRuntimeType returns the effective runtime for a create-application
// request: worker runtime when workerName is set, otherwise server runtime.
func (p *DeploymentPlanner) ResolveRuntimeType(ctx context.Context, workerName string) (string, error) {
	if isLocalWorkerName(workerName) {
		return p.serverRuntimeType, nil
	}

	if err := p.ValidateWorker(ctx, workerName); err != nil {
		return "", err
	}

	workerRT, ok := p.workerRegistry.WorkerRuntimeType(workerName)
	if !ok || workerRT == "" {
		return "", fmt.Errorf("worker %q runtime type not available", workerName)
	}
	if !runtimeTypes.RuntimeType(workerRT).Valid() {
		return "", fmt.Errorf("worker %q has unsupported runtime type %q", workerName, workerRT)
	}

	return workerRT, nil
}

func isLocalWorkerName(workerName string) bool {
	return workerName == "" || strings.EqualFold(workerName, workerconstants.LocalWorkerName)
}

// Made with Bob
