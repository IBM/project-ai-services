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
	Long:  ``,
	Args:  cobra.MaximumNArgs(0),
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
