package model

import (
	"context"
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/spf13/cobra"
)

var (
	ModelCmd = &cobra.Command{
		Use:   "model",
		Short: "Manage application models",
		Long: `Manage AI models for application templates.
This command provides subcommands to list and download models required by application templates.`,
		Example: `  # List available models for a template
	 ai-services application model list --template chatbot --runtime podman

	 # Download models for a template
	 ai-services application model download --template chatbot --runtime podman`,
		Args: cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	hiddenTemplates    bool
	experimentalModels bool
)

func init() {
	ModelCmd.AddCommand(listCmd)
	ModelCmd.AddCommand(downloadCmd)
	ModelCmd.PersistentFlags().BoolVar(&experimentalModels, "experimental", false, "Use experimental catalog-based model listing")
}

func models(template string) ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
	apps, err := tp.ListApplications(hiddenTemplates)
	if err != nil {
		return nil, fmt.Errorf("failed to list the applications, err: %w", err)
	}

	if !slices.Contains(apps, template) {
		return nil, fmt.Errorf("application template %s does not exist", template)
	}

	return helpers.ListModels(template, "")
}

// getCatalogModels is a helper that creates a catalog provider and collects models.
// excludeComponentProviders is a variadic parameter that allows excluding specific component provider by ID.
func getCatalogModels(templateID string, excludeComponentProviders ...string) ([]string, error) {
	provider, err := catalog.NewCatalogProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider: %w", err)
	}

	models, err := provider.GetCatalogModels(context.Background(), templateID, excludeComponentProviders...)
	if err != nil {
		return nil, err
	}

	return models, nil
}
