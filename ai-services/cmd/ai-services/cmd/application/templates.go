package application

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/assets"
	appTemplates "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	experimentalTemplates bool
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Lists the offered application templates and their supported parameters",
	Long:  `Retrieves information about the offered application templates and their supported parameters`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		// When experimentalTemplates is true and runtime is podman, use experimental catalog templates
		// For openshift runtime, always use the older/stable code path regardless of experimental flag
		if experimentalTemplates && vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
			// Use experimental catalog templates listing (architectures and services)
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
	templatesCmd.Flags().BoolVar(&experimentalTemplates, "experimental", false, "Include experimental application templates")

	// Add parameters subcommand
	templatesCmd.AddCommand(appTemplates.NewParametersCmd())
}

// listCatalogTemplates lists architectures, services, and components from the catalog.
func listCatalogTemplates(cmd *cobra.Command) error {
	// Create catalog provider
	provider, err := catalog.NewCatalogProvider()
	if err != nil {
		return fmt.Errorf("failed to create catalog provider: %w", err)
	}

	// Get all data
	architectures, err := provider.ListArchitectures()
	if err != nil {
		return fmt.Errorf("failed to list architectures: %w", err)
	}

	services, err := provider.ListServices()
	if err != nil {
		return fmt.Errorf("failed to list services: %w", err)
	}

	components, err := provider.ListComponents()
	if err != nil {
		return fmt.Errorf("failed to list components: %w", err)
	}

	// Section 1: Deployment Architectures with list of services
	logger.Infoln("Available Deployment Architectures:")
	for _, arch := range architectures {
		displayArchitectureWithServiceList(arch)
	}

	// Section 2: Deployment Services with metadata and required components
	logger.Infoln("\nAvailable Services:")
	for _, svc := range services {
		displayServiceWithComponents(svc, components)
	}

	// Inform user about parameters subcommand
	logger.Infoln("\nTo list supported parameters for each template use: application templates parameters --template <Template ID>\n")

	return nil
}

// displayArchitectureWithServiceList displays an architecture with just the list of service IDs.
func displayArchitectureWithServiceList(arch catalogTypes.Architecture) {
	logger.Infof("- %s (%s)", arch.ID, arch.Name)
	if arch.Description != "" {
		logger.Infof("  Description: %s", arch.Description)
	}

	// Display list of services in this architecture
	if len(arch.Services) > 0 {
		logger.Infoln("  Services:")
		for _, svcRef := range arch.Services {
			logger.Infof("     - %s", svcRef.ID)
		}
	}
}

// displayServiceWithComponents displays a service with its metadata and required components.
func displayServiceWithComponents(svc catalogTypes.Service, components []catalogTypes.Component) {
	logger.Infof("- %s (%s)", svc.ID, svc.Name)
	if svc.Description != "" {
		logger.Infof("  Description: %s", svc.Description)
	}

	// Display component dependencies
	if len(svc.Dependencies) > 0 {
		logger.Infoln("  Required Components:")
		for _, dep := range svc.Dependencies {
			// Find matching components by type
			matchingComps := []string{}
			for _, comp := range components {
				if comp.ComponentType == dep.ID {
					matchingComps = append(matchingComps, comp.ID)
				}
			}

			if len(matchingComps) > 0 {
				logger.Infof("    %s: %s", dep.ID, strings.Join(matchingComps, ", "))
			} else {
				logger.Infof("    %s: (no components available)", dep.ID)
			}
		}
	}
}
