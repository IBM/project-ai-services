package image

import (
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ImagePullPolicy type.
type ImagePullPolicy string

const (
	PullAlways       ImagePullPolicy = "Always"
	PullIfNotPresent ImagePullPolicy = "IfNotPresent"
	PullNever        ImagePullPolicy = "Never"
)

// Valid checks for supported ImagePullPolicy values.
func (p ImagePullPolicy) Valid() bool {
	return p == PullAlways || p == PullNever || p == PullIfNotPresent
}

// Images manages container images for applications, including listing and pulling based on policies.
type Images struct {
	Runtime     runtime.Runtime
	App         string
	AppTemplate string
}

// ListImages returns the list of images required for the application template.
func (img *Images) ListImages() ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)

	// Fetch list of app templates
	apps, err := tp.ListApplications(true)
	if err != nil {
		return nil, fmt.Errorf("error listing templates: %w", err)
	}
	if found := slices.Contains(apps, img.AppTemplate); !found {
		return nil, fmt.Errorf("provided template name is wrong. Please provide a valid template name")
	}

	// Load all the pod templates for given template
	tmpls, err := tp.LoadAllTemplates(img.AppTemplate)
	if err != nil {
		return nil, fmt.Errorf("error loading templates for %s: %w", img.AppTemplate, err)
	}

	images := []string{
		// Include tool image as well which is used for all the housekeeping tasks
		vars.ToolImage,
	}

	// Fetch all the images required for the given template by looping over each of the pod template files
	for _, tmpl := range tmpls {
		ps, err := tp.LoadPodTemplateWithValues(img.AppTemplate, tmpl.Name(), img.App, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("error loading pod template: %w", err)
		}
		for _, container := range ps.Spec.Containers {
			images = append(images, container.Image)
		}
	}

	return utils.UniqueSlice(images), nil
}

// Run executes the image pull policy.
func (img *Images) Run(policy ImagePullPolicy) error {
	// Fetch all images required for the template
	images, err := img.ListImages()
	if err != nil {
		return fmt.Errorf("failed to list container images for app %s template %s: %w", img.App, img.AppTemplate, err)
	}

	switch policy {
	case PullAlways:
		return img.always(images)
	case PullIfNotPresent:
		return img.ifNotPresent(images)
	case PullNever:
		return img.never(images)
	default:
		return fmt.Errorf("unsupported policy: %s", policy)
	}
}

// always -> pulls all the images for a given app template.
func (img *Images) always(images []string) error {
	logger.Infoln("Downloading container images required for application template " + img.AppTemplate + ":")

	return pullImageFromRegistry(img.Runtime, images)
}

// ifNotPresent -> pulls only the missing images for a given app template.
func (img *Images) ifNotPresent(images []string) error {
	notFoundImages, err := fetchImagesNotFound(img.Runtime, images)
	if err != nil {
		return err
	}

	if len(notFoundImages) == 0 {
		logger.Infoln("All required container images are already present locally.")

		return nil
	}

	return pullImageFromRegistry(img.Runtime, notFoundImages)
}

// never -> never pulls any image.
// It checks whether all the images for given appTemplate is present locally, if not then raises an error.
func (img *Images) never(images []string) error {
	notFoundImages, err := fetchImagesNotFound(img.Runtime, images)
	if err != nil {
		return err
	}

	if len(notFoundImages) > 0 {
		return fmt.Errorf("some required images are not present locally: %v. Either pull the image manually or rerun create command without --image-pull-policy or --skip-image-download flag", notFoundImages)
	}

	logger.Infoln("All required container images are present locally.")

	return nil
}
