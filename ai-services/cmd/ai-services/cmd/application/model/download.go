package model

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download models for a given application template",
	Long:  ``,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		return download(cmd)
	},
}

func init() {
	downloadCmd.Flags().StringVarP(&templateName, "template", "t", "", "Application template name(Required)")
	_ = downloadCmd.MarkFlagRequired("template")
	downloadCmd.Flags().StringVar(&vars.ToolImage, "tool-image", vars.ToolImage, "Tool container image used for downloading the model (for development purposes only)")
	_ = downloadCmd.Flags().MarkHidden("tool-image")
	downloadCmd.Flags().StringVar(&vars.ModelDirectory, "dir", vars.ModelDirectory, "Directory to download the model files")
}

func download(cmd *cobra.Command) error {
	models, err := models(templateName)
	if err != nil {
		return err
	}
	logger.Infoln("Downloaded Models in application template" + templateName + ":")
	for _, model := range models {
		err := helpers.DownloadModel(model, vars.ModelDirectory)
		if err != nil {
			return fmt.Errorf("failed to download model: %w", err)
		}
	}

	return nil
}
