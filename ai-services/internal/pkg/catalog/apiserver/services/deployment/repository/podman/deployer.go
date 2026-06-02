package podman

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
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
	logger.Infof("Starting deployment execution for '%s'\n", plan.ApplicationName)

	// Step 1.a: Pull container images for all components and services
	if err := d.pullImagesForDeployment(plan); err != nil {
		d.handleDeploymentError(ctx, plan.ApplicationID, "Image pull failed", err)

		return fmt.Errorf("failed to pull images: %w", err)
	}

	// Step 1.b: Download models specified in parameters
	if err := d.downloadModelsForDeployment(plan); err != nil {
		d.handleDeploymentError(ctx, plan.ApplicationID, "Model download failed", err)

		return fmt.Errorf("failed to download models: %w", err)
	}

	// Update application status to Deploying before starting deployment
	deployMsg := "Deploying application"
	d.updateStatusIgnoreError(ctx, plan.ApplicationID, models.ApplicationStatusDeploying, deployMsg)

	// Step 2: Deploy components if any
	if len(plan.Components) > 0 {
		if err := d.deployComponents(plan); err != nil {
			d.handleDeploymentError(ctx, plan.ApplicationID, "Component deployment failed", err)

			return fmt.Errorf("failed to deploy components: %w", err)
		}
	}

	// Step 4: Deploy services if any
	if len(plan.Services) > 0 {
		if err := d.deployServices(ctx, plan); err != nil {
			d.handleDeploymentError(ctx, plan.ApplicationID, "Service deployment failed", err)

			return fmt.Errorf("failed to deploy services: %w", err)
		}
	}

	// Step 5: Update application status to Running
	d.updateStatusIgnoreError(ctx, plan.ApplicationID, models.ApplicationStatusRunning, "Deployment completed successfully")

	logger.Infof("Deployment completed successfully for '%s'\n", plan.ApplicationName)

	return nil
}

// handleDeploymentError updates application status on error and logs any update failures.
func (d *PodmanDeployer) handleDeploymentError(ctx context.Context, appID uuid.UUID, message string, err error) {
	fullMessage := fmt.Sprintf("%s: %v", message, err)
	if updateErr := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, appID, models.ApplicationStatusError, fullMessage); updateErr != nil {
		logger.Errorf("Failed to update application status: %v\n", updateErr)
	}
}

// updateStatusIgnoreError updates application status and logs any failures without returning error.
func (d *PodmanDeployer) updateStatusIgnoreError(ctx context.Context, appID uuid.UUID, status models.ApplicationStatus, message string) {
	if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, appID, status, message); err != nil {
		logger.Errorf("Failed to update application status: %v\n", err)
	}
}

// downloadModelsForDeployment downloads all models specified in component and service parameters.
// Models are extracted from params that contain "model" in their key name.
func (d *PodmanDeployer) downloadModelsForDeployment(plan *DeploymentPlan) error {
	logger.Infof("Downloading models for application '%s'\n", plan.ApplicationName)

	modelSet := d.collectModelsFromPlan(plan)

	if len(modelSet) == 0 {
		logger.Infof("No models to download for application '%s'\n", plan.ApplicationName)

		return nil
	}

	if err := d.downloadModels(modelSet); err != nil {
		return err
	}

	logger.Infof("Successfully downloaded all models for application '%s'\n", plan.ApplicationName)

	return nil
}

// collectModelsFromPlan collects all unique model names from the deployment plan.
func (d *PodmanDeployer) collectModelsFromPlan(plan *DeploymentPlan) map[string]bool {
	modelSet := make(map[string]bool)

	// Extract models from component params
	for _, comp := range plan.Components {
		// do not download models for watsonx
		if strings.EqualFold(comp.ProviderID, "watsonx") {
			logger.Infof("Skipping model download for provider: %s\n", comp.ProviderID)

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
func (d *PodmanDeployer) downloadModels(modelSet map[string]bool) error {
	modelsPath := utils.GetModelsPath()

	for modelName := range modelSet {
		logger.Infof("Downloading model: %s\n", modelName)

		if err := helpers.DownloadModelContainer(modelName, modelsPath); err != nil {
			return fmt.Errorf("failed to download model %s: %w", modelName, err)
		}
	}

	return nil
}

// pullImagesForDeployment pulls all container images required for components and services.
func (d *PodmanDeployer) pullImagesForDeployment(plan *DeploymentPlan) error {
	logger.Infof("Pulling container images for application '%s'\n", plan.ApplicationName)

	imageSet := d.collectImagesFromPlan(plan)

	if len(imageSet) == 0 {
		logger.Infof("No images to pull for application '%s'\n", plan.ApplicationName)

		return nil
	}

	if err := d.pullImages(imageSet); err != nil {
		return err
	}

	logger.Infof("Successfully pulled all images for application '%s'\n", plan.ApplicationName)

	return nil
}

// collectImagesFromPlan collects all unique container images from the deployment plan.
func (d *PodmanDeployer) collectImagesFromPlan(plan *DeploymentPlan) map[string]bool {
	imageSet := make(map[string]bool)

	// Include tool image which is used for all housekeeping tasks
	imageSet[vars.ToolImage] = true

	// Extract images from component templates
	for _, comp := range plan.Components {
		d.extractImagesFromComponent(comp, imageSet)
	}

	// Extract images from service templates
	for _, svc := range plan.Services {
		d.extractImagesFromService(svc, imageSet)
	}

	return imageSet
}

// extractImagesFromComponent extracts container images from a component's templates.
func (d *PodmanDeployer) extractImagesFromComponent(comp *ComponentPlan, imageSet map[string]bool) {
	// Load component templates
	tmpls, err := d.catalogProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		logger.Errorf("Failed to load component templates for %s/%s: %v\n", comp.ComponentType, comp.ProviderID, err)

		return
	}

	// Extract images from each template
	for templateName, tmpl := range tmpls {
		d.extractImagesFromTemplate(tmpl, templateName, comp.Values, imageSet)
	}
}

// extractImagesFromService extracts container images from a service's templates.
func (d *PodmanDeployer) extractImagesFromService(svc *ServicePlan, imageSet map[string]bool) {
	// Load service templates
	tmpls, err := d.catalogProvider.LoadServiceTemplates(svc.CatalogID)
	if err != nil {
		logger.Errorf("Failed to load service templates for %s: %v\n", svc.CatalogID, err)

		return
	}

	// Extract images from each template
	for templateName, tmpl := range tmpls {
		d.extractImagesFromTemplate(tmpl, templateName, svc.Values, imageSet)
	}
}

// extractImagesFromTemplate renders a template and extracts container images from it.
func (d *PodmanDeployer) extractImagesFromTemplate(
	tmpl *template.Template,
	templateName string,
	values map[string]any,
	imageSet map[string]bool,
) {
	// Prepare minimal params for rendering
	initialParams := map[string]any{
		"InstanceSlug": "image-extraction",
		"TemplateID":   uuid.New(),
		"BaseDir":      utils.GetBaseDir(),
		"Values":       values,
		"env":          map[string]map[string]string{},
	}

	// Render the template
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, initialParams); err != nil {
		logger.Errorf("Failed to render template %s for image extraction: %v\n", templateName, err)

		return
	}

	// Parse the rendered template
	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		logger.Errorf("Failed to parse rendered template %s: %v\n", templateName, err)

		return
	}

	// Extract images from containers
	for _, container := range podSpec.Spec.Containers {
		if container.Image != "" {
			imageSet[container.Image] = true
		}
	}
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
func (d *PodmanDeployer) deployComponents(plan *DeploymentPlan) error {
	// Deploy all components concurrently
	logger.Infof("Deploying %d components concurrently...\n", len(plan.Components))
	if err := d.deployComponentsConcurrently(plan.Components, plan); err != nil {
		return fmt.Errorf("failed to deploy components: %w", err)
	}

	logger.Infof("All components deployed successfully\n")

	return nil
}

// deployComponentsConcurrently deploys multiple components concurrently using goroutines.
func (d *PodmanDeployer) deployComponentsConcurrently(components map[string]*ComponentPlan, plan *DeploymentPlan) error {
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
			if err := d.deployComponent(h, c, plan, &mu); err != nil {
				d.handleComponentDeploymentError(h, c, err)
				errChan <- fmt.Errorf("failed to deploy component %s: %w", h, err)

				return
			}
			d.handleComponentDeploymentSuccess(h, c)
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
func (d *PodmanDeployer) deployComponent(hash string, comp *ComponentPlan, plan *DeploymentPlan, mu *sync.Mutex) error {
	logger.Infof("Deploying component %s (%s/%s)...\n", comp.ComponentType, comp.ProviderID, hash)

	component, metadata, tmpls, err := d.loadComponentResources(comp)
	if err != nil {
		return err
	}

	logger.Infof("Component %s loaded: %s\n", component.ID, component.Name)

	if err := d.deployComponentPods(comp, metadata, tmpls, comp.CatalogPath, plan); err != nil {
		return fmt.Errorf("failed to deploy component pods: %w", err)
	}

	d.mergeComponentEndpoints(comp, plan, mu)

	logger.Infof("Component %s deployed successfully\n", comp.ComponentType)

	return nil
}

// handleComponentDeploymentError updates component status to Error when deployment fails.
func (d *PodmanDeployer) handleComponentDeploymentError(hash string, comp *ComponentPlan, err error) {
	if comp.DatabaseID == uuid.Nil {
		return
	}
	errMsg := fmt.Sprintf("Component deployment failed: %v", err)
	if updateErr := d.componentRepo.UpdateStatus(context.Background(), comp.DatabaseID, models.ComponentStatusError, errMsg); updateErr != nil {
		logger.Errorf("Failed to update component %s status: %v\n", hash, updateErr)
	}
}

// handleComponentDeploymentSuccess updates component status to Running after successful deployment.
func (d *PodmanDeployer) handleComponentDeploymentSuccess(hash string, comp *ComponentPlan) {
	if comp.DatabaseID == uuid.Nil {
		return
	}
	if err := d.componentRepo.UpdateStatus(context.Background(), comp.DatabaseID, models.ComponentStatusRunning, "Component deployed successfully"); err != nil {
		logger.Errorf("Failed to update component %s status to Running: %v\n", hash, err)
	}
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
func (d *PodmanDeployer) mergeComponentEndpoints(comp *ComponentPlan, plan *DeploymentPlan, mu *sync.Mutex) {
	if len(comp.Endpoints) == 0 {
		logger.Infof("Component %s has no endpoints to merge\n", comp.ComponentType)

		return
	}

	mu.Lock()
	defer mu.Unlock()

	for _, serviceID := range comp.UsedByServices {
		d.mergeEndpointIntoService(comp, plan, serviceID)
	}
}

// mergeEndpointIntoService merges component endpoint data into a specific service.
func (d *PodmanDeployer) mergeEndpointIntoService(comp *ComponentPlan, plan *DeploymentPlan, serviceID string) {
	svc, ok := plan.Services[serviceID]
	if !ok {
		return
	}

	logger.Infof("Service %s Values before merge: %v\n", serviceID, svc.Values)

	if svc.Values == nil {
		svc.Values = make(map[string]any)
	}

	// Add instanceSlug to the component's values in the service
	// This allows templates to reference it as .Values.vector_store.instanceSlug
	if componentValues, ok := svc.Values[comp.ComponentType].(map[string]any); ok {
		instanceSlug := generateInstanceSlug(comp.DatabaseID.String())
		componentValues["instanceSlug"] = instanceSlug
		logger.Infof("Added instanceSlug '%s' to component %s in service %s\n", instanceSlug, comp.ComponentType, serviceID)
	}

	endpointData, ok := comp.Endpoints[comp.ComponentType]
	if !ok {
		logger.Errorf("Component %s endpoint data not found in comp.Endpoints map\n", comp.ComponentType)

		return
	}

	d.updateServiceValuesWithEndpoint(svc, comp.ComponentType, endpointData, serviceID)
}

// updateServiceValuesWithEndpoint updates service values with endpoint data.
func (d *PodmanDeployer) updateServiceValuesWithEndpoint(
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
		logger.Infof("Updated component %s host/port in service %s\n", componentType, serviceID)
	}
}

// deployComponentPods deploys all pods for a component and extracts endpoint information.
func (d *PodmanDeployer) deployComponentPods(
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
					"InstanceSlug": generateInstanceSlug(comp.DatabaseID.String()),
					"TemplateID":   comp.DatabaseID,
					"BaseDir":      utils.GetBaseDir(),
					"Values":       values,
					"env":          map[string]map[string]string{},
				}

				// Pass componentEndpoints to collect endpoint info, use component type as ID
				if err := d.deployComponentTemplate(podTemplateName, tmpls, plan, initialParams, componentEndpoints, comp.ComponentType); err != nil {
					return fmt.Errorf("failed to deploy pod template %s: %w", podTemplateName, err)
				}
			}
		}
	} else {
		// If no PodTemplateExecutions defined, deploy all templates
		logger.Infof("No PodTemplateExecutions defined for %s, deploying all templates\n", componentPath)
		for templateName := range tmpls {
			// Prepare initialParams for the template
			initialParams := map[string]any{
				"InstanceSlug": generateInstanceSlug(comp.DatabaseID.String()),
				"TemplateID":   comp.DatabaseID,
				"BaseDir":      utils.GetBaseDir(),
				"Values":       values,
				"env":          map[string]map[string]string{},
			}

			// Pass componentEndpoints to collect endpoint info, use component type as ID
			if err := d.deployComponentTemplate(templateName, tmpls, plan, initialParams, componentEndpoints, comp.ComponentType); err != nil {
				return fmt.Errorf("failed to deploy pod template %s: %w", templateName, err)
			}
		}
	}

	// Store extracted endpoints in the component plan for use by services
	if len(componentEndpoints) > 0 {
		comp.Endpoints = componentEndpoints
		logger.Infof("Component %s endpoints extracted: %v\n", comp.ComponentType, componentEndpoints)
	}

	return nil
}

// deployServices deploys all services in the plan concurrently.
func (d *PodmanDeployer) deployServices(ctx context.Context, plan *DeploymentPlan) error {
	logger.Infof("Deploying %d services concurrently...\n", len(plan.Services))

	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.Services))

	for serviceID, svc := range plan.Services {
		wg.Add(1)
		go func(sID string, service *ServicePlan) {
			defer wg.Done()

			if err := d.deployService(ctx, plan, sID, service); err != nil {
				// Update service status to Error
				if service.DatabaseID != uuid.Nil {
					errMsg := fmt.Sprintf("Service deployment failed: %v", err)
					if updateErr := d.serviceRepo.UpdateStatus(ctx, service.DatabaseID, models.ServiceStatusError, errMsg); updateErr != nil {
						logger.Errorf("Failed to update service %s status: %v\n", sID, updateErr)
					}
				}
				errCh <- fmt.Errorf("failed to deploy service %s: %w", sID, err)

				return
			}

			// Update service status to Running after successful deployment
			if service.DatabaseID != uuid.Nil {
				if err := d.serviceRepo.UpdateStatus(ctx, service.DatabaseID, models.ServiceStatusRunning, "Service deployed successfully"); err != nil {
					logger.Errorf("Failed to update service %s status to Running: %v\n", sID, err)
					// Don't fail the deployment if status update fails
				}
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

	logger.Infof("All services deployed successfully\n")

	return nil
}

// deployService deploys a single service and updates its endpoint in the database.
func (d *PodmanDeployer) deployService(ctx context.Context, plan *DeploymentPlan, serviceID string, svc *ServicePlan) error {
	logger.Infof("Deploying service %s...\n", serviceID)

	// Update service status to Initializing in database
	if err := d.serviceRepo.UpdateStatus(ctx, svc.DatabaseID, models.ServiceStatusInitializing, "Deploying service"); err != nil {
		logger.Errorf("Failed to update service status to Initializing: %v\n", err)
		// Don't fail the deployment if status update fails
	}

	// Load service from catalog
	service, err := d.catalogProvider.LoadService(svc.CatalogID)
	if err != nil {
		return fmt.Errorf("failed to load service from catalog: %w", err)
	}
	logger.Infof("Service %s loaded: %s\n", service.ID, service.Name)

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
	if err := d.deployServicePods(plan.ApplicationID, svc, serviceAppMetadata, tmpls); err != nil {
		return fmt.Errorf("failed to deploy service pods: %w", err)
	}

	logger.Infof("Service %s deployed successfully\n", serviceID)

	return nil
}

// deployServicePods deploys all pods for a service.
func (d *PodmanDeployer) deployServicePods(
	applicationID uuid.UUID,
	svc *ServicePlan,
	metadata *templates.AppMetadata,
	tmpls map[string]*template.Template,
) error {
	// Use the values already loaded in the service plan
	values := svc.Values

	// If PodTemplateExecutions is defined, use it for ordered deployment
	if len(metadata.PodTemplateExecutions) > 0 {
		// Execute each pod template in the service following the defined order
		for _, layer := range metadata.PodTemplateExecutions {
			for _, podTemplateName := range layer {
				// Prepare initialParams for the template
				initialParams := map[string]any{
					"InstanceSlug": generateInstanceSlug(applicationID.String()),
					"TemplateID":   svc.DatabaseID,
					"BaseDir":      utils.GetBaseDir(),
					"Values":       values,
					"env":          map[string]map[string]string{},
				}

				_, err := d.deployPodTemplate(podTemplateName, tmpls, initialParams)
				if err != nil {
					return fmt.Errorf("failed to deploy pod template %s: %w", podTemplateName, err)
				}
			}
		}
	} else {
		// If no PodTemplateExecutions defined, deploy all templates
		logger.Infof("No PodTemplateExecutions defined for service %s, deploying all templates\n", svc.CatalogID)
		for templateName := range tmpls {
			// Prepare initialParams for the template
			initialParams := map[string]any{
				"InstanceSlug": generateInstanceSlug(applicationID.String()),
				"TemplateID":   svc.DatabaseID,
				"BaseDir":      utils.GetBaseDir(),
				"Values":       values,
				"env":          map[string]map[string]string{},
			}

			_, err := d.deployPodTemplate(templateName, tmpls, initialParams)
			if err != nil {
				return fmt.Errorf("failed to deploy pod template %s: %w", templateName, err)
			}
		}
	}

	return nil
}

// deployComponentTemplate deploys a component pod template.
// This is a generic method to deploy all component templates with Spyre card support.
// The serviceParams map is updated with the component's endpoint information (host and port).
func (d *PodmanDeployer) deployComponentTemplate(
	podTemplateName string,
	tmpls map[string]*template.Template,
	plan *DeploymentPlan,
	initialParams map[string]any,
	serviceParams map[string]any,
	componentID string,
) error {
	logger.Infof("Deploying component template '%s'...\n", podTemplateName)

	podTemplate, ok := tmpls[podTemplateName]
	if !ok {
		return fmt.Errorf("pod template '%s' not found", podTemplateName)
	}

	// Render and parse initial template
	podSpec, err := d.renderAndParsePodTemplate(podTemplate, podTemplateName, initialParams)
	if err != nil {
		return err
	}

	// Check if pod already exists
	if exists, err := d.runtime.PodExists(podSpec.Name); err != nil {
		return fmt.Errorf("failed to check pod existence: %w", err)
	} else if exists {
		logger.Infof("Pod '%s' already exists, skipping deployment\n", podSpec.Name)

		return nil
	}

	// Get environment parameters and render final template
	finalPodSpec, renderedBytes, err := d.renderFinalPodTemplate(podTemplate, podTemplateName, initialParams, podSpec, plan)
	if err != nil {
		return err
	}

	// Deploy the pod using rendered bytes directly
	if err := d.deployPodSpec(finalPodSpec, renderedBytes, podTemplateName); err != nil {
		return err
	}

	logger.Infof("Component template '%s' deployed successfully\n", podTemplateName)

	// Update service params with endpoint information
	d.updateServiceParamsWithEndpoint(serviceParams, componentID, finalPodSpec)

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
	podTemplate *template.Template,
	templateName string,
	initialParams map[string]any,
	podSpec *podmodels.PodSpec,
	plan *DeploymentPlan,
) (*podmodels.PodSpec, []byte, error) {
	env, err := d.getEnvParamsForComponent(podSpec, plan)
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
func (d *PodmanDeployer) deployPodSpec(podSpec *podmodels.PodSpec, renderedBytes []byte, templateName string) error {
	// Use the rendered bytes directly instead of marshaling PodSpec

	reader := bytes.NewReader(renderedBytes)
	podAnnotations := specs.FetchPodAnnotations(*podSpec)
	podDeployOptions := clipodman.ConstructPodDeployOptions(podAnnotations)

	if err := clipodman.DeployPodAndReadinessCheck(d.runtime, podSpec, templateName, reader, podDeployOptions); err != nil {
		return fmt.Errorf("failed to deploy pod: %w", err)
	}

	return nil
}

// updateServiceParamsWithEndpoint updates service parameters with component endpoint information.
func (d *PodmanDeployer) updateServiceParamsWithEndpoint(
	serviceParams map[string]any,
	componentID string,
	podSpec *podmodels.PodSpec,
) {
	if serviceParams == nil || componentID == "" {
		return
	}

	componentInfo, err := d.extractComponentEndpointInfo(podSpec)
	if err != nil {
		logger.Errorf("Failed to extract component endpoint info: %v\n", err)

		return
	}

	if componentInfo != nil {
		componentEndpoint := map[string]any{
			"host": componentInfo.Domain,
			"port": componentInfo.Port,
		}
		serviceParams[componentID] = componentEndpoint
		logger.Infof("Updated service params with component '%s' endpoint: %s:%s\n",
			componentID, componentInfo.Domain, componentInfo.Port)
	}
}

// extractComponentEndpointInfo extracts host (pod name) and port from a deployed pod spec.
func (d *PodmanDeployer) extractComponentEndpointInfo(podSpec *podmodels.PodSpec) (*ComponentInfo, error) {
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
		logger.Infof("No port found in pod spec for '%s'\n", host)
	}

	return &ComponentInfo{
		Domain: host,
		Port:   port,
	}, nil
}

// deployPodTemplate deploys a single pod template for a service and returns endpoint information.
func (d *PodmanDeployer) deployPodTemplate(
	podTemplateName string,
	tmpls map[string]*template.Template,
	initialParams map[string]any,
) (map[string]string, error) {
	logger.Infof("Deploying service template '%s'...\n", podTemplateName)

	podTemplate, ok := tmpls[podTemplateName]
	if !ok {
		return nil, fmt.Errorf("pod template '%s' not found", podTemplateName)
	}

	// Render template to get both PodSpec and raw bytes
	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, initialParams); err != nil {
		return nil, fmt.Errorf("failed to render template %s: %w", podTemplateName, err)
	}

	renderedBytes := rendered.Bytes()

	// Parse into PodSpec for metadata
	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(renderedBytes, &podSpec); err != nil {
		return nil, fmt.Errorf("failed to parse rendered pod spec: %w", err)
	}

	exists, err := d.runtime.PodExists(podSpec.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check pod existence: %w", err)
	}

	if exists {
		logger.Infof("Pod '%s' already exists, skipping deployment\n", podSpec.Name)

		return d.extractPodEndpoints(&podSpec), nil
	}

	// Deploy using rendered bytes directly (same as components)
	if err := d.deployPodSpec(&podSpec, renderedBytes, podTemplateName); err != nil {
		return nil, err
	}

	logger.Infof("Service template '%s' deployed successfully\n", podTemplateName)

	return d.extractPodEndpoints(&podSpec), nil
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

// getEnvParamsForComponent returns environment parameters for a component including Spyre card PCI addresses.
func (d *PodmanDeployer) getEnvParamsForComponent(podSpec *podmodels.PodSpec, plan *DeploymentPlan) (map[string]map[string]string, error) {
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

			logger.Infof("Allocated %d Spyre cards to container '%s' in pod '%s': %s\n",
				spyreCount, containerName, podSpec.Name, pciAddressStr)
		}
	}

	return env, nil
}

// generateInstanceSlug creates a short slug from an ID using SHA256 hash.
// Returns the first 10 characters of the hex-encoded hash.
func generateInstanceSlug(id string) string {
	hash := sha256.Sum256([]byte(id))
	hexHash := hex.EncodeToString(hash[:])

	return hexHash[:10]
}

// Made with Bob
