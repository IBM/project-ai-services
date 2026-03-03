package image

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
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
	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypeOpenShift {
		// Since we do not have tmpl files in OpenShift marking it as unsupported for now
		logger.Warningln("Not supported for openshift runtime")

		return nil
	}

	images, err := image.ListImages(templateName, "")
	if err != nil {
		return fmt.Errorf("error listing images: %w", err)
	}

	logger.Infof("Container images for application template '%s' are:\n", templateName)
	for _, image := range images {
		logger.Infoln("- " + image)
	}

	return nil
}
