package podman

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	podmodels "github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	k8syaml "sigs.k8s.io/yaml"
)

// ComponentInfo holds the information derived from a deployed component.
type ComponentInfo struct {
	Endpoint string
	Domain   string
	Port     string
	Model    string
}

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

	// Step 1.a: Pull container images for all components and services
	if err := d.pullImagesForDeployment(ctx, plan); err != nil {
		d.handleDeploymentStepError(ctx, plan.ApplicationID, "Image pull failed", err)

		return fmt.Errorf("failed to pull images: %w", err)
	}

	// Step 1.b: Download models specified in parameters
	if err := d.downloadModelsForDeployment(ctx, plan); err != nil {
		d.handleDeploymentStepError(ctx, plan.ApplicationID, "Model download failed", err)

		return fmt.Errorf("failed to download models: %w", err)
	}

	// Update application status to Deploying before starting deployment
	if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, plan.ApplicationID, models.ApplicationStatusDeploying, "Deploying application"); err != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status to Deploying: %v\n", err)
	}

	// Step 2: Deploy components if any
	if len(plan.Components) > 0 {
		if err := d.deployComponents(ctx, plan); err != nil {
			d.handleDeploymentStepError(ctx, plan.ApplicationID, "Component deployment failed", err)

			return fmt.Errorf("failed to deploy components: %w", err)
		}
	}

	// Step 4: Deploy services if any
	if len(plan.Services) > 0 {
		if err := d.deployServices(ctx, plan); err != nil {
			d.handleDeploymentStepError(ctx, plan.ApplicationID, "Service deployment failed", err)

			return fmt.Errorf("failed to deploy services: %w", err)
		}
	}

	// Step 5: Register routes with Caddy proxy
	if err := d.registerApplicationRoutes(ctx, plan); err != nil {
		d.handleDeploymentStepError(ctx, plan.ApplicationID, "Failed to register application routes", err)

		return fmt.Errorf("failed to register application routes: %w", err)
	}

	// Step 6: Update application status to Running
	if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, plan.ApplicationID, models.ApplicationStatusRunning, "Deployment completed successfully"); err != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status to Running: %v\n", err)
	}

	logger.InfofCtx(ctx, "Deployment completed successfully for '%s'\n", plan.ApplicationName)

	return nil
}

// handleDeploymentStepError updates application status to Error and logs the failure.
func (d *PodmanDeployer) handleDeploymentStepError(ctx context.Context, appID uuid.UUID, context string, err error) {
	errMsg := fmt.Sprintf("%s: %v", context, err)
	if updateErr := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, appID, models.ApplicationStatusError, errMsg); updateErr != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status: %v\n", updateErr)
	}
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

// downloadModels downloads all models in the provided set.
func (d *PodmanDeployer) downloadModels(ctx context.Context, modelSet map[string]bool) error {
	modelsPath := utils.GetModelsPath()

	for modelName := range modelSet {
		logger.InfofCtx(ctx, "Downloading model: %s\n", modelName)

		if err := helpers.DownloadModelContainer(ctx, modelName, modelsPath); err != nil {
			return fmt.Errorf("failed to download model %s: %w", modelName, err)
		}
	}

	return nil
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

	if err := d.pullImages(imageSet); err != nil {
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
	// Load component templates
	templates, err := d.catalogProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to load component templates for %s/%s: %w", comp.ComponentType, comp.ProviderID, err)
	}

	// Extract images from templates with custom values directly into imageSet
	if err := d.catalogProvider.CollectImagesFromTemplates(ctx, templates, comp.Values, imageSet); err != nil {
		return fmt.Errorf("failed to extract images from component %s/%s: %w", comp.ComponentType, comp.ProviderID, err)
	}

	return nil
}

// extractImagesFromService extracts container images from a service's templates.
func (d *PodmanDeployer) extractImagesFromService(ctx context.Context, svc *ServicePlan, imageSet map[string]bool) error {
	// Load service templates
	templates, err := d.catalogProvider.LoadServiceTemplates(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service templates for %s: %w", svc.CatalogID, err)
	}

	// Extract images from templates with custom values directly into imageSet
	if err := d.catalogProvider.CollectImagesFromTemplates(ctx, templates, svc.Values, imageSet); err != nil {
		return fmt.Errorf("failed to extract images from service %s: %w", svc.CatalogID, err)
	}

	return nil
}

// pullImages pulls only missing images from the provided set using the runtime.
// Images that are already present locally are skipped.
func (d *PodmanDeployer) pullImages(imageSet map[string]bool) error {
	// Convert map to slice
	images := make([]string, 0, len(imageSet))
	for img := range imageSet {
		images = append(images, img)
	}

	// Use the image package's IfNotPresent method
	imgHelper := &image.Images{
		Runtime: d.runtime,
	}

	if err := imgHelper.IfNotPresent(images); err != nil {
		return fmt.Errorf("failed to pull images: %w", err)
	}

	return nil
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

	var wg sync.WaitGroup
	var mu sync.Mutex // Mutex to protect concurrent writes to service Values maps
	errChan := make(chan error, len(components))

	for hash, comp := range components {
		wg.Add(1)
		go func(h string, c *ComponentPlan) {
			defer wg.Done()
			if err := d.deployComponent(ctx, h, c, plan, &mu); err != nil {
				errMsg := fmt.Sprintf("Component deployment failed: %v", err)
				if updateErr := catalogutils.UpdateComponentStatus(ctx, d.componentRepo, c.DatabaseID, models.ComponentStatusError, errMsg); updateErr != nil {
					logger.ErrorfCtx(ctx, "Failed to update component %s status: %v\n", h, updateErr)
				}
				errChan <- fmt.Errorf("failed to deploy component %s: %w", h, err)

				return
			}
			// Update component status to Running after successful deployment
			if err := catalogutils.UpdateComponentStatus(ctx, d.componentRepo, c.DatabaseID, models.ComponentStatusRunning, "Component deployed successfully"); err != nil {
				logger.ErrorfCtx(ctx, "Failed to update component %s status to Running: %v\n", h, err)
			}
		}(hash, comp)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check for any errors
	errs := make([]error, 0, len(plan.Components))
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// Return the first error (could be enhanced to return all errors)
		return errs[0]
	}

	return nil
}

// deployComponent deploys a single component and updates its endpoint in the database.
func (d *PodmanDeployer) deployComponent(ctx context.Context, hash string, comp *ComponentPlan, plan *DeploymentPlan, mu *sync.Mutex) error {
	logger.InfofCtx(ctx, "Deploying component %s (%s/%s)...\n", comp.ComponentType, comp.ProviderID, hash)

	component, metadata, tmpls, err := d.loadComponentResources(comp)
	if err != nil {
		return err
	}

	logger.InfofCtx(ctx, "Component %s loaded: %s\n", component.ID, component.Name)

	if err := d.deployComponentPods(ctx, comp, metadata, tmpls, comp.CatalogPath, plan); err != nil {
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
	component, err := d.catalogProvider.LoadComponent(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load component from catalog: %w", err)
	}

	metadata, err := d.catalogProvider.LoadComponentRuntimeMetadata(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load component runtime metadata: %w", err)
	}

	tmpls, err := d.catalogProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
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
func (d *PodmanDeployer) deployComponentPods(
	ctx context.Context,
	comp *ComponentPlan,
	metadata *templates.AppMetadata,
	tmpls map[string]*template.Template,
	componentPath string,
	plan *DeploymentPlan,
) error {
	// Use the loaded Values from the component plan (includes defaults from values.yaml + overrides)
	values := comp.Values

	// Initialize component endpoints map to store extracted endpoint info
	componentEndpoints := make(map[string]any)

	// If PodTemplateExecutions is defined, use it for ordered deployment
	if len(metadata.PodTemplateExecutions) > 0 {
		// Execute each pod template in the component following the defined order
		for _, layer := range metadata.PodTemplateExecutions {
			for _, podTemplateName := range layer {
				// Prepare initialParams for the template
				initialParams := map[string]any{
					"InstanceSlug": catalogutils.GenerateInstanceSlug(comp.DatabaseID.String()),
					"TemplateID":   comp.DatabaseID,
					"BaseDir":      utils.GetBaseDir(),
					"Values":       values,
					"env":          map[string]map[string]string{},
				}

				// Pass componentEndpoints to collect endpoint info, use component type as ID
				if err := d.deployComponentTemplate(ctx, podTemplateName, tmpls, plan, initialParams, componentEndpoints, comp.ComponentType); err != nil {
					return fmt.Errorf("failed to deploy pod template %s: %w", podTemplateName, err)
				}
			}
		}
	} else {
		// If no PodTemplateExecutions defined, deploy all templates
		logger.InfofCtx(ctx, "No PodTemplateExecutions defined for %s, deploying all templates\n", componentPath)
		for templateName := range tmpls {
			// Prepare initialParams for the template
			initialParams := map[string]any{
				"InstanceSlug": catalogutils.GenerateInstanceSlug(comp.DatabaseID.String()),
				"TemplateID":   comp.DatabaseID,
				"BaseDir":      utils.GetBaseDir(),
				"Values":       values,
				"env":          map[string]map[string]string{},
			}

			// Pass componentEndpoints to collect endpoint info, use component type as ID
			if err := d.deployComponentTemplate(ctx, templateName, tmpls, plan, initialParams, componentEndpoints, comp.ComponentType); err != nil {
				return fmt.Errorf("failed to deploy pod template %s: %w", templateName, err)
			}
		}
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

	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.Services))

	for serviceID, svc := range plan.Services {
		wg.Add(1)
		go func(sID string, service *ServicePlan) {
			defer wg.Done()

			if err := d.deployService(ctx, plan, sID, service); err != nil {
				// Update service status to Error
				errMsg := fmt.Sprintf("Service deployment failed: %v", err)
				if updateErr := catalogutils.UpdateServiceStatus(ctx, d.serviceRepo, service.DatabaseID, models.ServiceStatusError, errMsg); updateErr != nil {
					logger.ErrorfCtx(ctx, "Failed to update service %s status: %v\n", sID, updateErr)
				}
				errCh <- fmt.Errorf("failed to deploy service %s: %w", sID, err)

				return
			}

			// Update service status to Running after successful deployment
			if err := catalogutils.UpdateServiceStatus(ctx, d.serviceRepo, service.DatabaseID, models.ServiceStatusRunning, "Service deployed successfully"); err != nil {
				logger.ErrorfCtx(ctx, "Failed to update service %s status to Running: %v\n", sID, err)
				// Don't fail the deployment if status update fails
			}
		}(serviceID, svc)
	}

	wg.Wait()
	close(errCh)

	// Collect all errors
	errs := make([]error, 0, len(plan.Services))
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("service deployment errors: %v", errs)
	}

	logger.InfofCtx(ctx, "All services deployed successfully\n")

	return nil
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

	// Load runtime-specific metadata (contains PodTemplateExecutions)
	serviceAppMetadata, err := d.catalogProvider.LoadServiceRuntimeMetadata(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service runtime metadata: %w", err)
	}

	// Load service templates
	tmpls, err := d.catalogProvider.LoadServiceTemplates(svc.CatalogID)
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

		_, podName, routes, err := d.deployPodTemplate(ctx, podTemplateName, tmpls, initialParams)
		if err != nil {
			return fmt.Errorf("failed to deploy pod template %s: %w", podTemplateName, err)
		}

		if routes != "" {
			svc.Routes[podName] = routes
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

		_, podName, routes, err := d.deployPodTemplate(ctx, templateName, tmpls, initialParams)
		if err != nil {
			return fmt.Errorf("failed to deploy pod template %s: %w", templateName, err)
		}

		if routes != "" {
			svc.Routes[podName] = routes
		}
	}

	return nil
}

// buildInitialParams creates the initial parameters map for template deployment.
func (d *PodmanDeployer) buildInitialParams(applicationID uuid.UUID, databaseID uuid.UUID, values map[string]any) map[string]any {
	return map[string]any{
		"InstanceSlug": catalogutils.GenerateInstanceSlug(applicationID.String()),
		"TemplateID":   databaseID,
		"BaseDir":      utils.GetBaseDir(),
		"Values":       values,
		"env":          map[string]map[string]string{},
	}
}

// deployComponentTemplate deploys a component pod template.
// This is a generic method to deploy all component templates with Spyre card support.
// The serviceParams map is updated with the component's endpoint information (host and port).
func (d *PodmanDeployer) deployComponentTemplate(
	ctx context.Context,
	podTemplateName string,
	tmpls map[string]*template.Template,
	plan *DeploymentPlan,
	initialParams map[string]any,
	serviceParams map[string]any,
	componentID string,
) error {
	logger.InfofCtx(ctx, "Deploying component template '%s'...\n", podTemplateName)

	podTemplate, ok := tmpls[podTemplateName]
	if !ok {
		return fmt.Errorf("pod template '%s' not found", podTemplateName)
	}

	// Render and parse initial template
	podSpec, err := d.renderAndParsePodTemplate(podTemplate, podTemplateName, initialParams)
	if err != nil {
		return err
	}

	// Get environment parameters and render final template
	finalPodSpec, renderedBytes, err := d.renderFinalPodTemplate(ctx, podTemplate, podTemplateName, initialParams, podSpec, plan)
	if err != nil {
		return err
	}

	if strings.TrimSpace(string(renderedBytes)) == "" {
		// skip deploy if there is nothing to apply
		return nil
	}

	// Check if pod already exists
	if exists, err := d.runtime.PodExists(finalPodSpec.Name); err != nil {
		return fmt.Errorf("failed to check pod existence: %w", err)
	} else if exists {
		logger.InfofCtx(ctx, "Pod '%s' already exists, skipping deployment\n", podSpec.Name)

		return nil
	}

	// Deploy the pod using rendered bytes directly
	if err := d.deployPodSpec(ctx, finalPodSpec, renderedBytes, podTemplateName); err != nil {
		return err
	}

	logger.InfofCtx(ctx, "Component template '%s' deployed successfully\n", podTemplateName)

	// Update service params with endpoint information
	d.updateServiceParamsWithEndpoint(ctx, serviceParams, componentID, finalPodSpec)

	return nil
}

// renderAndParsePodTemplate renders a pod template and parses it into a PodSpec.
func (d *PodmanDeployer) renderAndParsePodTemplate(
	podTemplate *template.Template,
	templateName string,
	params map[string]any,
) (*podmodels.PodSpec, error) {
	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, params); err != nil {
		return nil, fmt.Errorf("failed to render template %s: %w", templateName, err)
	}

	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return nil, fmt.Errorf("failed to parse rendered pod spec: %w", err)
	}

	return &podSpec, nil
}

// renderFinalPodTemplate renders the final pod template with environment parameters.
// Returns both the PodSpec (for metadata) and the rendered bytes (for deployment).
func (d *PodmanDeployer) renderFinalPodTemplate(
	ctx context.Context,
	podTemplate *template.Template,
	templateName string,
	initialParams map[string]any,
	podSpec *podmodels.PodSpec,
	plan *DeploymentPlan,
) (*podmodels.PodSpec, []byte, error) {
	env, err := d.getEnvParamsForComponent(ctx, podSpec, plan)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get env params: %w", err)
	}

	// Always set/overwrite the env to ensure Spyre card PCI addresses are included
	initialParams["env"] = env

	var finalRendered bytes.Buffer
	if err := podTemplate.Execute(&finalRendered, initialParams); err != nil {
		return nil, nil, fmt.Errorf("failed to render template %s with env: %w", templateName, err)
	}

	renderedBytes := finalRendered.Bytes()

	// Parse into PodSpec for metadata (annotations, name, etc.) but don't use it for deployment
	var finalPodSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(renderedBytes, &finalPodSpec); err != nil {
		return nil, nil, fmt.Errorf("failed to parse final rendered pod spec: %w", err)
	}

	return &finalPodSpec, renderedBytes, nil
}

// deployPodSpec deploys a pod using the rendered YAML bytes directly.
func (d *PodmanDeployer) deployPodSpec(ctx context.Context, podSpec *podmodels.PodSpec, renderedBytes []byte, templateName string) error {
	// Use the rendered bytes directly instead of marshaling PodSpec

	reader := bytes.NewReader(renderedBytes)
	podAnnotations := specs.FetchPodAnnotations(*podSpec)
	podDeployOptions := clipodman.ConstructPodDeployOptions(podAnnotations)

	if err := clipodman.DeployPodAndReadinessCheck(ctx, d.runtime, podSpec, templateName, reader, podDeployOptions); err != nil {
		return fmt.Errorf("failed to deploy pod: %w", err)
	}

	return nil
}

// updateServiceParamsWithEndpoint updates service parameters with component endpoint information.
func (d *PodmanDeployer) updateServiceParamsWithEndpoint(
	ctx context.Context,
	serviceParams map[string]any,
	componentID string,
	podSpec *podmodels.PodSpec,
) {
	if serviceParams == nil || componentID == "" {
		return
	}

	componentInfo, err := d.extractComponentEndpointInfo(ctx, podSpec)
	if err != nil {
		logger.ErrorfCtx(ctx, "Failed to extract component endpoint info: %v\n", err)

		return
	}

	if componentInfo != nil {
		componentEndpoint := map[string]any{
			"host": componentInfo.Domain,
			"port": componentInfo.Port,
		}
		serviceParams[componentID] = componentEndpoint
		logger.InfofCtx(ctx, "Updated service params with component '%s' endpoint: %s:%s\n",
			componentID, componentInfo.Domain, componentInfo.Port)
	}
}

// extractComponentEndpointInfo extracts host (pod name) and port from a deployed pod spec.
func (d *PodmanDeployer) extractComponentEndpointInfo(ctx context.Context, podSpec *podmodels.PodSpec) (*ComponentInfo, error) {
	if podSpec == nil {
		return nil, fmt.Errorf("pod spec is nil")
	}

	// Use pod name as the host (for pod-to-pod communication)
	host := podSpec.Name
	if host == "" {
		return nil, fmt.Errorf("pod name is empty")
	}

	// Extract port from the first container's first exposed port
	var port string
	if len(podSpec.Spec.Containers) > 0 {
		container := podSpec.Spec.Containers[0]
		if len(container.Ports) > 0 {
			// Use the container port (not host port) for internal communication
			port = fmt.Sprintf("%d", container.Ports[0].ContainerPort)
		}
	}

	if port == "" {
		logger.InfofCtx(ctx, "No port found in pod spec for '%s'\n", host)
	}

	return &ComponentInfo{
		Domain: host,
		Port:   port,
	}, nil
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

	exists, err := d.runtime.PodExists(podSpec.Name)
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

// fetchSpyreCardsFromPodAnnotations extracts Spyre card requirements from pod annotations.
func (d *PodmanDeployer) fetchSpyreCardsFromPodAnnotations(annotations map[string]string) (int, map[string]int, error) {
	var spyreCards int
	spyreCardContainerMap := map[string]int{}

	isSpyreCardAnnotation := func(annotation string) (string, bool) {
		matches := vars.SpyreCardAnnotationRegex.FindStringSubmatch(annotation)
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

// getEnvParamsForComponent returns environment parameters for a component including Spyre card PCI addresses.
func (d *PodmanDeployer) getEnvParamsForComponent(ctx context.Context, podSpec *podmodels.PodSpec, plan *DeploymentPlan) (map[string]map[string]string, error) {
	env := make(map[string]map[string]string)

	// Get container names from pod spec
	for _, container := range podSpec.Spec.Containers {
		env[container.Name] = make(map[string]string)
	}

	if plan.SpyreCardPool == nil {
		return env, nil
	}

	// Fetch Spyre card requirements from annotations
	spyreCards, spyreCardContainerMap, err := d.fetchSpyreCardsFromPodAnnotations(podSpec.Annotations)
	if err != nil {
		return env, err
	}

	if spyreCards == 0 {
		return env, nil
	}

	// Allocate PCI addresses to containers that need them
	for containerName, spyreCount := range spyreCardContainerMap {
		if spyreCount != 0 {
			// Allocate addresses from the pool (thread-safe)
			allocatedAddresses, err := plan.SpyreCardPool.Allocate(spyreCount)
			if err != nil {
				return env, fmt.Errorf("failed to allocate Spyre cards for container %s: %w", containerName, err)
			}

			// Join addresses with space separator
			pciAddressStr := ""
			for i, addr := range allocatedAddresses {
				if i > 0 {
					pciAddressStr += " "
				}
				pciAddressStr += addr
			}

			env[containerName][string(constants.PCIAddressKey)] = pciAddressStr

			logger.InfofCtx(ctx, "Allocated %d Spyre cards to container '%s' in pod '%s': %s\n",
				spyreCount, containerName, podSpec.Name, pciAddressStr)
		}
	}

	return env, nil
}

// registerApplicationRoutes registers routes for all services with Caddy proxy and updates endpoints in database.
func (d *PodmanDeployer) registerApplicationRoutes(ctx context.Context, plan *DeploymentPlan) error {
	logger.InfofCtx(ctx, "Registering routes for application '%s'\n", plan.ApplicationName)

	domainSuffix, httpsPort, proxyManager, err := d.getCaddyConfiguration()
	if err != nil {
		return err
	}

	// Register routes for each service and update endpoints in database
	var registrationErrors []error
	for _, svc := range plan.Services {
		if len(svc.Routes) == 0 {
			continue
		}

		if err := d.registerServiceRoutes(ctx, svc, proxyManager, domainSuffix, httpsPort, &registrationErrors); err != nil {
			registrationErrors = append(registrationErrors, err)
		}
	}

	if len(registrationErrors) > 0 {
		return fmt.Errorf("failed to register routes and update endpoints: %w", errors.Join(registrationErrors...))
	}

	logger.InfofCtx(ctx, "Successfully registered routes and updated endpoints for application '%s'\n", plan.ApplicationName)

	return nil
}

// getCaddyConfiguration retrieves Caddy configuration and creates a ProxyManager.
func (d *PodmanDeployer) getCaddyConfiguration() (string, string, proxy.ProxyManager, error) {
	// Get domain suffix from env var (set during catalog configure)
	// This is pre-computed: certDomain OR customDomain OR hostIP.nip.io
	domainSuffix := utils.GetEnv("DOMAIN_SUFFIX", "")
	if domainSuffix == "" {
		return "", "", nil, fmt.Errorf("DOMAIN_SUFFIX environment variable not set")
	}

	httpsPort := utils.GetEnv("CADDY_HTTPS_PORT", catalogconstants.DefaultHTTPSPort)

	// Get Caddy proxy manager - fails if CADDY_ADMIN_URL not set
	proxyManager, err := proxy.GetCaddyProxyManager()
	if err != nil {
		return "", "", nil, err
	}

	return domainSuffix, httpsPort, proxyManager, nil
}

// registerServiceRoutes registers routes for a single service and updates its endpoints in the database.
func (d *PodmanDeployer) registerServiceRoutes(
	ctx context.Context,
	svc *ServicePlan,
	proxyManager proxy.ProxyManager,
	domainSuffix string,
	httpsPort string,
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
			domainSuffix,
			podName,
		)
		if err != nil {
			*registrationErrors = append(*registrationErrors, fmt.Errorf("pod %s: %w", podName, err))

			continue
		}

		// Convert registered routes to endpoint format using route type
		for _, route := range registeredRoutes {
			url := catalogutils.BuildExternalURL(route.Domain, httpsPort)

			endpoint := map[string]any{
				"type": route.Type,
				"url":  url,
			}
			serviceEndpoints = append(serviceEndpoints, endpoint)
		}
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
