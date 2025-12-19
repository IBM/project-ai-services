package image

import (
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

type ImagePullPolicy string

const (
	PullAlways       ImagePullPolicy = "Always"
	PullIfNotPresent ImagePullPolicy = "IfNotPresent"
	PullNever        ImagePullPolicy = "Never"
)

func (p ImagePullPolicy) Valid() bool {
	switch p {
	case PullAlways, PullNever, PullIfNotPresent:
		return true
	}
	return false
}

type ImagePull struct {
	Runtime          runtime.Runtime
	Policy           ImagePullPolicy
	App, AppTemplate string
}

func NewImagePull(runtime runtime.Runtime, policy ImagePullPolicy, app, appTemplate string) *ImagePull {
	return &ImagePull{
		Runtime:     runtime,
		Policy:      policy,
		App:         app,
		AppTemplate: appTemplate,
	}
}

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

func (p ImagePull) ifNotPresent() error {
	// Fetch all images required for a given template
	images, err := ListImages(p.AppTemplate, p.App)
	if err != nil {
		return fmt.Errorf("failed to list container images: %w", err)
	}

	notFoundImages, err := fetchImagesNotFound(p.Runtime, images)
	if err != nil {
		return err
	}

	// Pull only those images which does not exist
	return pullImageFromRegistry(p.Runtime, notFoundImages)
}

func (p ImagePull) never() error {
	// Fetch all images required for a given template
	images, err := ListImages(p.AppTemplate, p.App)
	if err != nil {
		return fmt.Errorf("failed to list container images: %w", err)
	}

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
