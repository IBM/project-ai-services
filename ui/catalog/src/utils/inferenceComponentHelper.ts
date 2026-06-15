/**
 * Helper utilities for determining inference components.
 * Inference components have multiple providers with model input parameters.
 */

import type { Component, Provider } from "@/types/digitalAssistants";

// Checks if provider schema expects model input
function providerExpectsModelInput(provider: Provider): boolean {
  if (!provider.schema) {
    return false;
  }

  try {
    const schema = JSON.parse(provider.schema);

    // Check if schema has a "model" property
    if (schema.properties && "model" in schema.properties) {
      return true;
    }

    // Check if "model" is in required fields
    if (Array.isArray(schema.required) && schema.required.includes("model")) {
      return true;
    }

    return false;
  } catch {
    // Schema might be a URL or invalid JSON, not a JSON schema
    return false;
  }
}

// Determines if component is an inference component (multiple providers with model input)
export function isInferenceComponent(component: Component): boolean {
  if (component.providers.length <= 1) {
    return false;
  }
  return component.providers.some(providerExpectsModelInput);
}

// Gets default inference backend provider ID for a service
// Returns the first component with multiple providers that has model input
export function getDefaultInferenceBackendProviderId(
  components: Component[],
): string | undefined {
  for (const component of components) {
    if (isInferenceComponent(component)) {
      const defaultProvider =
        component.providers.find((p) => p.default) || component.providers[0];
      return defaultProvider?.id;
    }
  }

  // Fallback: if no component with model input found, check for any component with multiple providers
  // This handles cases where schema parsing fails but we still want to show inference backend
  for (const component of components) {
    if (component.providers.length > 1) {
      const defaultProvider =
        component.providers.find((p) => p.default) || component.providers[0];
      return defaultProvider?.id;
    }
  }

  return undefined;
}
