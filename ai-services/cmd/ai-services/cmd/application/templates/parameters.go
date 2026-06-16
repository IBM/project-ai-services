package templates

import (
	"context"
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

// NewParametersCmd creates the parameters subcommand.
func NewParametersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parameters",
		Short: "Display supported parameters for a specific template",
		Long:  `Display all supported parameters for a specific template ID (service or architecture) from the catalog`,
		Example: `  # Display parameters for a service
  ai-services application templates parameters --template digitize --runtime podman

  # Display parameters for an architecture
  ai-services application templates parameters --template rag --runtime podman`,
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
	_ = cmd.MarkFlagRequired("template")

	return cmd
}

// displayServiceParameters displays all parameters for a specific service.
func displayServiceParameters(provider *catalog.CatalogProvider, serviceID string, dependencies []catalogTypes.DependencyReference) error {
	logger.Infof("Supported Parameters for '%s':", serviceID)

	// Display service's own parameters
	schema, err := provider.GetServiceParams(context.Background(), serviceID)
	if err == nil && schema != nil {
		displaySchemaParameters(schema, serviceID)
	}

	// Display component parameters
	return displayComponentsParameters(provider, dependencies, nil)
}

// displayArchitectureParameters displays all parameters for all services in an architecture.
func displayArchitectureParameters(provider *catalog.CatalogProvider, archID string, services []catalogTypes.ServiceReference) error {
	logger.Infof("Supported Parameters for '%s':", archID)

	// Track displayed components to avoid duplicates
	displayedComponents := make(map[string]bool)

	// Display parameters for each service in the architecture
	for _, svcRef := range services {
		if err := displayServiceInArchitecture(provider, svcRef.ID, displayedComponents); err != nil {
			continue
		}
	}

	return nil
}

// displayServiceInArchitecture displays parameters for a single service within an architecture.
func displayServiceInArchitecture(provider *catalog.CatalogProvider, serviceID string, displayedComponents map[string]bool) error {
	// Load the service to get its dependencies
	service, err := provider.LoadService(serviceID)
	if err != nil {
		return err
	}

	// Display service parameters
	schema, err := provider.GetServiceParams(context.Background(), serviceID)
	if err == nil && schema != nil {
		displaySchemaParameters(schema, serviceID)
	}

	// Display component parameters for this service
	return displayComponentsParameters(provider, service.Dependencies, displayedComponents)
}

// displayComponentsParameters displays parameters for components based on dependencies.
// If displayedComponents map is provided, it will track and skip duplicates.
func displayComponentsParameters(provider *catalog.CatalogProvider, dependencies []catalogTypes.DependencyReference, displayedComponents map[string]bool) error {
	if len(dependencies) == 0 {
		return nil
	}

	components, err := provider.ListComponents()
	if err != nil {
		return fmt.Errorf("failed to list components: %w", err)
	}

	for _, dep := range dependencies {
		// Find all components of this type
		for _, comp := range components {
			if comp.ComponentType == dep.ID {
				componentKey := fmt.Sprintf("%s.%s", comp.ComponentType, comp.ID)

				// Skip if already displayed (only when tracking duplicates)
				if displayedComponents != nil {
					if displayedComponents[componentKey] {
						continue
					}
					displayedComponents[componentKey] = true
				}

				schema, err := provider.GetComponentProviderParams(context.Background(), comp.ComponentType, comp.ID)
				if err == nil && schema != nil {
					displaySchemaParameters(schema, componentKey)
				}
			}
		}
	}

	return nil
}

// Made with Bob

// displaySchemaParameters displays parameters from a schema with the given prefix.
func displaySchemaParameters(schema map[string]any, prefix string) {
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) == 0 {
		return
	}

	displayPropertiesRecursive(properties, prefix)
}

// displayPropertiesRecursive recursively displays properties, handling nested objects.
// It skips fields marked with "x-ui-only": true (UI-only fields with no CLI meaning).
func displayPropertiesRecursive(properties map[string]any, prefix string) {
	for paramName, propValue := range properties {
		prop, ok := propValue.(map[string]any)
		if !ok {
			continue
		}

		// Skip fields explicitly marked as UI-only
		if uiOnly, _ := prop["x-ui-only"].(bool); uiOnly {
			continue
		}

		propType, _ := prop["type"].(string)
		description, _ := prop["description"].(string)

		// If this is an object type with nested properties, recurse into it
		if propType == "object" {
			if nestedProps, ok := prop["properties"].(map[string]any); ok {
				displayPropertiesRecursive(nestedProps, fmt.Sprintf("%s.%s", prefix, paramName))

				continue
			}
		}

		// Append default value if present and not empty
		if defaultValue, hasDefault := prop["default"]; hasDefault && defaultValue != nil && defaultValue != "" {
			logger.Infof("  %s.%s: %s (Default: %v)", prefix, paramName, description, defaultValue)
		} else {
			logger.Infof("  %s.%s: %s", prefix, paramName, description)
		}
	}
}
