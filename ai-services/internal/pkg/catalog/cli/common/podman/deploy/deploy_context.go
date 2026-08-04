package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	clipodman "github.com/project-ai-services/ai-services/internal/pkg/cli/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/specs"
)

// DeployContext encapsulates catalog deployment context.
// Provides template access, runtime client, and deployment operations.
type DeployContext struct {
	// Core components - exported for external access
	Runtime          *podman.PodmanClient
	TemplateProvider templates.Template
	argParams        map[string]string
	values           map[string]interface{}
	appMetadata      *templates.AppMetadata
	templates        map[string]*template.Template
}

// NewDeployContext creates and initializes a new deployment context.
// This is a factory method that handles all initialization including loading templates.
func NewDeployContext() (*DeployContext, error) {
	// Initialize runtime
	rt, err := podman.NewPodmanClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize podman client: %w", err)
	}

	// Create template provider
	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	// Load metadata from templates
	var appMetadata templates.AppMetadata
	if err := tp.LoadMetadata(catalogconstants.CatalogAppTemplate, true, &appMetadata); err != nil {
		return nil, fmt.Errorf("failed to load catalog metadata: %w", err)
	}

	// Load all templates
	tmpls, err := tp.LoadAllTemplates(catalogconstants.CatalogAppTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to load catalog templates: %w", err)
	}

	return &DeployContext{
		Runtime:          rt,
		TemplateProvider: tp,
		argParams:        nil,
		appMetadata:      &appMetadata,
		templates:        tmpls,
	}, nil
}

// PrepareValues sets the application argument params and loads values.
func (d *DeployContext) PrepareValues(argParams map[string]string) error {
	// Set argParams first so it can be used in LoadValues
	d.argParams = argParams

	values, err := d.TemplateProvider.LoadValues(catalogconstants.CatalogAppTemplate, nil, d.argParams)
	if err != nil {
		return err
	}

	d.values = values

	return nil
}

// CheckStatus checks if the catalog is already deployed.
func (d *DeployContext) CheckStatus() (bool, []string, error) {
	catalogSecrets, err := collectSecretNames(d.TemplateProvider, d.argParams)
	if err != nil {
		return false, nil, err
	}

	existingResources, err := helpers.CheckExistingResourcesForApplication(context.Background(), d.Runtime, catalogconstants.CatalogAppName, catalogSecrets)
	if err != nil {
		return false, nil, err
	}

	return len(existingResources) == len(d.templates), existingResources, nil
}

// GetCaddyPodName extracts the Caddy pod name from templates.
func (d *DeployContext) GetCaddyPodName() (string, error) {
	return findCaddyPodNameFromTemplates(d.TemplateProvider, d.argParams)
}

// ExtractRouteInfos extracts route information from all templates.
func (d *DeployContext) ExtractRouteInfos() ([]caddy.TemplateRouteInfo, error) {
	return extractAllRoutesFromTemplates(d.TemplateProvider, d.argParams)
}

// ExecutePodLayers executes all pod template layers.
func (d *DeployContext) ExecutePodLayers(baseDir string, caddyCtx *caddy.Context,
	existingResources []string) error {
	logger.Debugln("executing catalog service resources...")

	for i, layer := range d.appMetadata.PodTemplateExecutions {
		logger.Infof("\n Executing Layer %d/%d: %v\n", i+1, len(d.appMetadata.PodTemplateExecutions), layer)
		logger.Infoln("-------")

		if err := d.executeLayer(layer, baseDir, caddyCtx, i, existingResources); err != nil {
			return err
		}

		logger.Infof("Layer %d completed\n", i+1)
	}

	return nil
}

// executeLayer executes a single layer of pod templates.
func (d *DeployContext) executeLayer(layer []string, baseDir string, caddyCtx *caddy.Context,
	layerIndex int, existingResources []string) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(layer))

	// for each layer, fetch all the pod Template Names and do the pod deploy
	for _, podTemplateName := range layer {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			if err := d.executePodTemplate(t, baseDir, caddyCtx, existingResources); err != nil {
				errCh <- err
			}
		}(podTemplateName)
	}

	wg.Wait()
	close(errCh)

	// collect all errors for this layer
	errs := make([]error, 0, len(layer))
	for e := range errCh {
		errs = append(errs, fmt.Errorf("layer %d: %w", layerIndex+1, e))
	}

	// If an error exist for a given layer, then return (do not process further layers)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// executePodTemplate executes a single pod template.
func (d *DeployContext) executePodTemplate(podTemplateName string,
	baseDir string, caddyCtx *caddy.Context, existingResources []string) error {
	logger.Infof("Processing template: %s\n", catalogconstants.CatalogAppTemplate)

	// Fetch pod spec
	podSpec, err := d.TemplateProvider.LoadPodTemplateWithValues(catalogconstants.CatalogAppTemplate, podTemplateName, catalogconstants.CatalogAppName, nil, d.argParams)
	if err != nil {
		return fmt.Errorf("failed to load pod template: %w", err)
	}

	// Generate template parameters
	params := map[string]any{
		"AppName":         catalogconstants.CatalogAppName,
		"AppTemplateName": catalogconstants.CatalogAppTemplate,
		"Version":         d.appMetadata.Version,
		"BaseDir":         baseDir,
		"CaddyAdminURL":   caddyCtx.GetContainerAdminURL(),
		"DomainSuffix":    caddyCtx.GetDomainSuffix(),
		"Values":          d.values,
		"env":             map[string]map[string]string{},
	}

	// filter out resources
	if slices.Contains(existingResources, podSpec.Name) {
		logger.Infof("%s: Skipping resource deploy as '%s' it already exists", podTemplateName, podSpec.Name)

		return nil
	}

	// Get the template
	podTemplate := d.templates[podTemplateName]

	// Render template
	var rendered bytes.Buffer
	if err := podTemplate.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to render pod template: %w", err)
	}

	// Deploy the pod with readiness checks
	reader := bytes.NewReader(rendered.Bytes())
	podDeployOptions := clipodman.ConstructPodDeployOptions(specs.FetchPodAnnotations(*podSpec))

	if err := clipodman.DeployPodAndReadinessCheck(context.Background(), d.Runtime, podSpec, podTemplateName, reader, podDeployOptions); err != nil {
		return fmt.Errorf("failed to deploy pod: %w", err)
	}

	return nil
}

// Made with Bob
