package podman

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"text/template"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/podman"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	remoteruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	k8syaml "sigs.k8s.io/yaml"
)

// ComponentInfo holds the information derived from a deployed component.
type ComponentInfo = podmanutils.EndpointInfo

// Type aliases for deployment plan types.
type (
	DeploymentPlan = deploymenttypes.DeploymentPlan
	ComponentPlan  = deploymenttypes.ComponentPlan
	ServicePlan    = deploymenttypes.ServicePlan
	SpyreCardPool  = deploymenttypes.SpyreCardPool
)

// PodmanDeployer implements deployment execution for Podman runtime.
type PodmanDeployer struct {
	runtime         runtime.Runtime
	runtimeType     string
	baseDir         string
	catalogProvider *catalog.CatalogProvider
	appRepo         repository.ApplicationRepository
	serviceRepo     repository.ServiceRepository
	componentRepo   repository.ComponentRepository
}

// NewPodmanDeployer creates a new PodmanDeployer instance.
func NewPodmanDeployer(
	rt runtime.Runtime,
	catalogProvider *catalog.CatalogProvider,
	appRepo repository.ApplicationRepository,
	serviceRepo repository.ServiceRepository,
	componentRepo repository.ComponentRepository,
) *PodmanDeployer {
	return &PodmanDeployer{
		runtime:         rt,
		runtimeType:     rt.Type().String(),
		baseDir:         "",
		catalogProvider: catalogProvider,
		appRepo:         appRepo,
		serviceRepo:     serviceRepo,
		componentRepo:   componentRepo,
	}
}

// ExecuteDeployment executes the deployment plan for an application or standalone service.
// This implements the deployment flow:
// 1. Pull container images for all components and services
// 2. Download models specified in component/service parameters
// 3. Calculate and allocate Spyre cards if needed
// 4. Deploy components
// 5. Deploy services
// 6. Update database with endpoints and final status
//
// Note: Application, service, and component records are already created by ApplicationService
// before this method is called. This method only updates endpoints and status.
func (d *PodmanDeployer) ExecuteDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
) error {
	logger.InfofCtx(ctx, "Starting deployment execution for '%s'\n", plan.ApplicationName)

	if err := d.prepareDeployment(ctx, plan); err != nil {
		return err
	}

	// Step 2: Deploy components if any
	if len(plan.Components) > 0 {
		if err := d.deployComponents(ctx, plan); err != nil {
			catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Component deployment failed", err)

			return fmt.Errorf("failed to deploy components: %w", err)
		}
	}

	// Step 3: Deploy services if any
	if len(plan.Services) > 0 {
		if err := d.deployServices(ctx, plan); err != nil {
			catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Service deployment failed", err)

			return fmt.Errorf("failed to deploy services: %w", err)
		}
	}

	// Step 4: Register routes with Caddy proxy
	if err := d.registerApplicationRoutes(ctx, plan); err != nil {
		catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Failed to register application routes", err)

		return fmt.Errorf("failed to register application routes: %w", err)
	}

	// Step 5: Update application status to Running.
	// Skip if the context was cancelled — deletion is now in charge of the status.
	if ctx.Err() == nil {
		if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, plan.ApplicationID, models.ApplicationStatusRunning, "Deployment completed successfully"); err != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Running: %v\n", err)
		}

		logger.InfofCtx(ctx, "Deployment completed successfully for '%s'\n", plan.ApplicationName)
	}

	return nil
}

// prepareDeployment pulls images, downloads models, and transitions the
// application status to Deploying. It is a prerequisite for all deploy steps.
func (d *PodmanDeployer) prepareDeployment(ctx context.Context, plan *DeploymentPlan) error {
	baseDir, err := podmanutils.FetchBaseDir(ctx, d.runtime)
	if err != nil {
		return err
	}
	d.baseDir = baseDir

	// Step 1a: Pull container images for all components and services
	if err := d.pullImagesForDeployment(ctx, plan); err != nil {
		catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Image pull failed", err)

		return fmt.Errorf("failed to pull images: %w", err)
	}

	// Step 1b: Download models specified in parameters
	if err := d.downloadModelsForDeployment(ctx, plan); err != nil {
		catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Model download failed", err)

		return fmt.Errorf("failed to download models: %w", err)
	}

	// Transition status to Deploying before pod creation begins.
	// Skip if the context was cancelled — deletion is now in charge of the status.
	if ctx.Err() == nil {
		if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, plan.ApplicationID, models.ApplicationStatusDeploying, catalogutils.DeployingStatusMessage(plan.IsArchitecture)); err != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Deploying: %v\n", err)
		}
	}

	return nil
}

// downloadModelsForDeployment downloads all models specified in component and service parameters.
// Models are extracted from params that contain "model" in their key name.
func (d *PodmanDeployer) downloadModelsForDeployment(ctx context.Context, plan *DeploymentPlan) error {
	logger.InfofCtx(ctx, "Downloading models for application '%s'\n", plan.ApplicationName)

	modelSet := d.collectModelsFromPlan(ctx, plan)

	if len(modelSet) == 0 {
		logger.InfofCtx(ctx, "No models to download for application '%s'\n", plan.ApplicationName)

		return nil
	}

	if err := d.downloadModels(ctx, modelSet); err != nil {
		return err
	}

	logger.InfofCtx(ctx, "Successfully downloaded all models for application '%s'\n", plan.ApplicationName)

	return nil
}

// collectModelsFromPlan collects all unique model names from the deployment plan.
func (d *PodmanDeployer) collectModelsFromPlan(ctx context.Context, plan *DeploymentPlan) map[string]bool {
	modelSet := make(map[string]bool)

	// Extract models from component params
	for _, comp := range plan.Components {
		// do not download models for watsonx
		if strings.EqualFold(comp.ProviderID, "watsonx") {
			logger.InfofCtx(ctx, "Skipping model download for provider: %s\n", comp.ProviderID)

			continue
		}
		d.extractModelsFromParams(comp.Params, modelSet)
	}

	return modelSet
}

// extractModelsFromParams extracts model names from parameter maps.
func (d *PodmanDeployer) extractModelsFromParams(params map[string]any, modelSet map[string]bool) {
	for key, value := range params {
		if strings.Contains(strings.ToLower(key), "model") {
			if modelName, ok := value.(string); ok && modelName != "" {
				modelSet[modelName] = true
			}
		}
	}
}

// downloadModels downloads all models in the provided set using the shared podmanutils helper.
func (d *PodmanDeployer) downloadModels(ctx context.Context, modelSet map[string]bool) error {
	models := make([]string, 0, len(modelSet))
	for modelName := range modelSet {
		models = append(models, modelName)
	}

	return podmanutils.DownloadModels(ctx, d.runtime, models)
}

// pullImagesForDeployment pulls all container images required for components and services.
func (d *PodmanDeployer) pullImagesForDeployment(ctx context.Context, plan *DeploymentPlan) error {
	logger.InfofCtx(ctx, "Pulling container images for application '%s'\n", plan.ApplicationName)

	imageSet, err := d.collectImagesFromPlan(ctx, plan)
	if err != nil {
		return fmt.Errorf("failed to collect images: %w", err)
	}

	if len(imageSet) == 0 {
		logger.InfofCtx(ctx, "No images to pull for application '%s'\n", plan.ApplicationName)

		return nil
	}

	if err := d.pullImages(ctx, imageSet); err != nil {
		return err
	}

	logger.InfofCtx(ctx, "Successfully pulled all images for application '%s'\n", plan.ApplicationName)

	return nil
}

// collectImagesFromPlan collects all unique container images from the deployment plan.
func (d *PodmanDeployer) collectImagesFromPlan(ctx context.Context, plan *DeploymentPlan) (map[string]bool, error) {
	imageSet := make(map[string]bool)

	// Include tool image which is used for all housekeeping tasks
	imageSet[vars.ToolImage] = true

	// Extract images from component templates
	for _, comp := range plan.Components {
		if err := d.extractImagesFromComponent(ctx, comp, imageSet); err != nil {
			return nil, err
		}
	}

	// Extract images from service templates
	for _, svc := range plan.Services {
		if err := d.extractImagesFromService(ctx, svc, imageSet); err != nil {
			return nil, err
		}
	}

	return imageSet, nil
}

// extractImagesFromComponent extracts container images from a component's templates.
func (d *PodmanDeployer) extractImagesFromComponent(ctx context.Context, comp *ComponentPlan, imageSet map[string]bool) error {
	scopedProvider, err := d.catalogProvider.WithRuntime(d.runtimeType)
	if err != nil {
		return fmt.Errorf("failed to scope catalog provider for runtime %q: %w", d.runtimeType, err)
	}

	// Load component templates
	templates, err := scopedProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to load component templates for %s/%s: %w", comp.ComponentType, comp.ProviderID, err)
	}

	// Extract images from templates with custom values directly into imageSet
	if err := scopedProvider.CollectImagesFromTemplates(ctx, templates, comp.Values, imageSet); err != nil {
		return fmt.Errorf("failed to extract images from component %s/%s: %w", comp.ComponentType, comp.ProviderID, err)
	}

	return nil
}

// extractImagesFromService extracts container images from a service's templates.
func (d *PodmanDeployer) extractImagesFromService(ctx context.Context, svc *ServicePlan, imageSet map[string]bool) error {
	scopedProvider, err := d.catalogProvider.WithRuntime(d.runtimeType)
	if err != nil {
		return fmt.Errorf("failed to scope catalog provider for runtime %q: %w", d.runtimeType, err)
	}

	// Load service templates
	templates, err := scopedProvider.LoadServiceTemplates(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service templates for %s: %w", svc.CatalogID, err)
	}

	// Extract images from templates with custom values directly into imageSet
	if err := scopedProvider.CollectImagesFromTemplates(ctx, templates, svc.Values, imageSet); err != nil {
		return fmt.Errorf("failed to extract images from service %s: %w", svc.CatalogID, err)
	}

	return nil
}

// pullImages pulls only missing images from the provided set using the shared podmanutils helper.
// Images that are already present locally are skipped.
func (d *PodmanDeployer) pullImages(ctx context.Context, imageSet map[string]bool) error {
	images := make([]string, 0, len(imageSet))
	for img := range imageSet {
		images = append(images, img)
	}

	return podmanutils.PullImages(ctx, d.runtime, images)
}

// deployComponents deploys all components concurrently.
// All components are treated as shared and deployed together.
func (d *PodmanDeployer) deployComponents(ctx context.Context, plan *DeploymentPlan) error {
	// Deploy all components concurrently
	logger.InfofCtx(ctx, "Deploying %d components concurrently...\n", len(plan.Components))
	if err := d.deployComponentsConcurrently(ctx, plan.Components, plan); err != nil {
		return fmt.Errorf("failed to deploy components: %w", err)
	}

	logger.InfofCtx(ctx, "All components deployed successfully\n")

	return nil
}

// deployComponentsConcurrently deploys multiple components concurrently using goroutines.
func (d *PodmanDeployer) deployComponentsConcurrently(ctx context.Context, components map[string]*ComponentPlan, plan *DeploymentPlan) error {
	if len(components) == 0 {
		return nil
	}

	// Build hash → dbID index so RunConcurrently can track status per component.
	// A shared mutex is passed into each deployComponent call to protect concurrent
	// writes to service Values maps (endpoint merging).
	var mu sync.Mutex
	items := make(map[string]uuid.UUID, len(components))
	for hash, comp := range components {
		items[hash] = comp.DatabaseID
	}

	return catalogutils.RunConcurrently(
		ctx,
		items,
		func(ctx context.Context, hash string) error {
			return d.deployComponent(ctx, hash, components[hash], plan, &mu)
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateComponentStatus(ctx, d.componentRepo, dbID, models.ComponentStatusError, msg)
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateComponentStatus(ctx, d.componentRepo, dbID, models.ComponentStatusRunning, msg)
		},
	)
}

// deployComponent deploys a single component and updates its endpoint in the database.
func (d *PodmanDeployer) deployComponent(ctx context.Context, hash string, comp *ComponentPlan, plan *DeploymentPlan, mu *sync.Mutex) error {
	logger.InfofCtx(ctx, "Deploying component %s (%s/%s)...\n", comp.ComponentType, comp.ProviderID, hash)

	component, metadata, tmpls, err := d.loadComponentResources(comp)
	if err != nil {
		return err
	}

	logger.InfofCtx(ctx, "Component %s loaded: %s\n", component.ID, component.Name)

	if err := d.deployComponentPods(ctx, comp, metadata, tmpls, plan); err != nil {
		return fmt.Errorf("failed to deploy component pods: %w", err)
	}

	d.mergeComponentEndpoints(ctx, comp, plan, mu)

	// Update component endpoints in database (internal endpoints only)
	if len(comp.Endpoints) > 0 {
		if err := d.updateComponentEndpointsInDB(ctx, comp); err != nil {
			return fmt.Errorf("failed to update component endpoints in database: %w", err)
		}
	}

	logger.InfofCtx(ctx, "Component %s deployed successfully\n", comp.ComponentType)

	return nil
}

// loadComponentResources loads all necessary resources for a component.
func (d *PodmanDeployer) loadComponentResources(comp *ComponentPlan) (*types.Component, *templates.AppMetadata, map[string]*template.Template, error) {
	scopedProvider, err := d.catalogProvider.WithRuntime(d.runtimeType)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to scope catalog provider for runtime %q: %w", d.runtimeType, err)
	}

	component, err := d.catalogProvider.LoadComponent(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load component from catalog: %w", err)
	}

	metadata, err := scopedProvider.LoadComponentRuntimeMetadata(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load component runtime metadata: %w", err)
	}

	tmpls, err := scopedProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load component templates: %w", err)
	}

	return component, metadata, tmpls, nil
}

// mergeComponentEndpoints merges component endpoints into services that use the component.
func (d *PodmanDeployer) mergeComponentEndpoints(ctx context.Context, comp *ComponentPlan, plan *DeploymentPlan, mu *sync.Mutex) {
	if len(comp.Endpoints) == 0 {
		logger.InfofCtx(ctx, "Component %s has no endpoints to merge\n", comp.ComponentType)

		return
	}

	mu.Lock()
	defer mu.Unlock()

	for _, serviceID := range comp.UsedByServices {
		d.mergeEndpointIntoService(ctx, comp, plan, serviceID)
	}
}

// mergeEndpointIntoService merges component endpoint data into a specific service.
func (d *PodmanDeployer) mergeEndpointIntoService(ctx context.Context, comp *ComponentPlan, plan *DeploymentPlan, serviceID string) {
	svc, ok := plan.Services[serviceID]
	if !ok {
		return
	}

	if svc.Values == nil {
		svc.Values = make(map[string]any)
	}

	// Add instanceSlug to the component's values in the service
	// This allows templates to reference it as .Values.vector_store.instanceSlug
	if componentValues, ok := svc.Values[comp.ComponentType].(map[string]any); ok {
		instanceSlug := catalogutils.GenerateInstanceSlug(comp.DatabaseID.String())
		componentValues["instanceSlug"] = instanceSlug
		logger.InfofCtx(ctx, "Added instanceSlug '%s' to component %s in service %s\n", instanceSlug, comp.ComponentType, serviceID)
	}

	endpointData, ok := comp.Endpoints[comp.ComponentType]
	if !ok {
		logger.ErrorfCtx(ctx, "Component %s endpoint data not found in comp.Endpoints map\n", comp.ComponentType)

		return
	}

	d.updateServiceValuesWithEndpoint(ctx, svc, comp.ComponentType, endpointData, serviceID)
}

// updateServiceValuesWithEndpoint updates service values with endpoint data.
func (d *PodmanDeployer) updateServiceValuesWithEndpoint(
	ctx context.Context,
	svc *ServicePlan,
	componentType string,
	endpointData any,
	serviceID string,
) {
	existingData, exists := svc.Values[componentType]
	if !exists {
		svc.Values[componentType] = endpointData

		return
	}

	existingMap, isMap := existingData.(map[string]any)
	if !isMap {
		svc.Values[componentType] = endpointData

		return
	}

	endpointMap, isEndpointMap := endpointData.(map[string]any)
	if isEndpointMap {
		maps.Copy(existingMap, endpointMap)
		logger.InfofCtx(ctx, "Updated component %s host/port in service %s\n", componentType, serviceID)
	}
}

// deployComponentPods deploys all pods for a component and extracts endpoint information.
// The rendering, Spyre env injection and pod creation are shared with the model deployment
// path via podmanutils.DeployComponentPods.
func (d *PodmanDeployer) deployComponentPods(
	ctx context.Context,
	comp *ComponentPlan,
	metadata *templates.AppMetadata,
	tmpls map[string]*template.Template,
	plan *DeploymentPlan,
) error {
	componentEndpoints, err := podmanutils.DeployComponentPods(ctx, podmanutils.ComponentPodsParams{
		Runtime:       d.runtime,
		ComponentID:   comp.DatabaseID,
		ComponentType: comp.ComponentType,
		Values:        comp.Values,
		Metadata:      metadata,
		Templates:     tmpls,
		SpyrePool:     plan.SpyreCardPool,
		BaseDir:       d.getBaseDir(),
	})
	if err != nil {
		return err
	}

	// Store extracted endpoints in the component plan for use by services
	if len(componentEndpoints) > 0 {
		comp.Endpoints = componentEndpoints
		logger.InfofCtx(ctx, "Component %s endpoints extracted: %v\n", comp.ComponentType, componentEndpoints)
	}

	return nil
}

// deployServices deploys all services in the plan concurrently.
func (d *PodmanDeployer) deployServices(ctx context.Context, plan *DeploymentPlan) error {
	logger.InfofCtx(ctx, "Deploying %d services concurrently...\n", len(plan.Services))

	// Build id → dbID index so RunConcurrently can track status per service.
	items := make(map[string]uuid.UUID, len(plan.Services))
	for id, svc := range plan.Services {
		items[id] = svc.DatabaseID
	}

	return catalogutils.RunConcurrently(
		ctx,
		items,
		func(ctx context.Context, id string) error {
			return d.deployService(ctx, plan, id, plan.Services[id])
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateServiceStatus(ctx, d.serviceRepo, dbID, models.ServiceStatusError, msg)
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateServiceStatus(ctx, d.serviceRepo, dbID, models.ServiceStatusRunning, msg)
		},
	)
}

// deployService deploys a single service and updates its endpoint in the database.
func (d *PodmanDeployer) deployService(ctx context.Context, plan *DeploymentPlan, serviceID string, svc *ServicePlan) error {
	logger.InfofCtx(ctx, "Deploying service %s...\n", serviceID)

	// Update service status to Initializing in database
	if err := catalogutils.UpdateServiceStatus(ctx, d.serviceRepo, svc.DatabaseID, models.ServiceStatusInitializing, "Deploying service"); err != nil {
		logger.ErrorfCtx(ctx, "Failed to update service status to Initializing: %v\n", err)
		// Don't fail the deployment if status update fails
	}

	// Load service from catalog
	service, err := d.catalogProvider.LoadService(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service from catalog: %w", err)
	}
	logger.InfofCtx(ctx, "Service %s loaded: %s\n", service.ID, service.Name)

	scopedProvider, err := d.catalogProvider.WithRuntime(d.runtimeType)
	if err != nil {
		return fmt.Errorf("failed to scope catalog provider for runtime %q: %w", d.runtimeType, err)
	}

	// Load runtime-specific metadata (contains PodTemplateExecutions)
	serviceAppMetadata, err := scopedProvider.LoadServiceRuntimeMetadata(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service runtime metadata: %w", err)
	}

	// Load service templates
	tmpls, err := scopedProvider.LoadServiceTemplates(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service templates: %w", err)
	}

	// Deploy service pods
	if err := d.deployServicePods(ctx, plan.ApplicationID, svc, serviceAppMetadata, tmpls); err != nil {
		return fmt.Errorf("failed to deploy service pods: %w", err)
	}

	logger.InfofCtx(ctx, "Service %s deployed successfully\n", serviceID)

	return nil
}

// deployServicePods deploys all pods for a service and collects routes annotations.
func (d *PodmanDeployer) deployServicePods(
	ctx context.Context,
	applicationID uuid.UUID,
	svc *ServicePlan,
	metadata *templates.AppMetadata,
	tmpls map[string]*template.Template,
) error {
	// Use the values already loaded in the service plan
	values := svc.Values

	// Initialize routes map to collect routes from deployed pods
	if svc.Routes == nil {
		svc.Routes = make(map[string]string)
	}

	// If PodTemplateExecutions is defined, use it for ordered deployment
	if len(metadata.PodTemplateExecutions) > 0 {
		return d.deployPodTemplatesInOrder(ctx, applicationID, svc, metadata, tmpls, values)
	}

	// If no PodTemplateExecutions defined, deploy all templates
	return d.deployAllPodTemplates(ctx, applicationID, svc, tmpls, values)
}

// deployPodTemplatesInOrder deploys pod templates following the defined execution order.
func (d *PodmanDeployer) deployPodTemplatesInOrder(
	ctx context.Context,
	applicationID uuid.UUID,
	svc *ServicePlan,
	metadata *templates.AppMetadata,
	tmpls map[string]*template.Template,
	values map[string]any,
) error {
	// Execute each pod template in the service following the defined order
	for _, layer := range metadata.PodTemplateExecutions {
		if err := d.deployPodTemplateLayer(ctx, applicationID, svc, layer, tmpls, values); err != nil {
			return err
		}
	}

	return nil
}

// deployPodTemplateLayer deploys all pod templates in a single layer.
func (d *PodmanDeployer) deployPodTemplateLayer(
	ctx context.Context,
	applicationID uuid.UUID,
	svc *ServicePlan,
	layer []string,
	tmpls map[string]*template.Template,
	values map[string]any,
) error {
	for _, podTemplateName := range layer {
		initialParams := d.buildInitialParams(applicationID, svc.DatabaseID, values)

		podEndpoints, podName, routes, err := d.deployPodTemplate(ctx, podTemplateName, tmpls, initialParams)
		if err != nil {
			return fmt.Errorf("failed to deploy pod template %s: %w", podTemplateName, err)
		}

		if routes != "" {
			svc.Routes[podName] = routes
			if svc.InternalEndpoint == "" {
				svc.InternalEndpoint = buildInternalEndpointURL(podEndpoints)
			}
		}
	}

	return nil
}

// deployAllPodTemplates deploys all pod templates without a specific order.
func (d *PodmanDeployer) deployAllPodTemplates(
	ctx context.Context,
	applicationID uuid.UUID,
	svc *ServicePlan,
	tmpls map[string]*template.Template,
	values map[string]any,
) error {
	for templateName := range tmpls {
		initialParams := d.buildInitialParams(applicationID, svc.DatabaseID, values)

		podEndpoints, podName, routes, err := d.deployPodTemplate(ctx, templateName, tmpls, initialParams)
		if err != nil {
			return fmt.Errorf("failed to deploy pod template %s: %w", templateName, err)
		}

		if routes != "" {
			svc.Routes[podName] = routes
			if svc.InternalEndpoint == "" {
				svc.InternalEndpoint = buildInternalEndpointURL(podEndpoints)
			}
		}
	}

	return nil
}

// buildInitialParams creates the initial parameters map for template deployment.
func (d *PodmanDeployer) buildInitialParams(applicationID uuid.UUID, databaseID uuid.UUID, values map[string]any) map[string]any {
	return map[string]any{
		"InstanceSlug": catalogutils.GenerateInstanceSlug(applicationID.String()),
		"TemplateID":   databaseID,
		"BaseDir":      d.getBaseDir(),
		"Values":       values,
		"env":          map[string]map[string]string{},
	}
}

// deployPodSpec deploys a pod using the rendered YAML bytes directly.
func (d *PodmanDeployer) deployPodSpec(ctx context.Context, podSpec *podmodels.PodSpec, renderedBytes []byte, templateName string) error {
	return podmanutils.DeployPodmanPod(ctx, d.runtime, podSpec, renderedBytes, templateName)
}

// deployPodTemplate deploys a single pod template for a service and returns endpoint information and routes.
func (d *PodmanDeployer) deployPodTemplate(
	ctx context.Context,
	podTemplateName string,
	tmpls map[string]*template.Template,
	initialParams map[string]any,
) (map[string]string, string, string, error) {
	logger.InfofCtx(ctx, "Deploying service template '%s'...\n", podTemplateName)

	podTemplate, ok := tmpls[podTemplateName]
	if !ok {
		return nil, "", "", fmt.Errorf("pod template '%s' not found", podTemplateName)
	}

	// Render template to get both PodSpec and raw bytes
	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, initialParams); err != nil {
		return nil, "", "", fmt.Errorf("failed to render template %s: %w", podTemplateName, err)
	}

	renderedBytes := rendered.Bytes()
	if strings.TrimSpace(string(renderedBytes)) == "" {
		// skip deploy if there is nothing to apply
		return nil, "", "", nil
	}

	// Parse into PodSpec for metadata
	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(renderedBytes, &podSpec); err != nil {
		return nil, "", "", fmt.Errorf("failed to parse rendered pod spec: %w", err)
	}

	// Extract routes annotation if present
	routes := ""
	if podSpec.Annotations != nil {
		if r, ok := podSpec.Annotations[constants.PodRoutesAnnotationKey]; ok {
			routes = r
		}
	}

	exists, err := d.runtime.PodExists(ctx, podSpec.Name)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to check pod existence: %w", err)
	}

	if exists {
		logger.InfofCtx(ctx, "Pod '%s' already exists, skipping deployment\n", podSpec.Name)

		return d.extractPodEndpoints(&podSpec), podSpec.Name, routes, nil
	}

	// Deploy using rendered bytes directly (same as components)
	if err := d.deployPodSpec(ctx, &podSpec, renderedBytes, podTemplateName); err != nil {
		return nil, "", "", err
	}

	logger.InfofCtx(ctx, "Service template '%s' deployed successfully\n", podTemplateName)

	return d.extractPodEndpoints(&podSpec), podSpec.Name, routes, nil
}

// extractPodEndpoints extracts endpoint information from a pod specification.
func (d *PodmanDeployer) extractPodEndpoints(podSpec *podmodels.PodSpec) map[string]string {
	endpoints := make(map[string]string)
	endpoints["host"] = podSpec.Name

	if len(podSpec.Spec.Containers) > 0 && len(podSpec.Spec.Containers[0].Ports) > 0 {
		endpoints["port"] = fmt.Sprintf("%d", podSpec.Spec.Containers[0].Ports[0].ContainerPort)
	}

	return endpoints
}

// getBaseDir returns the base directory for deployment artifacts.
func (d *PodmanDeployer) getBaseDir() string {
	if d.baseDir != "" {
		return d.baseDir
	}

	return utils.GetBaseDir()
}

// registerApplicationRoutes registers routes for all services with Caddy proxy and updates endpoints in database.
func (d *PodmanDeployer) registerApplicationRoutes(ctx context.Context, plan *DeploymentPlan) error {
	logger.InfofCtx(ctx, "Registering routes for application '%s'\n", plan.ApplicationName)

	proxyManager, err := d.getProxyManager()
	if err != nil {
		return err
	}

	// Register routes for each service and update endpoints in database
	var registrationErrors []error
	for _, svc := range plan.Services {
		if len(svc.Routes) == 0 {
			continue
		}

		if err := d.registerServiceRoutes(ctx, svc, proxyManager, &registrationErrors); err != nil {
			registrationErrors = append(registrationErrors, err)
		}
	}

	if len(registrationErrors) > 0 {
		return fmt.Errorf("failed to register routes and update endpoints: %w", errors.Join(registrationErrors...))
	}

	logger.InfofCtx(ctx, "Successfully registered routes and updated endpoints for application '%s'\n", plan.ApplicationName)

	return nil
}

// getProxyManager returns the appropriate ProxyManager for this deployment.
// domainSuffix and httpsPort are intentionally not returned — RegisterRoute
// on the ProxyManager reads both from its own environment (local or worker)
// so the correct values are always used regardless of where Caddy is running.
func (d *PodmanDeployer) getProxyManager() (proxy.ProxyManager, error) {
	if rt, ok := d.runtime.(*remoteruntime.RemoteRuntime); ok {
		return proxy.NewRemoteProxyManager(rt.Sender), nil
	}

	return proxy.GetCaddyProxyManager()
}

// registerServiceRoutes registers routes for a single service and updates its endpoints in the database.
// domainSuffix and httpsPort are resolved by RegisterRoute from the ProxyManager's own environment.
func (d *PodmanDeployer) registerServiceRoutes(
	ctx context.Context,
	svc *ServicePlan,
	proxyManager proxy.ProxyManager,
	registrationErrors *[]error,
) error {
	var serviceEndpoints []map[string]any

	// Register routes for each pod in the service
	for podName, routesAnnotation := range svc.Routes {
		registeredRoutes, err := proxy.RegisterRoutesForAppAndReturn(
			ctx,
			catalogconstants.CatalogAppName,
			proxyManager,
			routesAnnotation,
			podName,
		)
		if err != nil {
			*registrationErrors = append(*registrationErrors, fmt.Errorf("pod %s: %w", podName, err))

			continue
		}

		for _, route := range registeredRoutes {
			endpoint := map[string]any{
				"type": route.Type,
				"url":  route.ExternalURL,
			}
			serviceEndpoints = append(serviceEndpoints, endpoint)
		}
	}

	// Also store the internal pod-to-pod endpoint
	if svc.InternalEndpoint != "" {
		serviceEndpoints = append(serviceEndpoints, map[string]any{
			"type": "internal",
			"url":  svc.InternalEndpoint,
		})
	}

	// Update service endpoints in database
	if len(serviceEndpoints) > 0 {
		if err := d.serviceRepo.UpdateEndpoints(ctx, svc.DatabaseID, serviceEndpoints); err != nil {
			return fmt.Errorf("service %s DB update: %w", svc.DatabaseID, err)
		}
		logger.InfofCtx(ctx, "Updated service %s with %d endpoint(s) in database\n", svc.DatabaseID, len(serviceEndpoints))
	}

	return nil
}

// buildInternalEndpointURL constructs http://host:port from the map returned by extractPodEndpoints.
// Returns an empty string when host is missing.
func buildInternalEndpointURL(endpoints map[string]string) string {
	host := endpoints["host"]
	if host == "" {
		return ""
	}

	port := endpoints["port"]
	if port == "" {
		return fmt.Sprintf("http://%s", host)
	}

	return fmt.Sprintf("http://%s:%s", host, port)
}

// updateComponentEndpointsInDB updates component endpoints in the database.
// Component endpoints are service endpoints (not exposed via Caddy) and stored in list format.
// [{"type": "service", "url": "http://host:port"}].
func (d *PodmanDeployer) updateComponentEndpointsInDB(ctx context.Context, comp *ComponentPlan) error {
	if comp.DatabaseID == uuid.Nil {
		logger.InfofCtx(ctx, "Component %s has no database ID, skipping endpoint update\n", comp.ComponentType)

		return nil
	}

	// Extract endpoint information from comp.Endpoints
	// comp.Endpoints format: {"component_type": {"host": "pod-name", "port": "8080"}}
	endpointData, ok := comp.Endpoints[comp.ComponentType]
	if !ok {
		logger.InfofCtx(ctx, "No endpoint data found for component %s\n", comp.ComponentType)

		return nil
	}

	endpointMap, ok := endpointData.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid endpoint data format for component %s", comp.ComponentType)
	}

	// Build URL from host and port
	host := endpointMap["host"].(string)
	port := endpointMap["port"].(string)

	// Create endpoint list with type and url
	url := fmt.Sprintf("http://%s:%s", host, port)
	endpointsList := []map[string]any{
		{
			"type": "service",
			"url":  url,
		},
	}

	// Update component endpoints in database
	if err := d.componentRepo.UpdateEndpoints(ctx, comp.DatabaseID, endpointsList); err != nil {
		return fmt.Errorf("failed to update component %s endpoints: %w", comp.ComponentType, err)
	}

	logger.InfofCtx(ctx, "Updated component %s with service endpoint in database: %v\n", comp.ComponentType, endpointsList)

	return nil
}

// Made with Bob
