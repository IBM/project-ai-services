package application

import (
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/image"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/model"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ApplicationCmd represents the application command
var ApplicationCmd = &cobra.Command{
	Use:   "application",
	Short: "Deploy and monitor the applications",
	Long:  `The application command helps you deploy and monitor the applications`,
}

func init() {
	ApplicationCmd.AddCommand(templatesCmd)
	ApplicationCmd.AddCommand(createCmd)
	ApplicationCmd.AddCommand(psCmd)
	ApplicationCmd.AddCommand(deleteCmd)
	ApplicationCmd.AddCommand(image.ImageCmd)
	ApplicationCmd.AddCommand(stopCmd)
	ApplicationCmd.AddCommand(startCmd)
	ApplicationCmd.AddCommand(infoCmd)
	ApplicationCmd.AddCommand(logsCmd)
	ApplicationCmd.AddCommand(model.ModelCmd)
	ApplicationCmd.PersistentFlags().StringVar(&vars.ToolImage, "tool-image", vars.ToolImage, "Tool image to use for downloading the model(only for the development purpose)")
	ApplicationCmd.PersistentFlags().MarkHidden("tool-image")
}
