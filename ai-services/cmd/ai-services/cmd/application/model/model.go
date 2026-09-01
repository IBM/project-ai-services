package model

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconfig "github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
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

// getCatalogModels resolves models for a service or architecture template.
// It first tries the running catalog API; if the user is not logged in it falls
// back to the embedded local CatalogProvider.
// excludeProviders is forwarded to the API as ?exclude_providers= and to the
// local provider's exclude list (e.g. "watsonx" during download).
func getCatalogModels(ctx context.Context, templateID string, excludeProviders ...string) ([]string, error) {
	appClient, err := client.NewApplicationClient(ctx)
	if err != nil {
		if !errors.Is(err, catalogconfig.ErrNotLoggedIn) {
			return nil, fmt.Errorf("failed to connect to catalog API: %w", err)
		}
		// No active session — fall back to embedded-only local provider.
		logger.Warningln("Not logged in to catalog server, falling back to local embedded catalog (custom bundle items will not be included)")

		return getCatalogModelsLocal(ctx, templateID, excludeProviders...)
	}

	// Try service endpoint first, then architecture.
	if apiModels, err := appClient.GetServiceModels(ctx, templateID, excludeProviders...); err == nil {
		return apiModels, nil
	}

	if apiModels, err := appClient.GetArchitectureModels(ctx, templateID, excludeProviders...); err == nil {
		return apiModels, nil
	}

	return nil, fmt.Errorf("template '%s' not found as service or architecture", templateID)
}

// getCatalogModelsLocal uses the embedded CatalogProvider directly (no API call).
func getCatalogModelsLocal(ctx context.Context, templateID string, excludeProviders ...string) ([]string, error) {
	provider, err := catalog.NewCatalogProvider(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider: %w", err)
	}

	return provider.GetCatalogModels(ctx, templateID, excludeProviders...)
}
