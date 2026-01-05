package model

import (
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/spf13/cobra"
)

var ModelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage application models",
	Long:  ``,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	ModelCmd.AddCommand(listCmd)
	ModelCmd.AddCommand(downloadCmd)
}

func models(template string) ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})
	apps, err := tp.ListApplications()
	if err != nil {
		return nil, fmt.Errorf("failed to list the applications, err: %w", err)
	}

	if !slices.Contains(apps, template) {
		return nil, fmt.Errorf("application template %s does not exist", template)
	}

	return helpers.ListModels(template, "")
}
