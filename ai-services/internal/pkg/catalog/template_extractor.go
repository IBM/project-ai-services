package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	texttemplate "text/template"

	k8syaml "sigs.k8s.io/yaml"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// PodSpecProcessor is a callback function that processes a rendered pod spec.
// It receives the template name and pod spec, and can return an error if processing fails.
type PodSpecProcessor func(templateName string, podSpec *models.PodSpec) error

// ProcessTemplates is a generic function that renders templates and processes each pod spec
// using the provided processor callback. This allows different use cases (image extraction,
// Spyre card counting, etc.) to reuse the same template rendering logic.
// Returns an error if any template fails to render, parse, or process.
func (p *CatalogProvider) ProcessTemplates(
	ctx context.Context,
	templates map[string]*texttemplate.Template,
	values map[string]any,
	instanceSlug string,
	processor PodSpecProcessor,
) error {
	var errorResponses []error

	// Process each template
	for templateName, tmpl := range templates {
		// Prepare minimal params for rendering
		initialParams := map[string]any{
			"InstanceSlug": instanceSlug,
			"TemplateID":   uuid.New(),
			"BaseDir":      utils.GetBaseDir(),
			"Values":       values,
			"env":          map[string]map[string]string{},
		}

		// Render the template
		var rendered bytes.Buffer
		if err := tmpl.Execute(&rendered, initialParams); err != nil {
			logger.ErrorfCtx(ctx, "Failed to render template %s: %v", templateName, err)
			errorResponses = append(errorResponses, err)

			continue
		}

		// Parse the rendered template as Pod spec
		var podSpec models.PodSpec
		if err := k8syaml.Unmarshal(rendered.Bytes(), &podSpec); err != nil {
			logger.ErrorfCtx(ctx, "Failed to parse rendered template %s: %v", templateName, err)
			errorResponses = append(errorResponses, err)

			continue
		}

		// Process the pod spec using the provided callback
		if err := processor(templateName, &podSpec); err != nil {
			return fmt.Errorf("failed to process template %s: %w", templateName, err)
		}
	}

	// Return error if any template failed to render or parse
	if len(errorResponses) > 0 {
		return fmt.Errorf("failed to process %d template(s): %w", len(errorResponses), errors.Join(errorResponses...))
	}

	return nil
}

// CollectImagesFromTemplates extracts images from a set of pre-loaded templates
// and adds them directly to the provided imageSet map.
// This is a convenience wrapper around ProcessTemplates for image extraction.
func (p *CatalogProvider) CollectImagesFromTemplates(
	ctx context.Context,
	templates map[string]*texttemplate.Template,
	values map[string]any,
	imageSet map[string]bool,
) error {
	// Create processor that extracts container images
	processor := func(templateName string, podSpec *models.PodSpec) error {
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

		return nil
	}

	return p.ProcessTemplates(ctx, templates, values, "image-extraction", processor)
}

// CollectSpyreCardsFromTemplates extracts Spyre card requirements from a set of pre-loaded templates
// by analyzing pod annotations. Returns the total number of Spyre cards required.
// This is a convenience wrapper around ProcessTemplates for Spyre card counting.
func (p *CatalogProvider) CollectSpyreCardsFromTemplates(
	ctx context.Context,
	templates map[string]*texttemplate.Template,
	values map[string]any,
) (int, error) {
	totalSpyreCards := 0

	// Create processor that counts Spyre cards from pod annotations
	processor := func(templateName string, podSpec *models.PodSpec) error {
		// Extract Spyre card requirements from annotations
		spyreCards, _, err := fetchSpyreCardsFromPodAnnotations(podSpec.Annotations)
		if err != nil {
			return fmt.Errorf("failed to extract Spyre cards: %w", err)
		}

		totalSpyreCards += spyreCards
		if spyreCards > 0 {
			logger.InfofCtx(ctx, "Template %s requires %d Spyre cards\n", templateName, spyreCards)
		}

		return nil
	}

	// Use the generic ProcessTemplates function
	if err := p.ProcessTemplates(ctx, templates, values, "spyre-card-calculation", processor); err != nil {
		return 0, fmt.Errorf("failed to process templates for Spyre card calculation: %w", err)
	}

	return totalSpyreCards, nil
}

// fetchSpyreCardsFromPodAnnotations extracts Spyre card requirements from pod annotations.
// Returns the total number of Spyre cards and a map of container names to their card requirements.
func fetchSpyreCardsFromPodAnnotations(annotations map[string]string) (int, map[string]int, error) {
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
