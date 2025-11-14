package helpers

import (
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

func ListImages(template string) ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})
	apps, err := tp.ListApplications()
	if err != nil {
		return nil, fmt.Errorf("error listing templates: %w", err)
	}
	if found := slices.Contains(apps, template); !found {
		return nil, fmt.Errorf("provided template name is wrong. Please provide a valid template name")
	}
	tmpls, err := tp.LoadAllTemplates(template)
	if err != nil {
		return nil, fmt.Errorf("error loading templates for %s: %w", template, err)
	}

	images := []string{
		// include tool image as well which is used for all the housekeeping tasks
		vars.ToolImage,
	}

	for _, tmpl := range tmpls {
		ps, err := tp.LoadPodTemplateWithDummyParams(template, tmpl.Name())
		if err != nil {
			return nil, fmt.Errorf("error loading pod template: %w", err)
		}
		for _, container := range ps.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return utils.UniqueSlice(images), nil
}
