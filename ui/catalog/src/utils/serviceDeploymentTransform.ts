import type {
  DeployFormData,
  ComponentConfig,
} from "@/components/ServicesDeployFlow/types";
import type {
  ServiceDeployOptions,
  ProviderSchema,
} from "@/services/deployment.api";
import { fetchProviderParams } from "@/services/deployment.api";

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
      cachedSchema || (await fetchProviderParams(componentType, providerId));
    const params: Record<string, unknown> = {};

    // Extract all properties with default values from schema
    if (schema?.properties) {
      for (const [key, property] of Object.entries(schema.properties)) {
        if (property.default !== undefined) {
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

function mergeParamsWithUserValues(
  schemaParams: Record<string, unknown>,
  userValues: Record<string, unknown>,
): Record<string, unknown> {
  const merged = { ...schemaParams };

  // Override with user-provided values (non-empty strings, non-null values)
  for (const [key, value] of Object.entries(userValues)) {
    if (value !== undefined && value !== null && value !== "") {
      merged[key] = value;
    }
  }

  return merged;
}

/**
 * Builds a deployment component from ComponentConfig
 */
async function buildDeploymentComponent(
  componentType: string,
  componentConfig: ComponentConfig,
  deployOptions: ServiceDeployOptions,
  schemaPromise: Promise<Record<string, unknown>>,
): Promise<DeploymentComponent> {
  const schemaParams = await schemaPromise;

  // Merge schema defaults with user-provided params
  const params = mergeParamsWithUserValues(
    schemaParams,
    componentConfig.params,
  );

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
  _serviceId?: string | null,
): Promise<DeploymentPayload> {
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

  // Process each enabled service
  // Service IDs are now used directly as keys (e.g., "digitize", "summarize")
  for (const [serviceId, serviceConfig] of Object.entries(formData.services)) {
    if (!serviceConfig.enabled) continue;

    const componentPromises: Promise<DeploymentComponent>[] = [];

    // Process service-specific components dynamically
    for (const [componentType, componentConfig] of Object.entries(
      serviceConfig.components,
    )) {
      componentPromises.push(
        buildDeploymentComponent(
          componentType,
          componentConfig,
          deployOptions,
          getSchemaPromise(
            componentType,
            componentConfig.providerId,
            serviceId,
          ),
        ),
      );
    }

    // Wait for all components of this service to be ready
    const components = await Promise.all(componentPromises);

    services.push({
      catalog_id: serviceId, // Use service ID directly as catalog_id
      version: serviceConfig.version,
      components,
    });
  }

  return {
    name: formData.name,
    catalog_id: deployOptions.id,
    version: formData.version,
    services,
  };
}

// Made with Bob
