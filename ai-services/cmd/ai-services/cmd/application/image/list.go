package image

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List container images for a given application template",
	Long:  ``,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		return list(templateName)
	},
}

func list(templateName string) error {
	if experimentalImages && vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		return listCatalogImages(templateName)
	}

	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypeOpenShift {
		logger.Warningln("Not supported for openshift runtime")

		return nil
	}

	img := &image.Images{
		AppTemplate: templateName,
	}
	images, err := img.ListImages()
	if err != nil {
		return fmt.Errorf("error listing images: %w", err)
	}

	logger.Infof("Container images for application template '%s' are:\n", templateName)
	for _, image := range images {
		logger.Infoln("- " + image)
	}

	return nil
}

// listCatalogImages lists container images for services or architectures from the catalog.
func listCatalogImages(templateID string) error {
	images, err := getCatalogImages(templateID)
	if err != nil {
		return err
	}

	if len(images) == 0 {
		logger.Infoln("No images found")

		return nil
	}

	logger.Infof("Container images for template '%s':\n", templateID)
	for _, img := range images {
		logger.Infof("- %s\n", img)
	}

	return nil
}
