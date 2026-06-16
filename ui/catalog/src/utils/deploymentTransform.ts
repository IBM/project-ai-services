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
import { isInferenceComponent } from "./inferenceComponentHelper";

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
  backend?: Record<string, unknown>;
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
 * For inference components (determined generically), uses inferenceBackend as provider_id if specified
 */
function buildDeploymentComponent(
  componentType: string,
  componentConfig: ComponentConfig,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
  globalComponents: Record<string, ComponentConfig>,
  inferenceBackend?: string,
  inferenceBackendParams?: Record<string, unknown>,
): DeploymentComponent {
  // Determine if this is an inference component using generic logic
  // An inference component has multiple providers with model input parameters
  let componentDefinition: Component | undefined;

  // Find component definition in service or global components
  if (serviceDefinition) {
    componentDefinition = serviceDefinition.components.find(
      (c) => c.type === componentType,
    );
  }
  if (!componentDefinition) {
    componentDefinition = deployOptions.global_components.find(
      (c) => c.type === componentType,
    );
  }

  const isInferenceComp = componentDefinition
    ? isInferenceComponent(componentDefinition)
    : false;

  // For inference components, use inferenceBackend as provider if specified
  // This allows the UI's "Inference Backend" dropdown to control which provider runs the model
  const providerId =
    isInferenceComp && inferenceBackend
      ? inferenceBackend
      : componentConfig.providerId;

  // Get params from component config (already populated when provider was selected)
  let params = { ...componentConfig.params };

  // For inference components using inferenceBackend, merge inference backend params
  // These are params specifically for the inference backend provider (e.g., API keys)
  // NOT all service-level params (which may include service-specific params like systemPrompt)
  if (isInferenceComp && inferenceBackend && inferenceBackendParams) {
    params = {
      ...params,
      ...inferenceBackendParams,
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
 * Separates inference backend params from service-level params
 * Uses a heuristic based on common inference backend parameter patterns
 * Inference backend params: model, apiKey, and provider-specific auth/config params
 * Service-level params: application logic params like systemPrompt, temperature overrides, etc.
 */
function separateParams(
  allParams: Record<string, unknown>,
  inferenceBackendProviderId: string | undefined,
  componentType: string,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
): {
  inferenceBackendParams: Record<string, unknown>;
  serviceParams: Record<string, unknown>;
} {
  if (
    !inferenceBackendProviderId ||
    !allParams ||
    Object.keys(allParams).length === 0
  ) {
    return { inferenceBackendParams: {}, serviceParams: allParams || {} };
  }

  // Find the inference backend provider's component definition
  let componentDefinition: Component | undefined;
  if (serviceDefinition) {
    componentDefinition = serviceDefinition.components.find(
      (c) => c.type === componentType,
    );
  }
  if (!componentDefinition) {
    componentDefinition = deployOptions.global_components.find(
      (c) => c.type === componentType,
    );
  }

  if (!componentDefinition) {
    return { inferenceBackendParams: {}, serviceParams: allParams };
  }

  // Get the inference backend provider
  const provider = componentDefinition.providers.find(
    (p) => p.id === inferenceBackendProviderId,
  );

  // If no provider schema info, assume all params are service-level
  if (!provider?.schema) {
    return { inferenceBackendParams: {}, serviceParams: allParams };
  }

  // Heuristic for separating params:
  // Inference backend params are typically provider-specific configuration:
  // - model: the model identifier
  // - apiKey, api_key: authentication
  // - *Url, *Endpoint: API endpoints
  // - *ProjectId, *Region: cloud provider configs
  //
  // Service-level params are application logic:
  // - systemPrompt, temperature, maxTokens: application behavior
  // - editSystemPrompt: UI control flags
  const inferenceBackendParams: Record<string, unknown> = {};
  const serviceParams: Record<string, unknown> = {};

  const inferenceBackendParamPatterns = [
    "model",
    "apikey",
    "api_key",
    "url",
    "endpoint",
    "projectid",
    "project_id",
    "region",
    "watsonx", // watsonx-specific params
  ];

  for (const [key, value] of Object.entries(allParams)) {
    const lowerKey = key.toLowerCase();
    const isInferenceBackendParam = inferenceBackendParamPatterns.some(
      (pattern) => lowerKey.includes(pattern),
    );

    if (isInferenceBackendParam) {
      inferenceBackendParams[key] = value;
    } else {
      serviceParams[key] = value;
    }
  }

  return { inferenceBackendParams, serviceParams };
}

/**
 * Collects all inference backend params across all enabled services
 * Groups by provider + model combination to ensure consistent params
 * Backend requirement: same provider + same model = same API key
 */
function collectInferenceBackendParams(
  formData: DeployFormData,
  deployOptions: DeployOptionsResponse,
): Record<string, Record<string, unknown>> {
  const backendParamsMap: Record<string, Record<string, unknown>> = {};

  for (const [serviceId, serviceConfig] of Object.entries(formData.services)) {
    if (!serviceConfig.enabled || !serviceConfig.inferenceBackend) continue;

    const serviceDefinition = deployOptions.services.find(
      (s) => s.id === serviceId,
    );
    if (!serviceDefinition) continue;

    // Find the component type that uses the inference backend (llm or reranker)
    let componentType = "llm";
    const hasReranker = serviceDefinition.components.some(
      (c) => c.type === "reranker",
    );
    if (hasReranker && serviceConfig.components?.reranker) {
      componentType = "reranker";
    }

    // Get the model being used
    const componentConfig = serviceConfig.components?.[componentType];
    const model = componentConfig?.params?.model;

    // Separate params for this service
    const { inferenceBackendParams } = separateParams(
      serviceConfig.params || {},
      serviceConfig.inferenceBackend,
      componentType,
      serviceDefinition,
      deployOptions,
    );

    // Store params by provider + model combination
    if (Object.keys(inferenceBackendParams).length > 0 && model) {
      const key = `${serviceConfig.inferenceBackend}:${model}`;
      // Merge params, later services override earlier ones
      backendParamsMap[key] = {
        ...(backendParamsMap[key] || {}),
        ...inferenceBackendParams,
      };
    }
  }

  return backendParamsMap;
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
  // First, collect all inference backend params across all services
  // This ensures consistent params for shared inference backends
  const sharedInferenceBackendParams = collectInferenceBackendParams(
    formData,
    deployOptions,
  );

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

    // Find the component type that uses the inference backend (llm or reranker)
    let componentType = "llm";
    const hasReranker = serviceDefinition.components.some(
      (c) => c.type === "reranker",
    );
    if (hasReranker && serviceConfig.components?.reranker) {
      componentType = "reranker";
    }

    // Get the model being used
    const componentConfig = serviceConfig.components?.[componentType];
    const model = componentConfig?.params?.model;

    // Separate inference backend params from service-level params
    const { serviceParams } = separateParams(
      serviceConfig.params || {},
      serviceConfig.inferenceBackend,
      componentType,
      serviceDefinition,
      deployOptions,
    );

    // Get shared inference backend params for this service's backend + model combination
    let inferenceBackendParams: Record<string, unknown> = {};
    if (serviceConfig.inferenceBackend && model) {
      const key = `${serviceConfig.inferenceBackend}:${model}`;
      inferenceBackendParams = sharedInferenceBackendParams[key] || {};
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
            inferenceBackendParams, // Pass shared inference backend params (e.g., API keys)
          ),
        );
      }
    }

    const deploymentService: DeploymentService = {
      catalog_id: serviceId,
      version: serviceConfig.version || formData.version,
      components,
    };

    // Add backend configuration if service has service-level params
    if (serviceParams && Object.keys(serviceParams).length > 0) {
      deploymentService.backend = serviceParams;
    }

    services.push(deploymentService);
  }

  return {
    name: formData.name,
    catalog_id: deployOptions.id,
    version: formData.version,
    services,
  };
}
