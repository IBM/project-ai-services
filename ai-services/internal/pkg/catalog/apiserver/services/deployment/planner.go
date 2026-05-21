package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/params"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// DeploymentPlanner plans the deployment of applications by:
// 1. Parsing and validating the request
// 2. Collecting parameters for each service and component
// 3. Deduplicating components (same type + provider + params = single deployment)
// 4. Creating deployment plan with shared components
type DeploymentPlanner struct {
	catalogProvider *catalog.CatalogProvider
	componentRepo   repository.ComponentRepository
	requestParser   *RequestParser
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
		requestParser:   NewRequestParser(componentRepo),
		paramBuilder:    params.NewParamBuilder(provider),
	}
}

// Type aliases for deployment plan types
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

	// Parse the request
	parsedReq, err := p.requestParser.ParseRequest(ctx, req, isArchitecture)
	if err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	// Create deployment plan
	plan := &DeploymentPlan{
		ApplicationID:   uuid.New(),
		ApplicationName: parsedReq.ApplicationName,
		CatalogID:       parsedReq.CatalogID,
		IsArchitecture:  parsedReq.IsArchitecture,
		Components:      make(map[string]*ComponentPlan),
		Services:        make(map[string]*ServicePlan),
	}

	// Process each service from parsed request
	for serviceID, parsedSvc := range parsedReq.Services {
		if err := p.processServiceFromParsed(ctx, parsedSvc, plan, runtimeType); err != nil {
			return nil, fmt.Errorf("failed to process service '%s': %w", serviceID, err)
		}
	}

	return plan, nil
}

// processServiceFromParsed processes a single service from parsed request.
func (p *DeploymentPlanner) processServiceFromParsed(
	ctx context.Context,
	parsedSvc *ParsedService,
	plan *DeploymentPlan,
	runtimeType string,
) error {
	servicePlan := &ServicePlan{
		CatalogID:     parsedSvc.CatalogID,
		CatalogPath:   fmt.Sprintf("services/%s/%s", parsedSvc.CatalogID, runtimeType),
		Version:       parsedSvc.Version,
		ComponentRefs: make([]string, 0),
		ArgParams:     make(map[string]string),
	}

	// Process each component in the service
	for _, parsedComp := range parsedSvc.Components {
		componentHash, err := p.processComponentFromParsed(parsedComp, parsedSvc.CatalogID, plan, runtimeType)
		if err != nil {
			return fmt.Errorf("failed to process component '%s': %w", parsedComp.ComponentType, err)
		}

		// Add component reference to service
		servicePlan.ComponentRefs = append(servicePlan.ComponentRefs, componentHash)

		// Merge component argParams into service argParams
		compPlan := plan.Components[componentHash]
		maps.Copy(servicePlan.ArgParams, compPlan.ArgParams)
	}

	// Load values using ParamBuilder
	// Convert ParsedService to apimodels.Service for ParamBuilder
	svcReq := apimodels.Service{
		CatalogID:  parsedSvc.CatalogID,
		Version:    parsedSvc.Version,
		Components: make([]apimodels.Component, 0, len(parsedSvc.Components)),
	}
	for _, comp := range parsedSvc.Components {
		svcReq.Components = append(svcReq.Components, apimodels.Component{
			ComponentType: comp.ComponentType,
			ProviderID:    comp.ProviderID,
			Params:        comp.Params,
		})
	}

	serviceParams, err := p.paramBuilder.BuildServiceParams(ctx, svcReq, nil)
	if err != nil {
		return fmt.Errorf("failed to build service params: %w", err)
	}

	// Use values from ParamBuilder (already includes component values nested under component_type)
	servicePlan.Values = serviceParams.Values

	// Extract component values from serviceParams.Values and populate ComponentPlan.Values
	for _, compHash := range servicePlan.ComponentRefs {
		compPlan := plan.Components[compHash]
		// Component values are nested under component_type in serviceParams.Values
		if compValues, ok := serviceParams.Values[compPlan.ComponentType].(map[string]interface{}); ok {
			compPlan.Values = compValues
		}
	}

	// Add service to plan
	plan.Services[parsedSvc.CatalogID] = servicePlan

	return nil
}

// processComponentFromParsed processes a single component from parsed request and returns its hash.
// If the same component configuration already exists, it reuses it.
func (p *DeploymentPlanner) processComponentFromParsed(
	parsedComp *ParsedComponent,
	catalogID string,
	plan *DeploymentPlan,
	runtimeType string,
) (string, error) {
	// Calculate component hash based on type + provider + params
	// This allows deduplication: same config = same deployment
	componentHash := p.calculateComponentHash(
		parsedComp.ComponentType,
		parsedComp.ProviderID,
		parsedComp.Params,
	)

	// Check if this component configuration already exists in the plan
	if existingComp, exists := plan.Components[componentHash]; exists {
		// Component already planned, just add this service to its users
		existingComp.UsedByServices = append(existingComp.UsedByServices, catalogID)
		return componentHash, nil
	}

	// Create new component plan
	compPlan := &ComponentPlan{
		Hash:           componentHash,
		ComponentType:  parsedComp.ComponentType,
		ProviderID:     parsedComp.ProviderID,
		CatalogPath:    fmt.Sprintf("components/%s/%s/%s", parsedComp.ComponentType, parsedComp.ProviderID, runtimeType),
		Params:         parsedComp.Params,
		ArgParams:      utils.FlattenMapWithValues(parsedComp.Params, ""),
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
	params map[string]interface{},
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

// Made with Bob
