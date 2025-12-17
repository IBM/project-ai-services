package image

import (
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ListImages returns the list of images required for given application template.
func ListImages(template, appName string) ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})

	// fetch list of app templates
	apps, err := tp.ListApplications()
	if err != nil {
		return nil, fmt.Errorf("error listing templates: %w", err)
	}
	if found := slices.Contains(apps, template); !found {
		return nil, fmt.Errorf("provided template name is wrong. Please provide a valid template name")
	}

	// load all the pod templates for given template
	tmpls, err := tp.LoadAllTemplates(template)
	if err != nil {
		return nil, fmt.Errorf("error loading templates for %s: %w", template, err)
	}

	images := []string{
		// include tool image as well which is used for all the housekeeping tasks
		vars.ToolImage,
	}

	// fetch all the images required for the given template by looping over each of the pod template files
	for _, tmpl := range tmpls {
		ps, err := tp.LoadPodTemplateWithValues(template, tmpl.Name(), appName, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("error loading pod template: %w", err)
		}
		for _, container := range ps.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return utils.UniqueSlice(images), nil
}

// pullImageFromRegistry pulls the required images from registry.
func pullImageFromRegistry(runtime runtime.Runtime, images []string) error {
	for _, image := range images {
		logger.Infoln("Downloading image: " + image + "...")
		if err := utils.Retry(vars.RetryCount, vars.RetryInterval, nil, func() error {
			return runtime.PullImage(image, nil)
		}); err != nil {
			return fmt.Errorf("failed to download image: %w", err)
		}
	}

	return nil
}

// fetchImagesNotFound returns list of images which are not present locally.
func fetchImagesNotFound(runtime runtime.Runtime, reqImages []string) ([]string, error) {
	notfoundImages := make([]string, 0, len(reqImages))

	// Verify the images existing locally
	lImages, err := runtime.ListImages()
	if err != nil {
		return nil, fmt.Errorf("failed to list local images: %w", err)
	}

	// Populate a map with all existing local images (tags and digests)
	existingImages := make(map[string]bool)

	for _, lImage := range lImages {
		for _, tag := range lImage.RepoTags {
			existingImages[tag] = true
		}
		for _, digest := range lImage.RepoDigests {
			existingImages[digest] = true
		}
	}

	// Filter the requested images against the existingImages map to determine the non existing images
	for _, image := range reqImages {
		if !existingImages[image] {
			notfoundImages = append(notfoundImages, image)
		}
	}

	return notfoundImages, nil
}
