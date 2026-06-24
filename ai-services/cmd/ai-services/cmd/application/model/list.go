package model

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var templateName string

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List models for a given application template",
	Long: `List all available models for a specific application template.
Note:
  - Supports only podman runtime.
  - Use 'ai-services application templates' to see available template names`,
	Example: `  # List models for a specific template for podman runtime
  ai-services application model list --template chatbot --runtime podman`,
	Args: cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true
		hiddenTemplates, _ = cmd.Flags().GetBool("hidden")

		return list(cmd)
	},
}

func init() {
	listCmd.Flags().StringVarP(&templateName, "template", "t", "", "Application template name (Required)")
	_ = listCmd.MarkFlagRequired("template")
}

func list(cmd *cobra.Command) error {
	if experimentalModels && vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		return listCatalogModels(templateName)
	}

	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypeOpenShift {
		// Since we do not have tmpl files in OpenShift marking it as unsupported for now
		logger.Warningln("Not supported for openshift runtime")

		return nil
	}

	models, err := models(templateName)
	if err != nil {
		return fmt.Errorf("failed to list the models, err: %w", err)
	}
	logger.Infoln("Models in application template " + templateName + ":")
	for _, model := range models {
		logger.Infoln("- " + model)
	}

	return nil
}

// listCatalogModels lists models for services or architectures from the catalog.
func listCatalogModels(templateID string) error {
	models, err := getCatalogModels(templateID)
	if err != nil {
		return err
	}

	if len(models) == 0 {
		logger.Infoln("No models found")

		return nil
	}

	logger.Infof("Models for template '%s':\n", templateID)
	for _, model := range models {
		logger.Infof("- %s\n", model)
	}

	return nil
}
