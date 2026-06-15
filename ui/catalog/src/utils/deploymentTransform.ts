import type {
  DeployFormData,
  ComponentConfig,
} from "@/components/DeployFlow/types";
import type {
  DeployOptionsResponse,
  Service,
  Component,
  Provider,
} from "@/types/digitalAssistants";

interface DeploymentComponent {
  component_type: string;
  provider_id: string;
  version: string;
  params?: Record<string, unknown>;
}

interface DeploymentService {
  catalog_id: string;
  version: string;
  components: DeploymentComponent[];
}

export interface DeploymentPayload {
  name: string;
  catalog_id: string;
  version: string;
  services: DeploymentService[];
}

/**
 * Gets the provider version from the API response
 * Searches service-specific components first, then falls back to global components
 * Throws error if version not found - version must come from API
 */
function getProviderVersion(
  componentType: string,
  providerId: string,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
): string {
  // First, try to find in service-specific components
  if (serviceDefinition) {
    const component = serviceDefinition.components.find(
      (c: Component) => c.type === componentType,
    );
    const provider = component?.providers.find(
      (p: Provider) => p.id === providerId,
    );
    if (provider?.version) {
      return provider.version;
    }
  }

  // Fall back to global components
  const globalComponent = deployOptions.global_components.find(
    (c: Component) => c.type === componentType,
  );
  const globalProvider = globalComponent?.providers.find(
    (p: Provider) => p.id === providerId,
  );
  if (globalProvider?.version) {
    return globalProvider.version;
  }

  // Version must come from API - throw error if not found
  throw new Error(
    `Provider version not found in API response for component type "${componentType}" and provider "${providerId}". ` +
      `This indicates a configuration issue - all provider versions must be defined in the API response.`,
  );
}

/**
 * Builds a deployment component from component configuration
 * All data comes from formData - no API calls needed
 * For LLM/reranker components, uses inferenceBackend as provider_id if specified
 */
function buildDeploymentComponent(
  componentType: string,
  componentConfig: ComponentConfig,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
  globalComponents: Record<string, ComponentConfig>,
  inferenceBackend?: string,
  serviceLevelParams?: Record<string, unknown>,
): DeploymentComponent {
  // For LLM and reranker components, use inferenceBackend as provider if specified
  // This allows the UI's "Inference Backend" dropdown to control which provider runs the model
  const isInferenceComponent =
    componentType === "llm" || componentType === "reranker";
  const providerId =
    isInferenceComponent && inferenceBackend
      ? inferenceBackend
      : componentConfig.providerId;

  // Get params from component config (already populated when provider was selected)
  let params = { ...componentConfig.params };

  // For inference components, merge service-level params (e.g., watsonx credentials)
  // These are entered via DynamicSchemaFields when selecting the inference backend
  if (isInferenceComponent && serviceLevelParams) {
    params = {
      ...serviceLevelParams,
      ...params,
    };
  }

  // For global components, merge with global component params
  const isGlobalComponent = deployOptions.global_components.some(
    (gc) => gc.type === componentType,
  );
  if (isGlobalComponent && globalComponents[componentType]) {
    params = {
      ...globalComponents[componentType].params,
      ...params,
    };
  }

  // Build component
  const component: DeploymentComponent = {
    component_type: componentType,
    provider_id: providerId,
    version: getProviderVersion(
      componentType,
      providerId,
      serviceDefinition,
      deployOptions,
    ),
  };

  // Only include params if there are any non-empty values
  if (Object.keys(params).length > 0) {
    component.params = params;
  }

  return component;
}

/**
 * Transforms form data into deployment payload format
 * Completely dynamic - works with any service/component configuration
 * All data comes from formData - no API calls needed
 */
export function transformToDeploymentPayload(
  formData: DeployFormData,
  deployOptions: DeployOptionsResponse,
): DeploymentPayload {
  const services: DeploymentService[] = [];

  // Process each enabled service dynamically
  for (const [serviceId, serviceConfig] of Object.entries(formData.services)) {
    if (!serviceConfig.enabled) continue;

    // Find the service definition in deploy options
    const serviceDefinition = deployOptions.services.find(
      (s) => s.id === serviceId,
    );
    if (!serviceDefinition) {
      console.warn(`Service definition not found for: ${serviceId}`);
      continue;
    }

    const components: DeploymentComponent[] = [];

    // Build components dynamically from service configuration
    // Iterate through the service definition to maintain correct order
    for (const componentDef of serviceDefinition.components) {
      const componentConfig = serviceConfig.components[componentDef.type];

      if (componentConfig && componentConfig.providerId) {
        components.push(
          buildDeploymentComponent(
            componentDef.type,
            componentConfig,
            serviceDefinition,
            deployOptions,
            formData.globalComponents,
            serviceConfig.inferenceBackend, // Pass inference backend for LLM/reranker components
            serviceConfig.params, // Pass service-level params (e.g., watsonx credentials)
          ),
        );
      }
    }

    const deploymentService: DeploymentService = {
      catalog_id: serviceId,
      version: serviceConfig.version || formData.version,
      components,
    };

    services.push(deploymentService);
  }

  return {
    name: formData.name,
    catalog_id: deployOptions.id,
    version: formData.version,
    services,
  };
}
