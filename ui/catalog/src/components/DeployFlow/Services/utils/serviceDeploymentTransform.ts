import type {
  DeployFormData,
  ComponentConfig,
} from "@/components/DeployFlow/Shared/types";
import type {
  ServiceDeployOptions,
  ProviderSchema,
  ServiceDeploymentPayload,
  DeploymentComponent,
  DeploymentService,
} from "@/types/api.types";
import { fetchProviderSchema } from "@/api/applications.api";
import { COMPONENT_TYPES } from "@/constants";
import { splitServiceParams } from "@/components/DeployFlow/Shared/utils/paramFilter";

/**
 * Extracts parameters with their defaults from a provider schema
 * Uses cached schema if provided, otherwise fetches it
 */
async function getProviderSchemaParams(
  componentType: string,
  providerId: string,
  cachedSchema?: ProviderSchema | null,
): Promise<Record<string, unknown>> {
  try {
    // Use cached schema if available
    const schema =
      cachedSchema || (await fetchProviderSchema(componentType, providerId));
    const params: Record<string, unknown> = {};

    // Extract all properties with default values from schema
    if (schema?.properties) {
      for (const [key, property] of Object.entries(schema.properties)) {
        if (property && property.default !== undefined) {
          params[key] = property.default;
        }
      }
    }

    return params;
  } catch {
    // If schema fetch fails (e.g., 404 for components without params like vector_store),
    // return empty params object - this is expected behavior
    console.debug(
      `No schema found for ${componentType}/${providerId} - this is normal for components without parameters`,
    );
    return {};
  }
}

/**
 * Gets the provider version from the API response
 */
function getProviderVersion(
  componentType: string,
  providerId: string,
  deployOptions: ServiceDeployOptions,
): string {
  // Find in service components
  const component = deployOptions.components.find(
    (c) => c.type === componentType,
  );
  const provider = component?.providers.find((p) => p.id === providerId);
  if (provider?.version) {
    return provider.version;
  }

  // Final fallback
  return "1.0.0";
}

// Builds a deployment component from ComponentConfig
async function buildDeploymentComponent(
  componentType: string,
  componentConfig: ComponentConfig,
  deployOptions: ServiceDeployOptions,
  schemaPromise: Promise<Record<string, unknown>>,
  extraParams?: Record<string, unknown>,
): Promise<DeploymentComponent> {
  const schemaParams = await schemaPromise;

  // Start from schema defaults, then apply user values and any extra params (e.g. credentials).
  // Only non-empty user values are written so empty strings don't override a valid default.
  const userValues = { ...componentConfig.params, ...(extraParams || {}) };
  const params = { ...schemaParams };
  for (const [key, value] of Object.entries(userValues)) {
    if (value !== undefined && value !== null && value !== "") {
      params[key] = value;
    }
  }

  // Build base component
  const component: DeploymentComponent = {
    component_type: componentType,
    provider_id: componentConfig.providerId,
    version: getProviderVersion(
      componentType,
      componentConfig.providerId,
      deployOptions,
    ),
  };

  // Only include params if there are actual parameters
  // This prevents sending empty params objects for components like vector_store
  if (Object.keys(params).length > 0) {
    component.params = params;
  }

  return component;
}

export async function transformToDeploymentPayload(
  formData: DeployFormData,
  deployOptions: ServiceDeployOptions,
  cachedSchemas?: Record<string, ProviderSchema>,
  serviceId?: string | null,
): Promise<ServiceDeploymentPayload> {
  const services: DeploymentService[] = [];

  // Collect all unique provider/component combinations to fetch in parallel
  const schemaFetchPromises = new Map<
    string,
    Promise<Record<string, unknown>>
  >();

  const getSchemaPromise = (
    componentType: string,
    providerId: string,
    currentServiceId: string,
  ) => {
    const key = `${componentType}:${providerId}`;
    if (!schemaFetchPromises.has(key)) {
      // Check if we have a cached schema for this component/provider
      // Schemas are stored with key format: serviceId:componentType:providerId
      const cacheKey = `${currentServiceId}:${componentType}:${providerId}`;
      const cachedSchema = cachedSchemas?.[cacheKey] || null;

      schemaFetchPromises.set(
        key,
        getProviderSchemaParams(componentType, providerId, cachedSchema),
      );
    }
    return schemaFetchPromises.get(key)!;
  };

  for (const [currentServiceId, serviceConfig] of Object.entries(
    formData.services,
  )) {
    if (!serviceConfig.enabled) continue;

    const inferenceComponentType =
      COMPONENT_TYPES.LLM in serviceConfig.components
        ? COMPONENT_TYPES.LLM
        : COMPONENT_TYPES.RERANKER in serviceConfig.components
          ? COMPONENT_TYPES.RERANKER
          : null;

    const inferenceProviderId = inferenceComponentType
      ? serviceConfig.components[inferenceComponentType]?.providerId
      : null;
    const inferenceProviderSchema =
      inferenceComponentType && inferenceProviderId && serviceId
        ? (cachedSchemas?.[
            `${serviceId}:${inferenceComponentType}:${inferenceProviderId}`
          ] ?? null)
        : null;

    const { inferenceCredentialParams: credentialParams } = splitServiceParams(
      serviceConfig.params || {},
      null,
      inferenceProviderSchema,
    );

    const componentPromises: Promise<DeploymentComponent>[] = [];

    for (const [componentType, componentConfig] of Object.entries(
      serviceConfig.components,
    )) {
      const isInferenceComp = componentType === inferenceComponentType;
      componentPromises.push(
        buildDeploymentComponent(
          componentType,
          componentConfig,
          deployOptions,
          getSchemaPromise(
            componentType,
            componentConfig.providerId,
            currentServiceId,
          ),
          isInferenceComp && Object.keys(credentialParams).length > 0
            ? credentialParams
            : undefined,
        ),
      );
    }

    // Wait for all components of this service to be ready
    const components = await Promise.all(componentPromises);

    services.push({
      catalog_id: currentServiceId,
      version: serviceConfig.version,
      components,
    });
  }

  return {
    name: formData.name,
    catalog_id: deployOptions.id,
    version: formData.version,
    deployment_type: "service",
    services,
  };
}
