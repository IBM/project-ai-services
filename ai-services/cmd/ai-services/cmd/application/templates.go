package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/assets"
	appTemplates "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/templates"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	legacyTemplates bool
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Lists the offered application templates and their supported parameters",
	Long:  `Retrieves information about the offered application templates and their supported parameters`,
	Example: `  For Podman:
  # List all available application templates (Podman)
  ai-services application templates --runtime podman

  # List parameters for a specific template (see subcommand)
  ai-services application templates parameters --template digitize --runtime podman

  # List templates using legacy implementation
  ai-services application templates --legacy --runtime podman

  For OpenShift:
  # List all available application templates (OpenShift)
  ai-services application templates --runtime openshift

  # List parameters for a specific template (see subcommand)
  ai-services application templates parameters --template digitize --runtime openshift`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// --runtime is required for templates: listing templates uses the runtime
		// to filter supported parameter sets.
		if runtimeType == "" {
			return fmt.Errorf("required flag(s) \"runtime\" not set")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		// When legacyTemplates is true, use the older/stable code path
		if !legacyTemplates {
			// Use catalog templates listing (architectures and services)
			return listCatalogTemplates(cmd)
		}

		tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)

		appTemplateNames, err := tp.ListApplications(hiddenTemplates)
		if err != nil {
			return fmt.Errorf("failed to list application templates: %w", err)
		}

		if len(appTemplateNames) == 0 {
			logger.Infoln("No application templates found.")

			return nil
		}

		// sort appTemplateNames alphabetically
		sort.Strings(appTemplateNames)

		logger.Infoln("Available application templates:")
		for _, name := range appTemplateNames {
			appTemplatesParametersWithDescription, err := tp.ListApplicationTemplateValues(name)
			if err != nil {
				// Skip applications that don't support the current runtime (silently)
				if errors.Is(err, templates.ErrRuntimeNotSupported) {
					continue
				}
				// Log other errors
				logger.Errorf("failed to list application template values: %v", err)

				continue
			}

			logger.Infof("- %s", name)
			var metadata templates.AppMetadata
			if err := tp.LoadMetadata(name, false, &metadata); err != nil {
				logger.Errorf("failed to load application metadata: %v", err)

				continue
			}
			if metadata.Description != "" {
				logger.Infof("  Description: %s", metadata.Description)
			}

			logger.Infoln("\n  Supported Parameters:")
			if len(appTemplatesParametersWithDescription) == 0 {
				logger.Infoln("\t" + "NONE")
			}

			for k, v := range appTemplatesParametersWithDescription {
				logger.Infoln("\t" + k + ":  " + v)
			}
		}

		return nil
	},
}

func init() {
	templatesCmd.Flags().BoolVar(&legacyTemplates, "legacy", false, "Use legacy application templates implementation")

	// Add parameters subcommand
	templatesCmd.AddCommand(appTemplates.NewParametersCmd())
}

// listCatalogTemplates lists architectures, services, and components from the catalog.
// It always tries the API first and falls back to the embedded catalog when the
// API is unreachable or the user is not logged in.
func listCatalogTemplates(cmd *cobra.Command) error {
	source, err := catalogClient.NewCatalogSource(cmd.Context())
	if err != nil {
		return err
	}

	runtimeType := string(vars.RuntimeFactory.GetRuntimeType())

	architectures, err := source.ListArchitectures(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to list architectures: %w", err)
	}

	services, err := source.ListServices(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	// Section 1: Deployment Architectures with list of services
	logger.Infoln("Available Deployment Architectures:")
	for _, arch := range architectures {
		displayArchitectureWithServiceList(arch)
	}

	// Section 2: Deployment Services with metadata and required components
	logger.Infoln("\nAvailable Services:")
	for _, svc := range services {
		displayServiceWithComponents(cmd.Context(), source, svc, runtimeType)
	}

	// Inform user about parameters subcommand
	logger.Infoln("\nTo list supported parameters for each template use: application templates parameters --template <Template ID>\n")

	return nil
}

// displayArchitectureWithServiceList displays an architecture with just the list of service IDs.
func displayArchitectureWithServiceList(arch catalogTypes.ArchitectureSummary) {
	logger.Infof("- %s (%s)", arch.ID, arch.Name)
	if arch.Description != "" {
		logger.Infof("  Description: %s", arch.Description)
	}

	// Display list of services in this architecture
	if len(arch.Services) > 0 {
		logger.Infoln("  Services:")
		for _, svcID := range arch.Services {
			logger.Infof("     - %s", svcID)
		}
	}
}

// displayServiceWithComponents displays a service with its metadata and required components.
// It calls GetServiceDeployOptions so that custom bundle providers from the catalog volume
// are included alongside the embedded ones.
func displayServiceWithComponents(ctx context.Context, source catalogClient.CatalogSource, svc catalogTypes.ServiceSummary, runtimeType string) {
	logger.Infof("- %s (%s)", svc.ID, svc.Name)
	if svc.Description != "" {
		logger.Infof("  Description: %s", svc.Description)
	}

	if len(svc.Dependencies) == 0 {
		return
	}

	deployOpts, err := source.GetServiceDeployOptions(ctx, svc.ID, runtimeType)
	if err != nil {
		// Fall back to dependency IDs only — better than nothing.
		logger.Infoln("  Required Components:")
		for _, dep := range svc.Dependencies {
			logger.Infof("    %s: (providers unavailable)", dep.ID)
		}

		return
	}

	logger.Infoln("  Required Components:")
	for _, comp := range deployOpts.Components {
		providerIDs := make([]string, 0, len(comp.Providers))
		for _, p := range comp.Providers {
			providerIDs = append(providerIDs, p.ID)
		}

		if len(providerIDs) > 0 {
			logger.Infof("    %s: %s", comp.Type, strings.Join(providerIDs, ", "))
		} else {
			logger.Infof("    %s: (no providers available)", comp.Type)
		}
	}
}
