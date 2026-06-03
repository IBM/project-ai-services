package templates

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	templateID string
)

// NewParametersCmd creates the parameters subcommand
func NewParametersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parameters",
		Short: "Display supported parameters for a specific template",
		Long:  `Display all supported parameters for a specific template ID (service or architecture) from the catalog`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			// Check runtime - only supported for Podman
			if vars.RuntimeFactory.GetRuntimeType() != types.RuntimeTypePodman {
				return fmt.Errorf("templates parameters subcommand is only supported for Podman runtime")
			}

			if templateID == "" {
				return fmt.Errorf("--template flag is required")
			}

			// Create catalog provider
			provider, err := catalog.NewCatalogProvider()
			if err != nil {
				return fmt.Errorf("failed to create catalog provider: %w", err)
			}

			// Try to load as architecture first
			arch, err := provider.LoadArchitecture(templateID)
			if err == nil {
				return displayArchitectureParameters(provider, templateID, arch.Services)
			}

			// Try to load as service
			service, err := provider.LoadService(templateID)
			if err == nil {
				return displayServiceParameters(provider, templateID, service.Dependencies)
			}

			return fmt.Errorf("template '%s' not found as service or architecture", templateID)
		},
	}

	cmd.Flags().StringVar(&templateID, "template", "", "Template ID (service or architecture)")
	cmd.MarkFlagRequired("template")

	return cmd
}

// displayServiceParameters displays all parameters for a specific service
func displayServiceParameters(provider *catalog.CatalogProvider, serviceID string, dependencies []catalogTypes.DependencyReference) error {
	logger.Infof("Supported Parameters for '%s':", serviceID)

	// Display service's own parameters
	schema, err := provider.GetServiceParams(serviceID)
	if err == nil && schema != nil {
		displaySchemaParameters(schema, serviceID)
	}

	// Display component parameters
	if len(dependencies) > 0 {
		components, err := provider.ListComponents()
		if err != nil {
			return fmt.Errorf("failed to list components: %w", err)
		}

		for _, dep := range dependencies {
			// Find all components of this type
			for _, comp := range components {
				if comp.ComponentType == dep.ID {
					schema, err := provider.GetComponentProviderParams(comp.ComponentType, comp.ID)
					if err == nil && schema != nil {
						displaySchemaParameters(schema, fmt.Sprintf("%s.%s", comp.ComponentType, comp.ID))
					}
				}
			}
		}
	}

	return nil
}

// displayArchitectureParameters displays all parameters for all services in an architecture
func displayArchitectureParameters(provider *catalog.CatalogProvider, archID string, services []catalogTypes.ServiceReference) error {
	logger.Infof("Supported Parameters for '%s':", archID)

	// Get all components for later use
	components, err := provider.ListComponents()
	if err != nil {
		return fmt.Errorf("failed to list components: %w", err)
	}

	// Track displayed components to avoid duplicates
	displayedComponents := make(map[string]bool)

	// Display parameters for each service in the architecture
	for _, svcRef := range services {
		// Load the service to get its dependencies
		service, err := provider.LoadService(svcRef.ID)
		if err != nil {
			continue
		}

		// Display service parameters
		schema, err := provider.GetServiceParams(svcRef.ID)
		if err == nil && schema != nil {
			displaySchemaParameters(schema, svcRef.ID)
		}

		// Display component parameters for this service
		for _, dep := range service.Dependencies {
			for _, comp := range components {
				if comp.ComponentType == dep.ID {
					componentKey := fmt.Sprintf("%s.%s", comp.ComponentType, comp.ID)

					// Skip if already displayed
					if displayedComponents[componentKey] {
						continue
					}
					displayedComponents[componentKey] = true

					schema, err := provider.GetComponentProviderParams(comp.ComponentType, comp.ID)
					if err == nil && schema != nil {
						displaySchemaParameters(schema, componentKey)
					}
				}
			}
		}
	}

	return nil
}

// Made with Bob

// displaySchemaParameters displays parameters from a schema with the given prefix
func displaySchemaParameters(schema map[string]any, prefix string) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		return
	}

	for paramName, propValue := range properties {
		prop, ok := propValue.(map[string]any)
		if !ok {
			continue
		}

		description, _ := prop["description"].(string)

		// Append default value if present and not empty
		if defaultValue, hasDefault := prop["default"]; hasDefault && defaultValue != nil && defaultValue != "" {
			logger.Infof("  %s.%s: %s (Default: %v)", prefix, paramName, description, defaultValue)
		} else {
			logger.Infof("  %s.%s: %s", prefix, paramName, description)
		}
	}
}
