package deployment

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/params"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/profiles"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// DeploymentPlanner plans the deployment of applications by:
// 1. Collecting parameters for each service and component
// 2. Deduplicating components (same type + provider + params = single deployment)
// 3. Creating deployment plan with shared components.
type DeploymentPlanner struct {
	catalogProvider *catalog.CatalogProvider
	paramBuilder    *params.ParamBuilder
	runtimeType     string
	// workers resolves the runtime of the target worker.
	workers worker.Resolver
	// models plans the model components (llm, embedding, reranker, ...) of an application. It may
	// be nil, in which case model components are deployed like any other component.
	models types.ModelManager
}

// NewDeploymentPlanner creates a new deployment planner.
func NewDeploymentPlanner(
	provider *catalog.CatalogProvider,
	workers worker.Resolver,
	models types.ModelManager,
) *DeploymentPlanner {
	return &DeploymentPlanner{
		catalogProvider: provider,
		paramBuilder:    params.NewParamBuilder(provider),
		workers:         workers,
		models:          models,
	}
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
	ModelManager   = types.ModelManager
)

// resolveIsArchitecture reports whether the given catalogID refers to an
// architecture. It returns false (and nil error) when it is a standalone
// service, and a non-nil error when the catalogID is not found in either form.
func (p *DeploymentPlanner) resolveIsArchitecture(catalogID string) (bool, error) {
	if _, err := p.catalogProvider.LoadArchitecture(catalogID); err == nil {
		return true, nil
	}

	if _, err := p.catalogProvider.LoadService(catalogID); err != nil {
		return false, fmt.Errorf("catalog_id '%s' not found as architecture or service", catalogID)
	}

	return false, nil
}

// PlanDeployment creates a deployment plan for an application (architecture or standalone service).
func (p *DeploymentPlanner) PlanDeployment(
	ctx context.Context,
	req apimodels.CreateApplicationRequest,
) (*DeploymentPlan, error) {
	if p.runtimeType == "" {
		return nil, fmt.Errorf("planner has no runtime type: call WithRuntime before PlanDeployment")
	}

	workerName := worker.DefaultName(req.WorkerName)

	isArchitecture, err := p.resolveIsArchitecture(req.CatalogID)
	if err != nil {
		return nil, err
	}

	// Create deployment plan
	appID := uuid.New()
	plan := &DeploymentPlan{
		ApplicationID:   appID,
		ApplicationName: req.Name,
		CreatedBy:       req.CreatedBy,
		Namespace:       utils.AppNamespace(appID),
		CatalogID:       req.CatalogID,
		Version:         req.Version,
		IsArchitecture:  isArchitecture,
		Components:      make(map[string]*ComponentPlan),
		Services:        make(map[string]*ServicePlan),
		WorkerName:      workerName,
		RuntimeType:     p.runtimeType,
	}

	// Process each service from request
	for _, svc := range req.Services {
		if err := p.processService(ctx, svc, plan); err != nil {
			return nil, fmt.Errorf("failed to process service '%s': %w", svc.CatalogID, err)
		}
	}

	// The models deploy concurrently, so they must fit on the worker together, not one by one.
	if err := p.checkModels(ctx, plan); err != nil {
		return nil, err
	}

	return plan, nil
}

// resolvedComponent is a component of the request together with what planning learned about it.
// For a component bound to an existing model instance, req is the request entry completed from the
// instance (provider, version and model), so the steps that follow need no special case for it.
type resolvedComponent struct {
	req       apimodels.Component
	instance  *dbmodels.Component // the existing model instance, nil for a new deployment
	connector *dbmodels.Connector // the remote model connector the service uses, nil for a component
}

// remoteModelParamKeys are the params stored in the DB for a remote model that a service reads, in
// place of the values.yaml a remote model does not have: the context length and the batch size.
var remoteModelParamKeys = []string{
	catalogconstants.ParamMaxModelLen,
	catalogconstants.ParamMaxBatchSize,
}

// processService processes a single service from the request.
//
//nolint:cyclop,funlen // one sequence of resolution steps, clearer kept together
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

	// Complete the components bound to an existing model first: the param builder and the
	// component plans below then see the provider, version and model like for any other component.
	resolved := make([]resolvedComponent, 0, len(svc.Components))
	resolvedSvc := svc
	resolvedSvc.Components = make([]apimodels.Component, 0, len(svc.Components))

	for _, comp := range svc.Components {
		rc, err := p.resolveComponent(ctx, comp, plan)
		if err != nil {
			return fmt.Errorf("failed to resolve component '%s': %w", comp.ComponentType, err)
		}

		// The profile is known before the values are built: the one the service recommends for a
		// new model, the one a reused instance runs with.
		rc.req.Profile, err = p.modelProfile(svc.CatalogID, rc)
		if err != nil {
			return fmt.Errorf("failed to resolve component '%s': %w", comp.ComponentType, err)
		}

		resolved = append(resolved, rc)
		resolvedSvc.Components = append(resolvedSvc.Components, rc.req)
	}

	// A remote model the service uses is planned like a model instance that already runs.
	connected, err := p.resolveConnectors(ctx, svc, plan)
	if err != nil {
		return err
	}

	// Process each component in the service
	for _, rc := range append(slices.Clone(resolved), connected...) {
		componentHash, err := p.processComponent(ctx, rc, svc.CatalogID, plan)
		if err != nil {
			return fmt.Errorf("failed to process component '%s': %w", rc.req.ComponentType, err)
		}

		// Add component reference to service
		servicePlan.ComponentRefs = append(servicePlan.ComponentRefs, componentHash)
	}

	if err := checkModelSources(svc.CatalogID, servicePlan, plan); err != nil {
		return err
	}

	// Load values using ParamBuilder
	serviceParams, err := p.paramBuilder.BuildServiceParams(ctx, resolvedSvc, nil)
	if err != nil {
		return fmt.Errorf("failed to build service params: %w", err)
	}

	// Use values from ParamBuilder (already includes component values nested under component_type)
	servicePlan.Values = serviceParams.Values

	// A remote model has no component values to load: its model and the params stored in the DB
	// stand in for them.
	for _, rc := range connected {
		serviceParams.Values[rc.req.ComponentType] = remoteModelValues(rc.req, rc.connector)
	}

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

// resolveConnectors resolves the remote model connectors of a service, each into the model
// component it supplies. The component type is the connector's own (metadata component_type) and
// must be one the service depends on. A component type is supplied by either components[] or one
// connector, never both.
func (p *DeploymentPlanner) resolveConnectors(
	ctx context.Context,
	svc apimodels.Service,
	plan *DeploymentPlan,
) ([]resolvedComponent, error) {
	var out []resolvedComponent

	roles := make(map[string]bool)

	for _, ref := range svc.Connectors {
		if ref.Type != catalogconstants.ConnectorTypeModels {
			continue
		}

		if p.models == nil {
			return nil, validators.BadRequest("model connectors are not supported: model management is not available")
		}

		id, err := uuid.Parse(ref.ID)
		if err != nil {
			return nil, validators.BadRequest("connector ref ID %q is not a valid UUID", ref.ID)
		}

		conn, err := p.models.GetConnectedModel(ctx, id, plan.CreatedBy)
		if err != nil {
			return nil, err
		}

		role, _ := conn.Metadata[catalogconstants.MetadataKeyComponentType].(string)
		if err := p.validateConnectorRole(svc, conn.ID, role, roles); err != nil {
			return nil, err
		}

		roles[role] = true
		model, _ := conn.Metadata[catalogconstants.MetadataKeyModel].(string)

		// The connector is to a remote model what the instance is to a local one: it identifies the
		// deployment, so two connectors of the same provider and model stay two components.
		out = append(out, resolvedComponent{
			req: apimodels.Component{
				ComponentType: role,
				ProviderID:    conn.Provider,
				Model:         model,
				InstanceID:    conn.ID.String(),
			},
			connector: conn,
		})
	}

	return out, nil
}

// validateConnectorRole checks the component type role of the model connector id for the service:
// the service must depend on it, and neither its components nor another connector (roles) may
// supply it.
func (p *DeploymentPlanner) validateConnectorRole(svc apimodels.Service, id uuid.UUID, role string, roles map[string]bool) error {
	catalogSvc, err := p.catalogProvider.LoadService(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service '%s': %w", svc.CatalogID, err)
	}

	if !slices.ContainsFunc(catalogSvc.Dependencies, func(d catalogtypes.DependencyReference) bool { return d.ID == role }) {
		return validators.BadRequest("model connector %q is a %q, which service %q does not use", id, role, svc.CatalogID)
	}

	// Any component of the role conflicts, whether it deploys a new model or binds an existing
	// instance (instance_id).
	if slices.ContainsFunc(svc.Components, func(c apimodels.Component) bool { return c.ComponentType == role }) {
		return validators.BadRequest(
			"service %q sets its %q both as a component (new model or instance_id) and as a model connector; use one",
			svc.CatalogID, role)
	}

	if roles[role] {
		return validators.BadRequest("service %q has more than one model connector for %q", svc.CatalogID, role)
	}

	return nil
}

// checkModelSources checks that a component type of the service is supplied by one source only:
// either a pre-deployed instance (instance_id) or a remote model connector, never both. It is
// checked on the planned components, so it holds whatever the way the service's components were
// resolved.
func checkModelSources(serviceID string, svc *ServicePlan, plan *DeploymentPlan) error {
	preDeployed := make(map[string]bool)
	remote := make(map[string]bool)

	for _, hash := range svc.ComponentRefs {
		comp := plan.Components[hash]

		switch {
		case comp.Remote:
			if preDeployed[comp.ComponentType] {
				return validators.BadRequest("service %q sets its %q both as a model instance and as a model connector", serviceID, comp.ComponentType)
			}

			remote[comp.ComponentType] = true
		case comp.PreDeployed:
			if remote[comp.ComponentType] {
				return validators.BadRequest("service %q sets its %q both as a model instance and as a model connector", serviceID, comp.ComponentType)
			}

			preDeployed[comp.ComponentType] = true
		}
	}

	return nil
}

// remoteModelValues are the values of a remote model component of a service: its model and the
// maxModelLen and maxBatchSize stored in the DB params of its connector. What the model service
// returns (host, key, ...) is merged later.
func remoteModelValues(comp apimodels.Component, conn *dbmodels.Connector) map[string]any {
	values := map[string]any{catalogconstants.ParamKeyModel: comp.Model}

	if conn == nil {
		return values
	}

	stored, _ := conn.Metadata[catalogconstants.MetadataKeyParams].(map[string]any)

	for _, key := range remoteModelParamKeys {
		v, ok := stored[key]
		if !ok {
			continue
		}

		// Metadata read back from the DB holds JSON numbers (float64); services expect whole numbers.
		if f, isFloat := v.(float64); isFloat {
			v = int(f)
		}

		values[key] = v
	}

	return values
}

// resolveComponent completes a component bound to an existing model instance (instance_id) from
// that instance. Any other component is returned as is. Only a component of a model type (catalog
// category "models") can be bound to an instance.
func (p *DeploymentPlanner) resolveComponent(
	ctx context.Context,
	comp apimodels.Component,
	plan *DeploymentPlan,
) (resolvedComponent, error) {
	if comp.InstanceID == "" {
		return resolvedComponent{req: comp}, nil
	}

	if !p.catalogProvider.IsModelComponentType(comp.ComponentType) {
		return resolvedComponent{}, validators.BadRequest(
			"component '%s' is not a model component; instance_id does not apply", comp.ComponentType)
	}

	// Which runtimes can host models is the model service's call, not the planner's: it is asked
	// on every runtime and rejects the ones that are unsupported.
	if p.models == nil {
		return resolvedComponent{}, validators.BadRequest("instance_id is not supported: model management is not available")
	}

	id, err := uuid.Parse(comp.InstanceID)
	if err != nil {
		return resolvedComponent{}, validators.BadRequest("instance_id %q is not a valid UUID", comp.InstanceID)
	}

	instance, err := p.models.GetRunningInstance(ctx, id, comp.ComponentType, plan.CreatedBy)
	if err != nil {
		return resolvedComponent{}, err
	}

	// TODO(litellm): the services reach the model through the gateway, which LiteLLM routes to any
	// worker, but a service on a remote worker cannot reach the gateway yet (see
	// litellm.GatewayAddress). Until it can, the model must be on the application's worker.
	if workerID, ok := p.workers.ID(plan.WorkerName); ok && (instance.WorkerID == nil || *instance.WorkerID != workerID) {
		return resolvedComponent{}, validators.BadRequest(
			"model instance %q does not run on worker %q; an application can only use models on its own worker", id, plan.WorkerName)
	}

	model, _ := instance.Metadata[catalogconstants.MetadataKeyModel].(string)

	comp.ProviderID = instance.Provider
	comp.Version = instance.Version
	comp.Model = model

	return resolvedComponent{req: comp, instance: instance}, nil
}

// processComponent processes a single component from the request and returns its hash.
// If the same component configuration already exists, it reuses it.
func (p *DeploymentPlanner) processComponent(
	ctx context.Context,
	rc resolvedComponent,
	catalogID string,
	plan *DeploymentPlan,
) (string, error) {
	comp := rc.req

	profile, err := p.modelProfile(catalogID, rc)
	if err != nil {
		return "", err
	}

	// Calculate component hash based on type + provider + params (+ model, profile and instance)
	// This allows deduplication: same config = same deployment
	componentHash := utils.CalculateComponentHash(
		comp.ComponentType,
		comp.ProviderID,
		identityWithProfile(comp.IdentityParams(), profile),
	)

	// Check if this component configuration already exists in the plan
	if existingComp, exists := plan.Components[componentHash]; exists {
		// Component already planned, just add this service to its users
		existingComp.UsedByServices = append(existingComp.UsedByServices, catalogID)

		return componentHash, nil
	}

	// Get component path from catalog provider. A remote model is not a catalog component: nothing
	// is deployed for it, so it has no path.
	var catalogPath string

	if rc.connector == nil {
		componentKey := fmt.Sprintf("%s/%s", comp.ComponentType, comp.ProviderID)

		componentPath, err := p.catalogProvider.GetCatalogItemPath(componentKey)
		if err != nil {
			return "", fmt.Errorf("failed to get component catalog path: %w", err)
		}

		catalogPath = path.Join(componentPath, plan.RuntimeType)
	}

	// Create new component plan
	compPlan := &ComponentPlan{
		Hash:           componentHash,
		ComponentType:  comp.ComponentType,
		ProviderID:     comp.ProviderID,
		CatalogPath:    catalogPath,
		Version:        comp.Version,
		Params:         comp.ValueParams(),
		UsedByServices: []string{catalogID},
	}

	if err := p.planModel(ctx, rc, profile, compPlan, plan); err != nil {
		return "", err
	}

	// Add to plan
	plan.Components[componentHash] = compPlan

	return componentHash, nil
}

// planModel is the plan phase of a model component, delegated to the model service on every
// runtime: an existing instance is used as is, a new one is validated and planned exactly like
// POST /api/v1/models. The model service rejects the runtimes that cannot host models, so the
// application is refused before anything is recorded. A component that is not a model is left alone.
func (p *DeploymentPlanner) planModel(
	ctx context.Context,
	rc resolvedComponent,
	profile string,
	compPlan *ComponentPlan,
	plan *DeploymentPlan,
) error {
	// A remote model is not inserted or deployed either: it was registered with the gateway when
	// its connector was created. Services link to it as a connector, not as a component.
	if rc.connector != nil {
		compPlan.PreDeployed = true
		compPlan.Remote = true
		compPlan.DatabaseID = rc.connector.ID

		return nil
	}

	// An existing instance is not inserted or deployed: the services using it reach it through the
	// gateway.
	if rc.instance != nil {
		compPlan.PreDeployed = true
		compPlan.DatabaseID = rc.instance.ID

		return nil
	}

	if p.models == nil || !p.catalogProvider.IsModelComponentType(compPlan.ComponentType) {
		return nil
	}

	comp := rc.req

	modelPlan, err := p.models.PlanLocalModel(ctx, apimodels.CreateModelRequest{
		Name:           plan.ApplicationName,
		DeploymentType: catalogconstants.DeploymentTypeLocal,
		ComponentType:  comp.ComponentType,
		ProviderID:     comp.ProviderID,
		Model:          comp.Model,
		Profile:        profile,
		WorkerName:     plan.WorkerName,
		Params:         comp.Params,
		CreatedBy:      plan.CreatedBy,
	})
	if err != nil {
		return err
	}

	compPlan.Model = modelPlan
	compPlan.DatabaseID = modelPlan.ComponentID

	return nil
}

// modelProfile returns the hardware profile of a local model component, "" for any other component.
// The client does not send one:
//   - an existing instance runs with the profile stored on its row;
//   - a new model is deployed with the profile the service recommends for it
//     (models[].providers[].recommendations in services/<id>/metadata.yaml), else the model's
//     default profile. Recommendations only guide the profile; they never restrict the models.
//
// A model the catalog does not list resolves to "": the model service rejects it with a 400.
func (p *DeploymentPlanner) modelProfile(catalogID string, rc resolvedComponent) (string, error) {
	comp := rc.req

	if comp.Profile != "" {
		return comp.Profile, nil
	}

	if rc.instance != nil {
		profile, _ := rc.instance.Metadata[catalogconstants.MetadataKeyProfile].(string)

		return profile, nil
	}

	// A remote model has no hardware profile.
	if rc.connector != nil || comp.Model == "" || !p.catalogProvider.IsModelComponentType(comp.ComponentType) {
		return "", nil
	}

	service, err := p.catalogProvider.LoadService(catalogID)
	if err != nil {
		return "", fmt.Errorf("failed to load service '%s' to resolve the model profile: %w", catalogID, err)
	}

	recommended := service.RecommendedProfile(comp.ComponentType, comp.ProviderID, comp.Model)

	profile, err := p.catalogProvider.ResolveModelProfile(comp.ComponentType, comp.ProviderID, comp.Model, recommended)

	switch {
	case errors.Is(err, catalog.ErrModelNotSupported):
		return "", nil
	case errors.Is(err, profiles.ErrNotSupported):
		return "", validators.BadRequest("service %q recommends an unusable profile for model %q: %v", catalogID, comp.Model, err)
	case err != nil:
		return "", fmt.Errorf("failed to resolve the profile of model '%s': %w", comp.Model, err)
	}

	return profile, nil
}

// identityWithProfile adds the resolved profile to the identity of a component: the same model
// with another profile runs on other hardware, so it is another deployment. It returns identity
// itself when there is no profile.
func identityWithProfile(identity map[string]any, profile string) map[string]any {
	if profile == "" {
		return identity
	}

	out := make(map[string]any, len(identity)+1)
	maps.Copy(out, identity)
	out[catalogconstants.ParamKeyProfile] = profile

	return out
}

// checkModels hands the models the application deploys, already planned so with their provider and
// profile, to the model service, which decides whether the worker can host them together. What
// they need (accelerator cards, whatever the vendor) and what the worker has is its call, not the
// planner's. An application whose models cannot be hosted is rejected before anything is recorded
// or started. It does nothing when the application deploys no model.
func (p *DeploymentPlanner) checkModels(ctx context.Context, plan *DeploymentPlan) error {
	if p.models == nil {
		return nil
	}

	var plans []*modeltypes.LocalPlan

	for _, comp := range plan.ModelComponents() {
		plans = append(plans, comp.Model)
	}

	return p.models.CheckLocalModels(ctx, plan.WorkerName, plans)
}

// Made with Bob
