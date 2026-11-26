package appcreate

import (
	"fmt"
	"strings"

	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	paramSplitParts    = 2
	expectedParamParts = 2
)

// extractServiceParams extracts service-level parameters from allParams, excluding component params.
// Format: {serviceID}.{param} -> {param}.
// Excludes: {serviceID}.{componentType}.{param} (those are component params).
// Nested dot-notation keys (e.g. backend.systemPrompt) are expanded into nested maps.
func extractServiceParams(serviceID string, components []catalogTypes.DeployOptionsComponent, allParams map[string]string) map[string]any {
	serviceParams := make(map[string]any)
	servicePrefix := serviceID + "."

	// Build a set of component types for this service.
	componentTypes := make(map[string]bool)
	for _, comp := range components {
		componentTypes[comp.Type] = true
	}

	for key, value := range allParams {
		after, ok := strings.CutPrefix(key, servicePrefix)
		if !ok {
			continue
		}

		// Check if this is a component parameter by seeing if it starts with a known component type.
		isComponentParam := isComponentParameter(after, componentTypes)

		// Only add to service params if it's not a component param.
		if !isComponentParam {
			setNestedParam(serviceParams, after, value)
		}
	}

	return serviceParams
}

// setNestedParam sets a value in a nested map using a dot-notation key.
// e.g. setNestedParam(m, "backend.systemPrompt", "hello") → m["backend"]["systemPrompt"] = "hello".
func setNestedParam(m map[string]any, dotKey string, value string) {
	parts := strings.SplitN(dotKey, ".", paramSplitParts)
	if len(parts) == 1 {
		m[dotKey] = value

		return
	}

	nested, ok := m[parts[0]].(map[string]any)
	if !ok {
		nested = make(map[string]any)
		m[parts[0]] = nested
	}

	setNestedParam(nested, parts[1], value)
}

// isComponentParameter checks if a parameter belongs to a component.
func isComponentParameter(param string, componentTypes map[string]bool) bool {
	for compType := range componentTypes {
		if strings.HasPrefix(param, compType+".") {
			return true
		}
	}

	return false
}

// extractComponentParamsForService extracts parameters for a specific component type from allParams.
// Supports provider-specific params:
// - Provider only: {componentType}.{providerID} (e.g., llm.vllm-cpu) - selects provider with defaults.
// - Provider with params: {componentType}.{providerID}.{param} (e.g., vector_store.opensearch.replicas).
// - Service-specific: {serviceID}.{componentType}.{providerID}[.{param}] (e.g., chat.llm.vllm-cpu).
// Returns a map with provider as key and params as value.
// Warns if a provider is explicitly set to false with no other provider selected.
func extractComponentParamsForService(serviceID string, componentType string, allParams map[string]string) map[string]map[string]string {
	providerParams := make(map[string]map[string]string)
	falseProviders := make(map[string]bool)

	// Extract global component params: {componentType}.{providerID}[.{param}].
	extractProviderParams(componentType+".", allParams, providerParams, falseProviders)

	// Extract service-specific component params (these override global).
	extractProviderParams(serviceID+"."+componentType+".", allParams, providerParams, falseProviders)

	// Warn if any provider was explicitly set to false but no other provider was selected.
	if len(falseProviders) > 0 && len(providerParams) == 0 {
		for providerID := range falseProviders {
			logger.Warningf("Provider '%s' for component type '%s' is set to 'false' but no other provider was specified; the default provider will be used\n", providerID, componentType)
		}
	}

	return providerParams
}

// extractProviderParams extracts provider parameters from allParams with the given prefix.
// For bare provider keys (e.g., llm.vllm-cpu=true/false):
//   - "true" selects the provider.
//   - "false" records the provider in falseProviders and skips it.
//   - Any other value logs a warning and is treated as "false".
func extractProviderParams(prefix string, allParams map[string]string, providerParams map[string]map[string]string, falseProviders map[string]bool) {
	for key, value := range allParams {
		after, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}

		// Split to get providerID and optional param.
		parts := strings.SplitN(after, ".", paramSplitParts)
		if len(parts) < 1 {
			continue
		}

		providerID := parts[0]

		// Bare provider key (e.g., llm.vllm-cpu=true/false).
		if len(parts) != expectedParamParts && !strings.EqualFold(value, "true") {
			// Any value other than "true" opts out of this provider.
			if !strings.EqualFold(value, "false") {
				logger.Warningf("Invalid value '%s' for provider parameter '%s': expected 'true' or 'false', treating as 'false'\n", value, key)
			}

			falseProviders[providerID] = true

			continue
		}

		// Ensure provider entry exists.
		if providerParams[providerID] == nil {
			providerParams[providerID] = make(map[string]string)
		}

		// Store nested param (e.g., vector_store.opensearch.replicas=3).
		if len(parts) == expectedParamParts {
			providerParams[providerID][parts[1]] = value
		}
	}
}

// selectProviderFromDeployOptions determines the provider ID for a component using deploy options.
// Priority:
// 1. User-specified provider (matched against deploy options).
// 2. Default-marked provider.
// 3. Provider is "vllm-spyre".
// 4. First available provider.
// Returns an error if multiple providers are explicitly selected for the same component type.
func selectProviderFromDeployOptions(compDeployOpt catalogTypes.DeployOptionsComponent, providerParams map[string]map[string]string) (string, map[string]string, error) {
	matchedProvider, defaultProvider, spyreProvider, firstProvider, err := collectProviderSelection(compDeployOpt, providerParams)
	if err != nil {
		return "", nil, err
	}

	if matchedProvider != "" {
		return matchedProvider, providerParams[matchedProvider], nil
	}

	if defaultProvider != "" {
		return defaultProvider, make(map[string]string), nil
	}

	if spyreProvider != "" {
		return spyreProvider, make(map[string]string), nil
	}

	return firstProvider, make(map[string]string), nil
}

func collectProviderSelection(compDeployOpt catalogTypes.DeployOptionsComponent, providerParams map[string]map[string]string) (string, string, string, string, error) {
	var matchedProvider string
	var defaultProvider string
	var spyreProvider string
	var firstProvider string

	for _, p := range compDeployOpt.Providers {
		if firstProvider == "" {
			firstProvider = p.ID
		}

		if spyreProvider == "" && p.ID == "vllm-spyre" {
			spyreProvider = p.ID
		}

		if p.Default {
			defaultProvider = p.ID
		}

		if _, selected := providerParams[p.ID]; !selected {
			continue
		}

		if matchedProvider != "" {
			return "", "", "", "", fmt.Errorf(
				"multiple providers specified for component type '%s': '%s' and '%s'. "+
					"Only one provider can be selected per component type",
				compDeployOpt.Type, matchedProvider, p.ID)
		}

		matchedProvider = p.ID
	}

	return matchedProvider, defaultProvider, spyreProvider, firstProvider, nil
}

// extractDefaultsFromSchema extracts default values from a JSON schema.
func extractDefaultsFromSchema(schema map[string]any) map[string]any {
	defaults := make(map[string]any)

	// Check if schema has properties
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return defaults
	}

	// Extract default value for each property
	for propName, propValue := range properties {
		if propMap, ok := propValue.(map[string]any); ok {
			if defaultValue, hasDefault := propMap["default"]; hasDefault {
				defaults[propName] = defaultValue
			}
		}
	}

	return defaults
}
