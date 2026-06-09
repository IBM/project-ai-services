package catalog

import (
	"bytes"
	"errors"
	"fmt"
	texttemplate "text/template"

	k8syaml "sigs.k8s.io/yaml"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// extractContainerImages extracts images from containers and init containers.
func extractContainerImages(podSpec *models.PodSpec, imageSet map[string]bool) {
	// Extract images from containers
	for _, container := range podSpec.Spec.Containers {
		if container.Image != "" {
			imageSet[container.Image] = true
		}
	}

	// Extract images from init containers
	for _, container := range podSpec.Spec.InitContainers {
		if container.Image != "" {
			imageSet[container.Image] = true
		}
	}
}

// CollectImagesFromTemplates extracts images from a set of pre-loaded templates
// and adds them directly to the provided imageSet map.
// This is the low-level function that callers use after loading templates themselves.
// Returns an error if any template fails to render or parse.
func (p *CatalogProvider) CollectImagesFromTemplates(templates map[string]*texttemplate.Template, values map[string]any, imageSet map[string]bool) error {
	var errorResponses []error

	// Process each template
	for templateName, tmpl := range templates {
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
			logger.Errorf("Failed to render template %s for image extraction: %v", templateName, err)
			errorResponses = append(errorResponses, err)
			continue
		}

		// Parse the rendered template as Pod spec
		var podSpec models.PodSpec
		if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
			logger.Errorf("Failed to parse rendered template %s: %v", templateName, err)
			errorResponses = append(errorResponses, err)
			continue
		}

		// Extract images from containers directly into the provided set
		extractContainerImages(&podSpec, imageSet)
	}

	// Return error if any template failed
	if len(errorResponses) > 0 {
		return fmt.Errorf("failed to process %d template(s): %w", len(errorResponses), errors.Join(errorResponses...))
	}

	return nil
}
