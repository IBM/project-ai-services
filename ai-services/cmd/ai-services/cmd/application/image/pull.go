package image

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/spf13/cobra"
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
	images, err := helpers.ListImages(template, "")
	if err != nil {
		return fmt.Errorf("error listing images: %w", err)
	}

	logger.Infof("Downloading the images for the application... ")
	runtimeClient, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to connect to podman: %w", err)
	}

	for _, image := range images {
		if err := runtimeClient.PullImage(image, nil); err != nil {
			return fmt.Errorf("failed to pull the image: %w", err)
		}
	}

	return nil
}
