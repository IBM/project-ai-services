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
	k8syaml "sigs.k8s.io/yaml"
)

// ComponentInfo holds the information derived from a deployed component
type ComponentInfo struct {
	Endpoint string
	Domain   string
	Port     string
	Model    string
}

// SpyreCardPool manages allocation of PCI addresses to components
type SpyreCardPool struct {
	addresses []string
	mutex     sync.Mutex
}

// Allocate takes n addresses from the pool and returns them
func (p *SpyreCardPool) Allocate(n int) ([]string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.addresses) < n {
		return nil, fmt.Errorf("insufficient Spyre cards in pool: need %d, have %d", n, len(p.addresses))
	}

	allocated := make([]string, n)
	copy(allocated, p.addresses[:n])
	p.addresses = p.addresses[n:]

	return allocated, nil
}

// Type aliases for deployment plan types
type (
	DeploymentPlan = deploymenttypes.DeploymentPlan
	ComponentPlan  = deploymenttypes.ComponentPlan
	ServicePlan    = deploymenttypes.ServicePlan
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

// ExecuteDeployment executes the deployment plan for an application.
// This implements Phase 4 of the deployment flow:
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
	logger.Infof("Starting deployment execution for application '%s'\n", plan.ApplicationName)

	// Step 0: Pull container images for all components and services
	// if err := d.pullImagesForDeployment(plan); err != nil {
	// 	d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Image pull failed: %v", err))
	// 	return fmt.Errorf("failed to pull container images: %w", err)
	// }

	// Step 1: Download models specified in parameters
	if err := d.downloadModelsForDeployment(plan); err != nil {
		d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Model download failed: %v", err))
		return fmt.Errorf("failed to download models: %w", err)
	}

	// Step 2: Calculate and allocate Spyre cards if needed
	pool, err := d.calculateAndAllocateSpyreCards(plan)
	if err != nil {
		d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Spyre card allocation failed: %v", err))
		return fmt.Errorf("failed to allocate Spyre cards: %w", err)
	}

	// Update application status to Deploying before starting component deployment
	if err := d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusDeploying, "Starting component and service deployment"); err != nil {
		logger.Errorf("Failed to update application status to Deploying: %v\n", err)
		// Don't fail the deployment if status update fails
	}

	// Step 3: Deploy components and update their endpoints
	if err := d.deployComponents(ctx, plan, pool); err != nil {
		// Update application status to Error
		d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Component deployment failed: %v", err))
		return fmt.Errorf("failed to deploy components: %w", err)
	}

	// Step 5: Deploy services with component references
	if err := d.deployServices(ctx, plan); err != nil {
		// Update application status to Error
		d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Service deployment failed: %v", err))
		return fmt.Errorf("failed to deploy services: %w", err)
	}

	// Step 6: Update application status to Running
	if err := d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusRunning, "Deployment completed successfully"); err != nil {
		logger.Errorf("Failed to update application status: %v\n", err)
		// Don't fail the deployment if status update fails
	}

	logger.Infof("Deployment completed successfully for application '%s'\n", plan.ApplicationName)
	return nil
}

// ExecuteServiceDeployment executes the deployment plan for a standalone service.
// This is a simplified version of ExecuteDeployment that focuses on deploying a single service
// with its components. It follows the same deployment phases but is optimized for service-only deployments.
//
// Deployment phases:
// 1. Download models specified in component/service parameters
// 2. Calculate and allocate Spyre cards if needed
// 3. Deploy components
// 4. Deploy the service
// 5. Update database with endpoints and final status
//
// Note: Service and component records are already created by ApplicationService
// before this method is called. This method only updates endpoints and status.
func (d *PodmanDeployer) ExecuteServiceDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	req apimodels.CreateApplicationRequest,
) error {
	logger.Infof("Starting service deployment execution for '%s'\n", plan.ApplicationName)

	// Step 1: Download models specified in parameters
	if err := d.downloadModelsForDeployment(plan); err != nil {
		d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Model download failed: %v", err))
		return fmt.Errorf("failed to download models: %w", err)
	}

	// Step 2: Calculate and allocate Spyre cards if needed
	pool, err := d.calculateAndAllocateSpyreCards(plan)
	if err != nil {
		d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Spyre card allocation failed: %v", err))
		return fmt.Errorf("failed to allocate Spyre cards: %w", err)
	}

	// Update application status to Deploying
	if err := d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusDeploying, "Starting service deployment"); err != nil {
		logger.Errorf("Failed to update application status to Deploying: %v\n", err)
	}

	// Step 3: Deploy components if any
	if len(plan.Components) > 0 {
		logger.Infof("Deploying %d components for service...\n", len(plan.Components))
		if err := d.deployComponents(ctx, plan, pool); err != nil {
			d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Component deployment failed: %v", err))
			return fmt.Errorf("failed to deploy components: %w", err)
		}
	}

	// Step 4: Deploy the service
	if len(plan.Services) > 0 {
		logger.Infof("Deploying service...\n")
		if err := d.deployServices(ctx, plan); err != nil {
			d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusError, fmt.Sprintf("Service deployment failed: %v", err))
			return fmt.Errorf("failed to deploy service: %w", err)
		}
	}

	// Step 5: Update application status to Running
	if err := d.updateApplicationStatus(ctx, plan.ApplicationID, models.ApplicationStatusRunning, "Service deployment completed successfully"); err != nil {
		logger.Errorf("Failed to update application status: %v\n", err)
	}

	logger.Infof("Service deployment completed successfully for '%s'\n", plan.ApplicationName)
	return nil
}

// updateApplicationStatus updates the application status and message in the database.
func (d *PodmanDeployer) updateApplicationStatus(ctx context.Context, appID uuid.UUID, status models.ApplicationStatus, message string) error {
	if err := d.appRepo.UpdateStatus(ctx, appID, status, message); err != nil {
		logger.Errorf("Failed to update application status in database: %v", err)
		return fmt.Errorf("failed to update application status: %w", err)
	}
	logger.Infof("Application %s status updated: %s - %s", appID, status, message)
	return nil
}

// pullImagesForDeployment pulls all container images required for components and services.
// Uses image.PullIfNotPresent policy to avoid unnecessary downloads.
func (d *PodmanDeployer) pullImagesForDeployment(plan *DeploymentPlan) error {
	logger.Infof("Pulling container images for application '%s'\n", plan.ApplicationName)

	// Collect all unique template paths for components and services
	templatePaths := make(map[string]bool)

	// Add component template paths
	for _, comp := range plan.Components {
		// Use dynamic catalog path from component plan
		templatePaths[comp.CatalogPath] = true
	}

	// Add service template paths
	for _, svc := range plan.Services {
		// Use dynamic catalog path from service plan
		templatePaths[svc.CatalogPath] = true
	}

	// Pull images for each unique template path
	for templatePath := range templatePaths {
		logger.Infof("Pulling images for template: %s\n", templatePath)

		img := &image.Images{
			Runtime:     d.runtime,
			App:         plan.ApplicationName,
			AppTemplate: templatePath,
		}

		// Use PullIfNotPresent policy to avoid unnecessary downloads
		if err := img.Run(image.PullIfNotPresent); err != nil {
			return fmt.Errorf("failed to pull images for template %s: %w", templatePath, err)
		}
	}

	logger.Infof("Successfully pulled all container images for application '%s'\n", plan.ApplicationName)
	return nil
}

// downloadModelsForDeployment downloads all models specified in component and service parameters.
// Models are extracted from params that contain "model" in their key name.
func (d *PodmanDeployer) downloadModelsForDeployment(plan *DeploymentPlan) error {
	logger.Infof("Downloading models for application '%s'\n", plan.ApplicationName)

	// Collect all unique model names from components and services
	modelSet := make(map[string]bool)

	// Extract models from component params
	for _, comp := range plan.Components {
		for key, value := range comp.Params {
			// Look for params with "model" in the key name
			if strings.Contains(strings.ToLower(key), "model") {
				if modelName, ok := value.(string); ok && modelName != "" {
					modelSet[modelName] = true
				}
			}
		}
	}

	// Extract models from service params (if services have model params)
	for _, svc := range plan.Services {
		if svc.Values != nil {
			// Check if Values contains model information
			if modelVal, ok := svc.Values["model"]; ok {
				if modelName, ok := modelVal.(string); ok && modelName != "" {
					modelSet[modelName] = true
				}
			}
		}
	}

	// If no models found, skip download
	if len(modelSet) == 0 {
		logger.Infof("No models to download for application '%s'\n", plan.ApplicationName)
		return nil
	}

	// Download each unique model
	modelsPath := utils.GetModelsPath()
	for modelName := range modelSet {
		logger.Infof("Downloading model: %s\n", modelName)

		if err := helpers.DownloadModelContainer(modelName, modelsPath); err != nil {
			return fmt.Errorf("failed to download model %s: %w", modelName, err)
		}
	}

	logger.Infof("Successfully downloaded all models for application '%s'\n", plan.ApplicationName)
	return nil
}

// deployComponents deploys all components concurrently.
// All components are treated as shared and deployed together.
func (d *PodmanDeployer) deployComponents(ctx context.Context, plan *DeploymentPlan, pool *SpyreCardPool) error {
	// Deploy all components concurrently
	logger.Infof("Deploying %d components concurrently...\n", len(plan.Components))
	if err := d.deployComponentsConcurrently(ctx, plan.Components, pool, plan); err != nil {
		return fmt.Errorf("failed to deploy components: %w", err)
	}

	logger.Infof("All components deployed successfully\n")
	return nil
}

// deployComponentsConcurrently deploys multiple components concurrently using goroutines.
func (d *PodmanDeployer) deployComponentsConcurrently(ctx context.Context, components map[string]*ComponentPlan, pool *SpyreCardPool, plan *DeploymentPlan) error {
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
			if err := d.deployComponent(ctx, h, c, pool, plan, &mu); err != nil {
				errChan <- fmt.Errorf("failed to deploy component %s: %w", h, err)
			}
		}(hash, comp)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check for any errors
	var errs []error
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
func (d *PodmanDeployer) deployComponent(ctx context.Context, hash string, comp *ComponentPlan, pool *SpyreCardPool, plan *DeploymentPlan, mu *sync.Mutex) error {
	logger.Infof("Deploying component %s (%s/%s)...\n", comp.ComponentType, comp.ProviderID, hash)

	// Load component from catalog
	component, err := d.catalogProvider.LoadComponent(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to load component from catalog: %w", err)
	}
	logger.Infof("Component %s loaded: %s\n", component.ID, component.Name)

	// Load runtime-specific metadata (contains PodTemplateExecutions)
	metadata, err := d.catalogProvider.LoadComponentRuntimeMetadata(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to load component runtime metadata: %w", err)
	}

	// Load component templates
	tmpls, err := d.catalogProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to load component templates: %w", err)
	}

	// Use dynamic catalog path from component plan
	componentPath := comp.CatalogPath

	// Deploy component pods
	if err := d.deployComponentPods(comp, metadata, tmpls, componentPath, pool); err != nil {
		return fmt.Errorf("failed to deploy component pods: %w", err)
	}

	// After successful deployment, merge component endpoints into all services that use this component
	if len(comp.Endpoints) > 0 {
		// Use mutex to protect concurrent writes to service Values maps
		mu.Lock()
		defer mu.Unlock()

		for _, serviceID := range comp.UsedByServices {
			if svc, ok := plan.Services[serviceID]; ok {
				logger.Infof("Service %s Values before merge: %v\n", serviceID, svc.Values)
				if svc.Values == nil {
					svc.Values = make(map[string]interface{})
				}
				// Merge component endpoints under the component type key
				if endpointData, ok := comp.Endpoints[comp.ComponentType]; ok {
					// Check if the component type key already exists in service Values
					if existingData, exists := svc.Values[comp.ComponentType]; exists {
						// If key is present and is a map, update host and port values
						if existingMap, isMap := existingData.(map[string]any); isMap {
							if endpointMap, isEndpointMap := endpointData.(map[string]any); isEndpointMap {
								// Update host and port in the existing map (preserving model, image, apiKey, etc.)
								maps.Copy(existingMap, endpointMap)
								logger.Infof("Updated component %s host/port in service %s\n", comp.ComponentType, serviceID)
							}
						} else {
							// If not a map, replace it
							svc.Values[comp.ComponentType] = endpointData
						}
					} else {
						// If key doesn't exist, create it
						svc.Values[comp.ComponentType] = endpointData
					}
				} else {
					logger.Errorf("Component %s endpoint data not found in comp.Endpoints map\n", comp.ComponentType)
				}
			}
		}
	} else {
		logger.Infof("Component %s has no endpoints to merge\n", comp.ComponentType)
	}

	logger.Infof("Component %s deployed successfully\n", comp.ComponentType)
	return nil
}

// deployComponentPods deploys all pods for a component and extracts endpoint information.
func (d *PodmanDeployer) deployComponentPods(
	comp *ComponentPlan,
	metadata *templates.AppMetadata,
	tmpls map[string]*template.Template,
	componentPath string,
	pool *SpyreCardPool,
) error {
	// Use the loaded Values from the component plan (includes defaults from values.yaml + overrides)
	// If Values is not set, fall back to ArgParams for backward compatibility
	values := comp.Values
	if values == nil {
		values = make(map[string]any)
		for k, v := range comp.ArgParams {
			values[k] = v
		}
	}

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
				if err := d.deployComponentTemplate(podTemplateName, tmpls, pool, initialParams, componentEndpoints, comp.ComponentType); err != nil {
					return fmt.Errorf("failed to deploy pod template %s: %w", podTemplateName, err)
				}
			}
		}
	} else {
		// If no PodTemplateExecutions defined, deploy all templates
		logger.Infof("No PodTemplateExecutions defined for %s, deploying all templates\n", componentPath)
		for templateName := range tmpls {
			// Prepare initialParams for the template
			initialParams := map[string]interface{}{
				"InstanceSlug": generateInstanceSlug(comp.DatabaseID.String()),
				"TemplateID":   comp.DatabaseID,
				"BaseDir":      utils.GetBaseDir(),
				"Values":       values,
				"env":          map[string]map[string]string{},
			}

			// Pass componentEndpoints to collect endpoint info, use component type as ID
			if err := d.deployComponentTemplate(templateName, tmpls, pool, initialParams, componentEndpoints, comp.ComponentType); err != nil {
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
					if updateErr := d.serviceRepo.UpdateStatus(ctx, service.DatabaseID, models.ApplicationStatusError); updateErr != nil {
						logger.Errorf("Failed to update service %s status: %v\n", sID, updateErr)
					}
				}
				errCh <- fmt.Errorf("failed to deploy service %s: %w", sID, err)
				return
			}

			// Update service status to Running after successful deployment
			if service.DatabaseID != uuid.Nil {
				if err := d.serviceRepo.UpdateStatus(ctx, service.DatabaseID, models.ApplicationStatusRunning); err != nil {
					logger.Errorf("Failed to update service %s status to Running: %v\n", sID, err)
					// Don't fail the deployment if status update fails
				}
			}
		}(serviceID, svc)
	}

	wg.Wait()
	close(errCh)

	// Collect all errors
	var errs []error
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

	// Update service status to Deploying in database
	if err := d.serviceRepo.UpdateStatus(ctx, svc.DatabaseID, models.ApplicationStatusDeploying); err != nil {
		logger.Errorf("Failed to update service status to Deploying: %v\n", err)
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

	// Extract external service endpoints (HostIP + routes)
	endpoints, err := d.extractServiceEndpoints(ctx, "")
	if err != nil {
		logger.Errorf("Failed to extract service endpoints: %v\n", err)
		// Don't fail deployment if endpoint extraction fails
		endpoints = map[string]any{}
	}

	// Update service endpoints in database
	if svc.DatabaseID != uuid.Nil && len(endpoints) > 0 {
		if err := d.serviceRepo.UpdateEndpoints(ctx, svc.DatabaseID, endpoints); err != nil {
			logger.Errorf("Failed to update service endpoints in database: %v\n", err)
			// Don't fail the deployment if endpoint update fails
		} else {
			logger.Infof("Service endpoints updated in database: %v\n", endpoints)
		}
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
	logger.Infof("Service %s Values before template rendering: %v\n", svc.CatalogID, values)

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
			initialParams := map[string]interface{}{
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

// extractServiceEndpoints extracts external service endpoints for Podman by inspecting the pod.
// Returns endpoints in format: {type: "external", name: "pod-name", endpoint: "hostIP:hostPort"}
func (d *PodmanDeployer) extractServiceEndpoints(ctx context.Context, serviceInstanceName string) (map[string]any, error) {
	// Get host IP
	hostIP, err := utils.GetHostIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get host IP: %w", err)
	}

	// Check if pod exists
	exists, err := d.runtime.PodExists(serviceInstanceName)
	if err != nil {
		return nil, fmt.Errorf("failed to check if pod exists: %w", err)
	}
	if !exists {
		logger.Infof("Pod %s does not exist yet\n", serviceInstanceName)
		return map[string]any{}, nil
	}

	// Inspect the pod to get port mappings
	podInfo, err := d.runtime.InspectPod(serviceInstanceName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect pod %s: %w", serviceInstanceName, err)
	}

	// Extract the first host port from the Ports map
	// Ports is map[string][]string where key is "containerPort/protocol" and value is list of host ports
	// Example: {"8080/tcp": ["37249"]}
	var hostPort string
	for _, hostPorts := range podInfo.Ports {
		if len(hostPorts) > 0 {
			hostPort = hostPorts[0]
			break
		}
	}

	if hostPort == "" {
		logger.Infof("No port mappings found for service %s\n", serviceInstanceName)
		return map[string]any{}, nil
	}

	// Build external endpoint
	endpoints := map[string]any{
		"type":     "external",
		"name":     serviceInstanceName,
		"endpoint": fmt.Sprintf("%s:%s", hostIP, hostPort),
	}

	return endpoints, nil
}

// deployComponentTemplate deploys a component pod template.
// This is a generic method to deploy all component templates with Spyre card support.
// The serviceParams map is updated with the component's endpoint information (host and port).
func (d *PodmanDeployer) deployComponentTemplate(
	podTemplateName string,
	tmpls map[string]*template.Template,
	pool *SpyreCardPool,
	initialParams map[string]any,
	serviceParams map[string]interface{},
	componentID string,
) error {
	logger.Infof("Deploying component template '%s'...\n", podTemplateName)

	// Get the pod template
	podTemplate, ok := tmpls[podTemplateName]
	if !ok {
		return fmt.Errorf("pod template '%s' not found", podTemplateName)
	}

	// Use the provided initialParams for first render
	var initialRendered bytes.Buffer
	if err := podTemplate.Execute(&initialRendered, initialParams); err != nil {
		return fmt.Errorf("failed to render template %s: %w", podTemplateName, err)
	}

	// Parse rendered YAML to get pod spec
	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(initialRendered.Bytes(), &podSpec); err != nil {
		return fmt.Errorf("failed to parse rendered pod spec: %w", err)
	}

	// Check if pod already exists
	exists, err := d.runtime.PodExists(podSpec.Name)
	if err != nil {
		return fmt.Errorf("failed to check pod existence: %w", err)
	}

	if exists {
		logger.Infof("Pod '%s' already exists, skipping deployment\n", podSpec.Name)
		return nil
	}

	// Get env params for this pod (including Spyre card PCI addresses if needed)
	env, err := d.getEnvParamsForComponent(&podSpec, pool)
	if err != nil {
		return fmt.Errorf("failed to get env params: %w", err)
	}

	// Append env to initialParams if not already present
	if _, exists := initialParams["env"]; !exists {
		initialParams["env"] = env
	}

	// Second render: with complete params including env
	var finalRendered bytes.Buffer
	if err := podTemplate.Execute(&finalRendered, initialParams); err != nil {
		return fmt.Errorf("failed to render template %s with env: %w", podTemplateName, err)
	}

	// Parse final rendered YAML to get complete pod spec
	var finalPodSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(finalRendered.Bytes(), &finalPodSpec); err != nil {
		return fmt.Errorf("failed to parse final rendered pod spec: %w", err)
	}

	// Deploy the pod
	reader := bytes.NewReader(finalRendered.Bytes())
	podAnnotations := specs.FetchPodAnnotations(finalPodSpec)
	podDeployOptions := clipodman.ConstructPodDeployOptions(podAnnotations)

	if err := clipodman.DeployPodAndReadinessCheck(d.runtime, &finalPodSpec, podTemplateName, reader, podDeployOptions); err != nil {
		return fmt.Errorf("failed to deploy pod: %w", err)
	}

	logger.Infof("Component template '%s' deployed successfully\n", podTemplateName)

	// Update service params with component endpoint information if provided
	if serviceParams != nil && componentID != "" {
		// Extract endpoint information from the deployed pod
		componentInfo, err := d.extractComponentEndpointInfo(&finalPodSpec)
		if err != nil {
			logger.Errorf("Failed to extract component endpoint info: %v\n", err)
			// Don't fail deployment if endpoint extraction fails
			return nil
		}

		if componentInfo != nil {
			// Create a nested map for this component's endpoint info
			componentEndpoint := map[string]interface{}{
				"host": componentInfo.Domain,
				"port": componentInfo.Port,
			}
			serviceParams[componentID] = componentEndpoint
			logger.Infof("Updated service params with component '%s' endpoint: %s:%s\n",
				componentID, componentInfo.Domain, componentInfo.Port)
		}
	}

	return nil
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

	// Get the pod template
	podTemplate, ok := tmpls[podTemplateName]
	if !ok {
		return nil, fmt.Errorf("pod template '%s' not found", podTemplateName)
	}

	// Use the provided initialParams for rendering
	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, initialParams); err != nil {
		return nil, fmt.Errorf("failed to render template %s: %w", podTemplateName, err)
	}

	// Parse rendered YAML to get pod spec
	var podSpec podmodels.PodSpec
	if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
		return nil, fmt.Errorf("failed to parse rendered pod spec: %w", err)
	}

	// Check if pod already exists
	exists, err := d.runtime.PodExists(podSpec.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check pod existence: %w", err)
	}

	if exists {
		logger.Infof("Pod '%s' already exists, skipping deployment\n", podSpec.Name)
		// Extract endpoints from existing pod
		// Extract endpoints from existing pod - use pod name as host
		endpoints := make(map[string]string)
		endpoints["host"] = podSpec.Name
		if len(podSpec.Spec.Containers) > 0 && len(podSpec.Spec.Containers[0].Ports) > 0 {
			endpoints["port"] = fmt.Sprintf("%d", podSpec.Spec.Containers[0].Ports[0].ContainerPort)
		}
		return endpoints, nil
	}

	// Deploy the pod
	reader := bytes.NewReader(rendered.Bytes())
	podAnnotations := specs.FetchPodAnnotations(podSpec)
	podDeployOptions := clipodman.ConstructPodDeployOptions(podAnnotations)

	if err := clipodman.DeployPodAndReadinessCheck(d.runtime, &podSpec, podTemplateName, reader, podDeployOptions); err != nil {
		return nil, fmt.Errorf("failed to deploy pod: %w", err)
	}

	logger.Infof("Service template '%s' deployed successfully\n", podTemplateName)

	// Extract endpoint information from deployed pod
	// Extract endpoint information - use pod name as host
	endpoints := make(map[string]string)
	endpoints["host"] = podSpec.Name
	if len(podSpec.Spec.Containers) > 0 && len(podSpec.Spec.Containers[0].Ports) > 0 {
		endpoints["port"] = fmt.Sprintf("%d", podSpec.Spec.Containers[0].Ports[0].ContainerPort)
	}
	return endpoints, nil
}

// calculateAndAllocateSpyreCards calculates required Spyre cards and creates allocation pool
func (d *PodmanDeployer) calculateAndAllocateSpyreCards(
	plan *DeploymentPlan,
) (*SpyreCardPool, error) {
	totalRequired := 0

	// Calculate total required Spyre cards from all components
	for _, comp := range plan.Components {
		required, err := d.getRequiredSpyreCardsForComponent(comp)
		if err != nil {
			return nil, fmt.Errorf("failed to get Spyre card requirements for component %s: %w", comp.ComponentType, err)
		}
		totalRequired += required
		if required > 0 {
			logger.Infof("Component %s/%s requires %d Spyre cards\n", comp.ComponentType, comp.ProviderID, required)
		}
	}

	if totalRequired == 0 {
		logger.Infof("No Spyre cards required for this deployment\n")
		return nil, nil
	}

	logger.Infof("Total Spyre cards required: %d\n", totalRequired)

	// Find available Spyre cards
	pciAddresses, err := helpers.FindFreeSpyreCards()
	if err != nil {
		return nil, fmt.Errorf("failed to find free Spyre cards: %w", err)
	}

	availableCount := len(pciAddresses)
	logger.Infof("Available Spyre cards: %d\n", availableCount)

	// Validate we have enough Spyre cards
	if availableCount < totalRequired {
		return nil, fmt.Errorf("insufficient Spyre cards: required %d, available %d", totalRequired, availableCount)
	}

	// Create pool with available addresses
	pool := &SpyreCardPool{
		addresses: pciAddresses,
	}

	return pool, nil
}

// getRequiredSpyreCardsForComponent calculates Spyre cards needed for a component
func (d *PodmanDeployer) getRequiredSpyreCardsForComponent(comp *ComponentPlan) (int, error) {
	// Load component templates using catalog provider
	tmpls, err := d.catalogProvider.LoadComponentTemplates(comp.ComponentType, comp.ProviderID)
	if err != nil {
		return 0, fmt.Errorf("failed to load component templates: %w", err)
	}

	totalSpyreCards := 0

	for templateName, tmpl := range tmpls {
		// Prepare minimal params for rendering
		params := map[string]any{
			"InstanceSlug": generateInstanceSlug(comp.DatabaseID.String()),
			"TemplateID":   comp.DatabaseID,
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
		spyreCards, _, err := d.fetchSpyreCardsFromPodAnnotations(podSpec.Annotations)
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

// fetchSpyreCardsFromPodAnnotations extracts Spyre card requirements from pod annotations
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

// getEnvParamsForComponent returns environment parameters for a component including Spyre card PCI addresses
func (d *PodmanDeployer) getEnvParamsForComponent(podSpec *podmodels.PodSpec, pool *SpyreCardPool) (map[string]map[string]string, error) {
	env := make(map[string]map[string]string)

	// Get container names from pod spec
	for _, container := range podSpec.Spec.Containers {
		env[container.Name] = make(map[string]string)
	}

	if pool == nil {
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
			allocatedAddresses, err := pool.Allocate(spyreCount)
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
// Returns the first 8 characters of the hex-encoded hash.
func generateInstanceSlug(id string) string {
	hash := sha256.Sum256([]byte(id))
	hexHash := hex.EncodeToString(hash[:])
	return hexHash[:8]
}

// Made with Bob
