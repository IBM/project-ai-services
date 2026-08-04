/**
 * Resource Sharing Utility
 * Determines how provider resources should be shared across services
 * Infers sharing behavior from provider parameters (model-based vs instance-based)
 */

// Generates unique key for resource deduplication based on provider model parameter
export function getResourceSharingKey(
  serviceId: string,
  componentType: string,
  providerId: string,
  params: Record<string, unknown>,
): string {
  // Check if provider has model parameter (indicates model-based sharing)
  const modelValue = params.model;

  if (modelValue !== undefined && modelValue !== null && modelValue !== "") {
    // Share resources by provider + model across all services
    // Example: "watsonx-granite-13b-chat" is shared across digitize and chat services
    return `${providerId}-${modelValue}`;
  } else {
    // Each service gets its own instance
    // Example: "digitize-ollama-llama3-llm" is separate from "chat-ollama-llama3-llm"
    return `${serviceId}-${providerId}-${componentType}`;
  }
}

// Checks if provider uses model-based resource sharing across services
export function isModelBasedSharing(params: Record<string, unknown>): boolean {
  return (
    params.model !== undefined && params.model !== null && params.model !== ""
  );
}
