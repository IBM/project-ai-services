import type {
  DeployFormData,
  ComponentConfig,
} from "@/components/DeployFlow/Shared/types";
import type {
  DeployOptionsResponse,
  DeployOptionsService as Service,
  DeployOptionsComponent as Component,
  Provider,
  ArchitectureDeploymentPayload,
  DeploymentComponent,
  DeploymentService,
  ProviderSchema,
} from "@/types/api.types";
import { COMPONENT_TYPES } from "@/constants";
import { splitServiceParams } from "@/components/DeployFlow/Shared/utils/paramFilter";

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
 * Builds a deployment component. extraParams carries inference credential params
 * that are merged on top of the component's own params.
 */
function buildDeploymentComponent(
  componentType: string,
  componentConfig: ComponentConfig,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
  globalComponents: Record<string, ComponentConfig>,
  extraParams?: Record<string, unknown>,
): DeploymentComponent {
  const providerId = componentConfig.providerId;

  let params = { ...componentConfig.params, ...(extraParams || {}) };

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

  if (Object.keys(params).length > 0) {
    component.params = params;
  }

  return component;
}

export function transformToDeploymentPayload(
  formData: DeployFormData,
  deployOptions: DeployOptionsResponse,
  serviceSchemas: Record<string, ProviderSchema> = {},
  providerParamsByType: Record<string, Record<string, ProviderSchema>> = {},
): ArchitectureDeploymentPayload {
  const services: DeploymentService[] = [];

  for (const [serviceId, serviceConfig] of Object.entries(formData.services)) {
    if (!serviceConfig.enabled) {
      continue;
    }

    const serviceDefinition = deployOptions.services.find(
      (s) => s.id === serviceId,
    );
    if (!serviceDefinition) {
      continue;
    }

    // llm takes priority; fall back to reranker
    const inferenceComponentType =
      (
        serviceDefinition.components.find(
          (c) => c.type === COMPONENT_TYPES.LLM,
        ) ??
        serviceDefinition.components.find(
          (c) => c.type === COMPONENT_TYPES.RERANKER,
        )
      )?.type ?? null;

    const inferenceProviderId = inferenceComponentType
      ? serviceConfig.components[inferenceComponentType]?.providerId
      : null;
    const inferenceProviderSchema =
      inferenceComponentType && inferenceProviderId
        ? (providerParamsByType[inferenceComponentType]?.[
            inferenceProviderId
          ] ?? null)
        : null;

    const { serviceBackendParams, inferenceCredentialParams } =
      splitServiceParams(
        serviceConfig.params || {},
        serviceSchemas[serviceId] ?? null,
        inferenceProviderSchema,
      );

    const components: DeploymentComponent[] = [];

    for (const componentDef of serviceDefinition.components) {
      const componentConfig = serviceConfig.components[componentDef.type];

      if (componentConfig && componentConfig.providerId) {
        const isInferenceComp = componentDef.type === inferenceComponentType;
        components.push(
          buildDeploymentComponent(
            componentDef.type,
            componentConfig,
            serviceDefinition,
            deployOptions,
            formData.globalComponents,
            isInferenceComp && Object.keys(inferenceCredentialParams).length > 0
              ? inferenceCredentialParams
              : undefined,
          ),
        );
      }
    }

    const deploymentService: DeploymentService = {
      catalog_id: serviceId,
      version: serviceConfig.version || formData.version,
      components,
    };

    if (Object.keys(serviceBackendParams).length > 0) {
      deploymentService.params = {
        backend: serviceBackendParams,
      };
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
