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
	schema, err := provider.GetServiceParams(serviceID)
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
	schema, err := provider.GetServiceParams(serviceID)
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

				schema, err := provider.GetComponentProviderParams(comp.ComponentType, comp.ID)
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

	displayPropertiesRecursive(schema, properties, prefix)
}

// displayPropertiesRecursive recursively displays properties, handling nested objects.
// It skips fields marked with "x-ui-only": true (UI-only fields with no CLI meaning)
// and collects all properties from the schema including those in conditional branches.
func displayPropertiesRecursive(parentSchema map[string]any, properties map[string]any, prefix string) {
	// Collect all properties including those from conditional branches
	allProperties := collectAllProperties(parentSchema, properties)

	// Display each property
	for paramName, propValue := range allProperties {
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
				displayPropertiesRecursive(prop, nestedProps, fmt.Sprintf("%s.%s", prefix, paramName))

				continue
			}
		}

		// Display the parameter with its description and default value
		if defaultValue, hasDefault := prop["default"]; hasDefault && defaultValue != nil && defaultValue != "" {
			logger.Infof("  %s.%s: %s (Default: %v)", prefix, paramName, description, defaultValue)
		} else {
			logger.Infof("  %s.%s: %s", prefix, paramName, description)
		}
	}
}

// collectAllProperties gathers all properties from a schema, including those defined
// in conditional branches (oneOf, anyOf, allOf, dependencies, if/then/else).
// It merges properties from all branches, with later definitions taking precedence.
func collectAllProperties(parentSchema map[string]any, baseProperties map[string]any) map[string]any {
	result := initializeBaseProperties(baseProperties)

	// Collect from conditional branches at current schema level
	collectConditionalBranches(parentSchema, result)

	// Check dependencies (legacy pattern)
	collectDependencyBranches(parentSchema, result)

	// Check if/then/else
	collectIfThenElseBranches(parentSchema, result)

	return result
}

// initializeBaseProperties creates the initial result map with base properties,
// skipping empty placeholders.
func initializeBaseProperties(baseProperties map[string]any) map[string]any {
	result := make(map[string]any)
	for name, prop := range baseProperties {
		if isValidProperty(prop) {
			result[name] = prop
		}
	}

	return result
}

// isValidProperty checks if a property is valid (non-empty map).
func isValidProperty(prop any) bool {
	propMap, ok := prop.(map[string]any)

	return ok && len(propMap) > 0
}

// collectConditionalBranches collects properties from oneOf/anyOf/allOf branches.
func collectConditionalBranches(parentSchema map[string]any, result map[string]any) {
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		if branches, ok := parentSchema[keyword].([]any); ok {
			mergePropertiesFromBranches(branches, result)
		}
	}
}

// collectDependencyBranches collects properties from dependency schemas.
func collectDependencyBranches(parentSchema map[string]any, result map[string]any) {
	deps, ok := parentSchema["dependencies"].(map[string]any)
	if !ok {
		return
	}

	for _, depValue := range deps {
		depSchema, ok := depValue.(map[string]any)
		if !ok {
			continue
		}
		collectConditionalBranches(depSchema, result)
	}
}

// collectIfThenElseBranches collects properties from if/then/else branches.
func collectIfThenElseBranches(parentSchema map[string]any, result map[string]any) {
	for _, keyword := range []string{"then", "else"} {
		if branch, ok := parentSchema[keyword].(map[string]any); ok {
			mergePropertiesFromBranch(branch, result)
		}
	}
}

// mergePropertiesFromBranches merges properties from multiple branches into result.
func mergePropertiesFromBranches(branches []any, result map[string]any) {
	for _, branch := range branches {
		branchMap, ok := branch.(map[string]any)
		if !ok {
			continue
		}
		mergePropertiesFromBranch(branchMap, result)
	}
}

// mergePropertiesFromBranch merges properties from a single branch into result.
func mergePropertiesFromBranch(branchMap map[string]any, result map[string]any) {
	branchProps, ok := branchMap["properties"].(map[string]any)
	if !ok {
		return
	}

	for name, prop := range branchProps {
		mergeProperty(name, prop, result)
	}
}

// mergeProperty merges a single property into result, preferring more detailed definitions.
func mergeProperty(name string, prop any, result map[string]any) {
	existing, exists := result[name]
	if !exists {
		result[name] = prop

		return
	}

	// Replace if new property has more detail
	if shouldReplaceProperty(existing, prop) {
		result[name] = prop
	}
}

// shouldReplaceProperty determines if an existing property should be replaced
// with a new one based on detail level (map size).
func shouldReplaceProperty(existing, new any) bool {
	existingMap, existingOk := existing.(map[string]any)
	newMap, newOk := new.(map[string]any)
	return existingOk && newOk && len(newMap) > len(existingMap)
}
