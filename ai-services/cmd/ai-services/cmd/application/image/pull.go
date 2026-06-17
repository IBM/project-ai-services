package image

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Pulls all container images for a given application template",
	Long:  ``,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		return pull(templateName)
	},
}

func pull(template string) error {
	if experimentalImages && vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		return pullCatalogImages(templateName)
	}

	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypeOpenShift {
		logger.Warningln("Not supported for openshift runtime")

		return nil
	}

	img := &image.Images{
		AppTemplate: template,
	}
	images, err := img.ListImages()
	if err != nil {
		return fmt.Errorf("error listing images: %w", err)
	}

	logger.Infof("Downloading the images for the application... ")
	runtimeClient, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to connect to podman: %w", err)
	}

	// Use shared helper function with retry logic
	return image.PullImageFromRegistry(context.Background(), runtimeClient, images)
}

// pullCatalogImages pulls container images for services or architectures from the catalog.
func pullCatalogImages(templateID string) error {
	images, err := getCatalogImages(templateID)
	if err != nil {
		return err
	}

	if len(images) == 0 {
		logger.Infoln("No images to pull")

		return nil
	}

	// Pull all images
	logger.Infof("Downloading %d images for template '%s'...\n", len(images), templateID)
	runtimeClient, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to connect to podman: %w", err)
	}

	// Use shared helper function with retry logic
	if err := image.PullImageFromRegistry(context.Background(), runtimeClient, images); err != nil {
		return err
	}

	logger.Infof("Successfully pulled all images for template '%s'\n", templateID)

	return nil
}
