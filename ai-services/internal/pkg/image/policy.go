package image

import (
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
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

// ImagePull type.
type ImagePull struct {
	Runtime          runtime.Runtime
	Policy           ImagePullPolicy
	App, AppTemplate string
}

// NewImagePull factory method to return ImagePull object.
func NewImagePull(runtime runtime.Runtime, policy ImagePullPolicy, app, appTemplate string) *ImagePull {
	return &ImagePull{
		Runtime:     runtime,
		Policy:      policy,
		App:         app,
		AppTemplate: appTemplate,
	}
}

// Run runs a particular imagePullPolicy method type based on the policy set within the ImagePull object.
func (p ImagePull) Run() error {
	switch p.Policy {
	case PullAlways:
		return p.always()
	case PullIfNotPresent:
		return p.ifNotPresent()
	case PullNever:
		return p.never()
	default:
		return errors.New("unsupported policy set")
	}
}

// always -> pulls all the images for a given app template.
func (p ImagePull) always() error {
	// Fetch all images required for a given template
	images, err := ListImages(p.AppTemplate, p.App)
	if err != nil {
		return fmt.Errorf("failed to list container images: %w", err)
	}

	// Download container images if flag is set to false (default: false)
	logger.Infoln("Downloading container images required for application template " + p.AppTemplate + ":")

	// Pull all the images
	return pullImageFromRegistry(p.Runtime, images)
}

// ifNotPresent -> pulls only the missing images for a given app template.
func (p ImagePull) ifNotPresent() error {
	// Fetch all images required for a given template
	images, err := ListImages(p.AppTemplate, p.App)
	if err != nil {
		return fmt.Errorf("failed to list container images: %w", err)
	}

	// Fetch all the images which are not found locally
	notFoundImages, err := fetchImagesNotFound(p.Runtime, images)
	if err != nil {
		return err
	}

	// Pull only those images which does not exist
	return pullImageFromRegistry(p.Runtime, notFoundImages)
}

// never -> never pulls any image.
// It checks whether all the images for given appTemplate is present locally, if not then raises an error.
func (p ImagePull) never() error {
	// Fetch all images required for a given template
	images, err := ListImages(p.AppTemplate, p.App)
	if err != nil {
		return fmt.Errorf("failed to list container images: %w", err)
	}

	// Fetch all the images which are not found locally
	notFoundImages, err := fetchImagesNotFound(p.Runtime, images)
	if err != nil {
		return err
	}

	if len(notFoundImages) > 0 {
		return fmt.Errorf("some required images are not present locally: %v. Either pull the image manually or rerun create command without --image-pull-policy or --skip-image-download flag", notFoundImages)
	}

	logger.Infoln("All required container images are present locally.")

	return nil
}
