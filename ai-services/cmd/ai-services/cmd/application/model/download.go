package model

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var modelDirectory string

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download models for a given application template",
	Long:  ``,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true
		hiddenTemplates, _ = cmd.Flags().GetBool("hidden")

		return download(cmd)
	},
}

func init() {
	downloadCmd.Flags().StringVarP(&templateName, "template", "t", "", "Application template name(Required)")
	_ = downloadCmd.MarkFlagRequired("template")
	downloadCmd.Flags().StringVar(&vars.ToolImage, "tool-image", vars.ToolImage, "Tool container image used for downloading the model (for development purposes only)")
	_ = downloadCmd.Flags().MarkHidden("tool-image")
	downloadCmd.Flags().StringVar(&modelDirectory, "dir", utils.GetModelsPath(), "Directory to download the model files")
}

func download(cmd *cobra.Command) error {
	if experimentalModels && vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		return downloadCatalogModels(templateName)
	}

	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypeOpenShift {
		// Since we do not have tmpl files in OpenShift marking it as unsupported for now
		logger.Warningln("Not supported for openshift runtime")

		return nil
	}

	models, err := models(templateName)
	if err != nil {
		return err
	}
	logger.Infoln("Downloaded Models in application template" + templateName + ":")
	for _, model := range models {
		err := helpers.DownloadModel(model, modelDirectory)
		if err != nil {
			return fmt.Errorf("failed to download model: %w", err)
		}
	}

	return nil
}

// downloadCatalogModels downloads models for services or architectures from the catalog.
func downloadCatalogModels(templateID string) error {
	models, err := getCatalogModels(templateID, "watsonx")
	if err != nil {
		return err
	}

	if len(models) == 0 {
		logger.Infoln("No models to download")

		return nil
	}

	// Download all models
	logger.Infof("Downloading %d models for template '%s'...\n", len(models), templateID)

	for _, model := range models {
		logger.Infof("Downloading model: %s\n", model)

		if err := helpers.DownloadModelContainer(context.Background(), model, modelDirectory); err != nil {
			return fmt.Errorf("failed to download model %s: %w", model, err)
		}
	}

	logger.Infof("Successfully downloaded all models for template '%s'\n", templateID)

	return nil
}
