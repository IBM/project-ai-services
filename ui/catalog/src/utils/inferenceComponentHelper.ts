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
    // Schema is URL or invalid JSON - can't determine from schema alone
    return false;
  }
}

// Determines if component is an inference component
// Uses schema-based detection first, with type-based fallback for safety
export function isInferenceComponent(component: Component): boolean {
  // Primary: Generic detection based on schema (any provider with model input)
  const hasModelInput = component.providers.some(providerExpectsModelInput);
  if (hasModelInput) {
    return true;
  }

  // Fallback: Known inference component types for backward compatibility
  // This ensures LLM/reranker always work even with:
  // - Single provider
  // - URL schemas
  // - Schema parse failures
  // TODO: [Next Release] Replace hardcoded "llm"/"reranker" with constants from a shared file
  if (component.type === "llm" || component.type === "reranker") {
    return true;
  }

  return false;
}

// Gets default inference backend provider ID for a service
// Returns the provider ID of the first inference component found
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

  return undefined;
}
