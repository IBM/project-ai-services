import type {
  DeployFormData,
  ComponentConfig,
  ServiceConfig,
} from "@/components/DeployFlow/types";
import type {
  DeployOptionsResponse,
  Service,
  Component,
  Provider,
} from "@/types/digitalAssistants";
import { isInferenceComponent } from "./inferenceComponentHelper";
import { shouldIncludeParam } from "./paramFilter";

/**
 * Determines the component type (llm or reranker) that uses the inference backend
 * for a given service configuration
 */
function getInferenceComponentType(
  serviceDefinition: Service,
  serviceConfig: ServiceConfig,
): string {
  // Default to llm
  let componentType = "llm";

  // Check if service has reranker component
  const hasReranker = serviceDefinition.components.some(
    (c) => c.type === "reranker",
  );

  // Use reranker if available and enabled in config
  if (hasReranker && serviceConfig.components?.reranker?.providerId) {
    componentType = "reranker";
  }

  return componentType;
}

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

  // Only include params if there are any values
  // Params are already filtered in separateParams based on schema defaults
  if (Object.keys(params).length > 0) {
    component.params = params;
  }

  return component;
}

/**
 * Separates inference backend params from service-level params
 * Uses provider and service schemas to accurately classify parameters
 * Inference backend params: defined in provider schema (model, apiKey, etc.)
 * Service-level params: defined in service schema under backend.properties (systemPrompt, etc.)
 */
function separateParams(
  allParams: Record<string, unknown>,
  providerSchemaData: Record<string, unknown> | null,
  serviceSchemaData: Record<string, unknown> | null,
): {
  inferenceBackendParams: Record<string, unknown>;
  serviceParams: Record<string, unknown>;
} {
  if (!allParams || Object.keys(allParams).length === 0) {
    return { inferenceBackendParams: {}, serviceParams: allParams || {} };
  }

  // Get provider schema properties with defaults
  const providerProperties =
    (providerSchemaData?.properties as Record<string, { default?: unknown }>) ||
    {};

  // Get service schema properties with defaults (under backend.properties)
  const serviceProperties: Record<string, { default?: unknown }> = {};
  if (serviceSchemaData?.properties) {
    const properties = serviceSchemaData.properties as Record<string, unknown>;
    if (properties.backend) {
      const backend = properties.backend as Record<string, unknown>;
      if (backend.properties) {
        Object.assign(
          serviceProperties,
          backend.properties as Record<string, { default?: unknown }>,
        );
      }
    }
  }

  // Classify and filter parameters based on schema definitions
  const inferenceBackendParams: Record<string, unknown> = {};
  const serviceParams: Record<string, unknown> = {};

  for (const [key, value] of Object.entries(allParams)) {
    // Check if this is a provider param or service param
    const isProviderParam = key in providerProperties;
    const schemaProperty = isProviderParam
      ? providerProperties[key]
      : serviceProperties[key];

    // Use shouldIncludeParam to filter based on schema defaults
    if (shouldIncludeParam(value, schemaProperty)) {
      if (isProviderParam) {
        inferenceBackendParams[key] = value;
      } else {
        serviceParams[key] = value;
      }
    }
  }

  return { inferenceBackendParams, serviceParams };
}

/**
 * Transforms form data into deployment payload format
 * Completely dynamic - works with any service/component configuration
 * All data comes from formData - no API calls needed
 *
 * Note: Each service sends its own parameters. The backend validates that services
 * sharing the same provider+model have identical parameters and returns an error if not.
 */
export function transformToDeploymentPayload(
  formData: DeployFormData,
  deployOptions: DeployOptionsResponse,
  providerParamsCache: Record<string, Record<string, unknown>>,
  serviceParamsCache: Record<string, Record<string, unknown>>,
): DeploymentPayload {
  const services: DeploymentService[] = [];

  // Process each enabled service
  for (const [serviceId, serviceConfig] of Object.entries(formData.services)) {
    if (!serviceConfig.enabled) {
      continue;
    }

    // Find the service definition in deploy options
    const serviceDefinition = deployOptions.services.find(
      (s) => s.id === serviceId,
    );
    if (!serviceDefinition) {
      continue;
    }

    // Determine component type (llm or reranker)
    // TODO: [Next Release] Replace hardcoded "llm"/"reranker" with constants from a shared file
    const componentType = getInferenceComponentType(
      serviceDefinition,
      serviceConfig,
    );

    // Get cached schemas from store
    const providerKey = `${componentType}:${serviceConfig.inferenceBackend}`;
    const providerSchemaData =
      (providerParamsCache[providerKey] as Record<string, unknown>) || null;
    const serviceSchemaData =
      (serviceParamsCache[serviceId] as Record<string, unknown>) || null;

    // Separate inference backend params from service-level params for this service
    const { inferenceBackendParams, serviceParams } = separateParams(
      serviceConfig.params || {},
      providerSchemaData,
      serviceSchemaData,
    );

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
            inferenceBackendParams, // Pass inference backend params (e.g., API keys)
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
