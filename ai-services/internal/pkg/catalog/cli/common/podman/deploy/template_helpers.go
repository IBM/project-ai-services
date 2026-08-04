package deploy

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
)

const (
	kindSecret = "Secret"
)

// processPodTemplates loads all templates and processes each pod spec with the provided callback.
// This helper function eliminates duplicate template loading and iteration logic.
func processPodTemplates(tp templates.Template, argParams map[string]string,
	processor func(podSpec *models.PodSpec) error) error {
	// Load all templates once
	tmpls, err := tp.LoadAllTemplates(catalogconstants.CatalogAppTemplate)
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Iterate through templates and process each pod spec
	for templateName := range tmpls {
		podSpec, err := tp.LoadPodTemplateWithValues(catalogconstants.CatalogAppTemplate, templateName,
			catalogconstants.CatalogAppName, nil, argParams)
		if err != nil {
			return fmt.Errorf("failed to load template %s: %w", templateName, err)
		}

		if err := processor(podSpec); err != nil {
			return err
		}
	}

	return nil
}

// collectSecretNames collects all secret names from templates.
func collectSecretNames(tp templates.Template, argParams map[string]string) ([]string, error) {
	var secretNames []string

	err := processPodTemplates(tp, argParams, func(podSpec *models.PodSpec) error {
		if podSpec.Kind == kindSecret {
			secretNames = append(secretNames, podSpec.Name)
		}

		return nil
	})

	return secretNames, err
}

// extractAllRoutesFromTemplates extracts routes annotations from all templates that have them.
// Returns a slice of caddy.TemplateRouteInfo containing pod name and routes for each template.
func extractAllRoutesFromTemplates(tp templates.Template, argParams map[string]string) ([]caddy.TemplateRouteInfo, error) {
	var routeInfos []caddy.TemplateRouteInfo

	err := processPodTemplates(tp, argParams, func(podSpec *models.PodSpec) error {
		// Check if this template has the routes annotation
		if podSpec.Annotations != nil {
			if routes, ok := podSpec.Annotations[constants.PodRoutesAnnotationKey]; ok {
				routeInfos = append(routeInfos, caddy.TemplateRouteInfo{
					PodName:          podSpec.Name,
					RoutesAnnotation: routes,
				})
			}
		}

		return nil
	})

	return routeInfos, err
}

// findCaddyPodNameFromTemplates finds the Caddy pod name by looking for the pod with component=proxy label in templates.
func findCaddyPodNameFromTemplates(tp templates.Template, argParams map[string]string) (string, error) {
	var caddyPodName string

	err := processPodTemplates(tp, argParams, func(podSpec *models.PodSpec) error {
		// Check if this is the Caddy pod (component=proxy label)
		if podSpec.Labels != nil {
			if component, ok := podSpec.Labels["ai-services.io/component"]; ok && component == "proxy" {
				caddyPodName = podSpec.Name
			}
		}

		return nil
	})

	// Check if we found the Caddy pod (err will be "found" sentinel)
	if caddyPodName != "" {
		return caddyPodName, nil
	}

	// If err is not nil and we didn't find the pod, it's a real error
	if err != nil {
		return "", err
	}

	return "", fmt.Errorf("no Caddy pod found with component=proxy label in templates")
}

// Made with Bob
