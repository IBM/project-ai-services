package image

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// pullImageFromRegistry pulls the required images from registry.
func pullImageFromRegistry(runtime runtime.Runtime, images []string) error {
	for _, image := range images {
		logger.Infoln("Downloading image: " + image + "...")
		if err := utils.Retry(vars.RetryCount, vars.RetryInterval, nil, func() error {
			return runtime.PullImage(image)
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
