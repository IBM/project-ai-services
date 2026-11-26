package templates

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewModelsCmd creates the models subcommand.
func NewModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Display the models that can be chosen for a specific template",
		Long: `Display, for a specific template ID (service or architecture) from the catalog, the model component types,
the models recommended by the template and the models that can be deployed.
Use them with the --model and --params flags of "application create".`,
		Example: `  # Display models for a service
  ai-services application templates models --template extract --runtime podman

  # Display models for an architecture
  ai-services application templates models --template rag --runtime podman`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if runtimeType == "" {
				return fmt.Errorf("required flag(s) \"runtime\" not set")
			}

			return cmdcommon.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			if templateID == "" {
				return fmt.Errorf("--template flag is required")
			}

			// NewCatalogSource tries the API first, falls back to the embedded
			// catalog when the user is not logged in.
			source, err := catalogClient.NewCatalogSource(cmd.Context())
			if err != nil {
				return err
			}

			// Try to display as architecture first
			if err := displayArchitectureModels(cmd.Context(), source, templateID); err == nil {
				return nil
			}

			// Try to display as service
			if err := displayServiceModels(cmd.Context(), source, templateID); err == nil {
				return nil
			}

			return fmt.Errorf("template '%s' not found as service or architecture", templateID)
		},
	}

	cmdcommon.ConfigureRuntimeFlag(cmd, &runtimeType)
	cmd.Flags().StringVar(&templateID, "template", "", "Template ID (service or architecture)")
	_ = cmd.MarkFlagRequired("template")

	return cmd
}

// displayServiceModels loads a service and displays the models of its model component types.
func displayServiceModels(ctx context.Context, source catalogClient.CatalogSource, serviceID string) error {
	deployOpts, err := source.GetServiceDeployOptions(ctx, serviceID, currentRuntime())
	if err != nil {
		return fmt.Errorf("failed to get deploy options for service '%s': %w", serviceID, err)
	}

	logger.Infof("Models for '%s':", serviceID)

	displayDeployOptionsModels(ctx, source, deployOpts, nil)

	return nil
}

// displayArchitectureModels loads an architecture and displays the models of all its services.
func displayArchitectureModels(ctx context.Context, source catalogClient.CatalogSource, archID string) error {
	arch, err := source.LoadArchitecture(ctx, archID)
	if err != nil {
		return err
	}

	logger.Infof("Models for '%s':", archID)

	// Track displayed component types to avoid duplicates across services
	displayedComponents := make(map[string]bool)

	// Log a warning if a service fails so the user knows output may be incomplete.
	for _, svcRef := range arch.Services {
		deployOpts, err := source.GetServiceDeployOptions(ctx, svcRef.ID, currentRuntime())
		if err != nil {
			logger.Warningf("skipping models for service '%s': %v", svcRef.ID, err)

			continue
		}
		displayDeployOptionsModels(ctx, source, deployOpts, displayedComponents)
	}

	return nil
}

// displayDeployOptionsModels displays the model section of each model component type of the deploy options.
// If displayedComponents map is provided, it tracks and skips component types already displayed.
func displayDeployOptionsModels(ctx context.Context, source catalogClient.CatalogSource, deployOpts *catalogTypes.DeployOptionsService, displayedComponents map[string]bool) {
	for _, comp := range deployOpts.Components {
		if comp.Category == catalogConstants.ComponentCategoryModels {
			displayModelComponent(ctx, source, deployOpts.ID, comp, displayedComponents)
		}
	}
}

// displayModelComponent displays a model component type: what the service recommends for each of its
// providers (the first is the primary), the models that can be deployed, and how to choose one.
// The models of a component type are shown once when displayedComponents is tracking duplicates.
func displayModelComponent(ctx context.Context, source catalogClient.CatalogSource, serviceID string, comp catalogTypes.DeployOptionsComponent, displayedComponents map[string]bool) {
	if displayedComponents != nil {
		key := "model." + comp.Type
		if displayedComponents[key] {
			return
		}
		displayedComponents[key] = true
	}

	entries, err := source.ListCatalogModels(ctx, comp.Type)
	if err != nil {
		logger.Warningf("skipping models for component type '%s': %v", comp.Type, err)

		return
	}

	for _, line := range modelComponentLines(serviceID, comp, entries) {
		logger.Infoln(line)
	}
}

// modelComponentLines formats the model section of a component type. Only models a local provider of
// the component can deploy are listed, as only those can be chosen with --model.
func modelComponentLines(serviceID string, comp catalogTypes.DeployOptionsComponent, entries []catalogTypes.ModelCatalogEntry) []string {
	lines := []string{fmt.Sprintf("  %s models (choose with --model %s=<model>, the provider with --params %s.<provider>=true):", comp.Type, comp.Type, comp.Type)}

	for _, provider := range comp.Providers {
		if len(provider.Recommendations) == 0 {
			continue
		}

		recommended := make([]string, 0, len(provider.Recommendations))
		for i, r := range provider.Recommendations {
			if i == 0 {
				recommended = append(recommended, r.Model+" (primary)")

				continue
			}
			recommended = append(recommended, r.Model)
		}

		lines = append(lines, fmt.Sprintf("    recommended by '%s' for provider '%s': %s", serviceID, provider.ID, strings.Join(recommended, ", ")))
	}

	localIDs := make(map[string]bool, len(comp.Providers))
	for _, provider := range comp.Providers {
		localIDs[provider.ID] = true
	}

	available := availableModelLines(entries, localIDs)
	if len(available) == 0 {
		return append(lines, "    no deployable models")
	}

	lines = append(lines, "    available models:")

	return append(lines, available...)
}

// availableModelLines lists, sorted by model ID, the models deployable by one of the given local providers.
func availableModelLines(entries []catalogTypes.ModelCatalogEntry, localIDs map[string]bool) []string {
	var lines []string

	sorted := slices.SortedFunc(slices.Values(entries), func(a, b catalogTypes.ModelCatalogEntry) int { return cmp.Compare(a.ID, b.ID) })

	for _, e := range sorted {
		offered := offeredProviders(e, localIDs)
		if len(offered) == 0 {
			continue
		}

		lines = append(lines, fmt.Sprintf("      - %s [%s]", e.ID, strings.Join(offered, ", ")))
	}

	return lines
}

// offeredProviders returns the local providers of the model that are in localIDs, marking the default one.
func offeredProviders(e catalogTypes.ModelCatalogEntry, localIDs map[string]bool) []string {
	var offered []string

	for _, p := range e.Providers {
		if p.DeploymentType != catalogConstants.DeploymentTypeLocal || !localIDs[p.ID] {
			continue
		}

		if p.DefaultModel {
			offered = append(offered, p.ID+" (default)")
		} else {
			offered = append(offered, p.ID)
		}
	}

	return offered
}
