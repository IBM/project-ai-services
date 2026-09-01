package model

import (
	"context"
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
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
		Args: cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	hiddenTemplates bool
	legacyModel     bool
)

func init() {
	ModelCmd.AddCommand(listCmd)
	ModelCmd.AddCommand(downloadCmd)
	ModelCmd.PersistentFlags().BoolVar(&legacyModel, "legacy", false, "Use legacy application model implementation")
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

// getCatalogModels calls the running catalog API to retrieve models for a
// service or architecture template. It tries the service endpoint first, then
// the architecture endpoint.
// excludeProviders is forwarded to the server as the ?exclude_providers= query
// param (e.g. pass "watsonx" to omit watsonx models during download).
func getCatalogModels(ctx context.Context, templateID string, excludeProviders ...string) ([]string, error) {
	appClient, err := client.NewApplicationClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to catalog API: %w", err)
	}

	// Try service endpoint first.
	apiModels, err := appClient.GetServiceModels(ctx, templateID, excludeProviders...)
	if err == nil {
		return apiModels, nil
	}

	// Fall through to architecture endpoint.
	apiModels, err = appClient.GetArchitectureModels(ctx, templateID, excludeProviders...)
	if err == nil {
		return apiModels, nil
	}

	return nil, fmt.Errorf("template '%s' not found as service or architecture", templateID)
}
