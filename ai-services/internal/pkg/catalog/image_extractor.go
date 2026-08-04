package catalog

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// GetCatalogImages collects all unique images from a service or architecture template.
// This is the main entry point for catalog-based image collection from CLI or API.
func (p *CatalogProvider) GetCatalogImages(ctx context.Context, templateID string) ([]string, error) {
	allImages := make(map[string]bool)

	// Include catalog asset images (tool image used for housekeeping tasks)
	allImages[vars.ToolImage] = true

	// Include catalog infrastructure images (catalog service itself)
	if err := p.addCatalogInfrastructureImages(ctx, allImages); err != nil {
		logger.ErrorfCtx(ctx, "Failed to collect catalog infrastructure images: %v", err)
	}

	// Try to load as architecture first
	arch, err := p.LoadArchitecture(templateID)
	if err == nil {
		if err := p.collectArchitectureImages(ctx, arch.Services, allImages); err != nil {
			return nil, err
		}

		return utils.ExtractMapKeys(allImages), nil
	}

	// Try to load as service
	service, err := p.LoadService(templateID)
	if err == nil {
		if err := p.collectServiceWithDependencies(ctx, templateID, service.Dependencies, allImages); err != nil {
			return nil, err
		}

		return utils.ExtractMapKeys(allImages), nil
	}

	return nil, fmt.Errorf("template '%s' not found as service or architecture", templateID)
}

// addCatalogInfrastructureImages adds images from the catalog service templates.
func (p *CatalogProvider) addCatalogInfrastructureImages(ctx context.Context, allImages map[string]bool) error {
	// Use the embed template provider to load catalog templates and values
	// Similar to loadCatalogTemplates() in configure.go
	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	// Load catalog values (no overrides needed for image collection)
	values, err := tp.LoadValues(constants.CatalogAppTemplate, nil, nil)
	if err != nil {
		return fmt.Errorf("failed to load catalog values: %w", err)
	}

	// Load catalog templates
	catalogTemplates, err := tp.LoadAllTemplates(constants.CatalogAppTemplate)
	if err != nil {
		return fmt.Errorf("failed to load catalog templates: %w", err)
	}

	// Collect images from catalog templates
	if err := p.CollectImagesFromTemplates(ctx, catalogTemplates, values, allImages); err != nil {
		return fmt.Errorf("failed to collect images from catalog templates: %w", err)
	}

	return nil
}

// collectArchitectureImages collects all images for all services in an architecture.
func (p *CatalogProvider) collectArchitectureImages(ctx context.Context, services []types.ServiceReference, allImages map[string]bool) error {
	for _, svcRef := range services {
		service, err := p.LoadService(svcRef.ID)
		if err != nil {
			return fmt.Errorf("failed to load service %s: %w", svcRef.ID, err)
		}

		// Reuse collectServiceWithDependencies to collect both service and component images
		if err := p.collectServiceWithDependencies(ctx, svcRef.ID, service.Dependencies, allImages); err != nil {
			return fmt.Errorf("failed to collect images for service %s: %w", svcRef.ID, err)
		}
	}

	return nil
}

// collectServiceWithDependencies collects images for a specific service and its dependencies.
func (p *CatalogProvider) collectServiceWithDependencies(ctx context.Context, serviceID string, dependencies []types.DependencyReference, allImages map[string]bool) error {
	// Get service images
	if err := p.addServiceImages(ctx, serviceID, allImages); err != nil {
		return err
	}

	// Get component images
	return p.collectComponentsImages(ctx, dependencies, allImages, nil)
}

// addServiceImages adds service images to the provided map.
func (p *CatalogProvider) addServiceImages(ctx context.Context, serviceID string, allImages map[string]bool) error {
	values, err := p.LoadServiceValues(serviceID, map[string]string{})
	if err != nil {
		return fmt.Errorf("failed to load values for service %s: %w", serviceID, err)
	}

	templates, err := p.LoadServiceTemplates(serviceID)
	if err != nil {
		return fmt.Errorf("failed to load service templates: %w", err)
	}

	if err := p.CollectImagesFromTemplates(ctx, templates, values, allImages); err != nil {
		return fmt.Errorf("failed to collect images from service templates: %w", err)
	}

	return nil
}

// collectComponentsImages collects images for components based on dependencies.
// If displayedComponents map is provided, it will track and skip duplicates.
func (p *CatalogProvider) collectComponentsImages(ctx context.Context, dependencies []types.DependencyReference, allImages map[string]bool, displayedComponents map[string]bool) error {
	if len(dependencies) == 0 {
		return nil
	}

	components, err := p.ListComponents()
	if err != nil {
		return fmt.Errorf("failed to list components: %w", err)
	}

	for _, dep := range dependencies {
		if err := p.collectComponentsByType(ctx, dep.ID, components, allImages, displayedComponents); err != nil {
			return err
		}
	}

	return nil
}

// collectComponentsByType collects images for all components of a specific type.
func (p *CatalogProvider) collectComponentsByType(ctx context.Context, componentType string, components []types.Component, allImages map[string]bool, displayedComponents map[string]bool) error {
	for _, comp := range components {
		if comp.ComponentType != componentType {
			continue
		}

		componentKey := fmt.Sprintf("%s.%s", comp.ComponentType, comp.ID)

		// Skip if already processed (only when tracking duplicates)
		if displayedComponents != nil && displayedComponents[componentKey] {
			continue
		}

		if displayedComponents != nil {
			displayedComponents[componentKey] = true
		}

		if err := p.addComponentImages(ctx, comp.ComponentType, comp.ID, allImages); err != nil {
			return fmt.Errorf("failed to collect images for component %s/%s: %w", comp.ComponentType, comp.ID, err)
		}
	}

	return nil
}

// addComponentImages adds component images to the provided map.
func (p *CatalogProvider) addComponentImages(ctx context.Context, componentType, componentID string, allImages map[string]bool) error {
	values, err := p.LoadComponentValues(componentType, componentID, map[string]string{})
	if err != nil {
		return fmt.Errorf("failed to load values for component %s/%s: %w", componentType, componentID, err)
	}

	templates, err := p.LoadComponentTemplates(componentType, componentID)
	if err != nil {
		return fmt.Errorf("failed to load component templates: %w", err)
	}

	if err := p.CollectImagesFromTemplates(ctx, templates, values, allImages); err != nil {
		return fmt.Errorf("failed to collect images from component templates: %w", err)
	}

	return nil
}

// Made with Bob
