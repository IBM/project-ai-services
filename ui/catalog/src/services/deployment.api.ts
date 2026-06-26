import { api } from "@/api/axios";
import { SERVICE_ENDPOINTS } from "@/constants/api-endpoints.constants";

export interface ResourcesResponse {
  cpu: {
    total_cpu: number;
    available_cpu: number;
  };
  memory: {
    total_bytes: number;
    available_bytes: number;
  };
  accelerators: {
    [key: string]: {
      total: number;
      available: number;
    };
  };
}

/**
 * Deploy Options Types - Used for fetching deployment configuration
 */
export interface Provider {
  id: string;
  name: string;
  description: string;
  version: string;
  default?: boolean;
  schema?: string;
  resources?: {
    cpu: number;
    memory: number;
    storage?: number;
    accelerators?: Record<string, number>;
  };
}

export interface DeployOptionsComponent {
  type: string;
  name: string;
  providers: Provider[];
}

export interface DeployOptionsService {
  id: string;
  name: string;
  version: string;
  schema?: string;
  components: DeployOptionsComponent[];
}

export interface DeployOptionsResponse {
  id: string;
  name: string;
  version: string;
  components: DeployOptionsComponent[];
  services: DeployOptionsService[];
}

/**
 * Application Types - Used for managing deployed digital assistants
 */
export interface ServiceComponent {
  type: string;
  provider: string;
  metadata: Record<string, unknown>;
}

export interface ApplicationService {
  id: string;
  type: string;
  version: string;
  status: string;
  created_at: string;
  updated_at: string;
  components: ServiceComponent[];
  endpoints: object[];
}

export interface Application {
  id: string;
  name: string;
  type: string;
  deployment_type: string;
  status: string;
  message: string;
  created_at: string;
  updated_at: string;
  services: ApplicationService[];
}

export interface PaginationMetadata {
  page: number;
  page_size: number;
  total_items: number;
  total_pages: number;
  has_next: boolean;
  has_prev: boolean;
}

export interface ApplicationListResponse {
  data: Application[];
  pagination: PaginationMetadata;
}

/**
 * API Request/Response Types
 */
export interface FetchApplicationsParams {
  page?: number;
  page_size?: number;
  deployment_type?: "architectures" | "services";
  catalog_id?: string;
}

export interface DeleteApplicationResponse {
  id: string;
  message: string;
  status: string;
}

export interface DeployApplicationResponse {
  id: string;
}

/**
 * UI-specific types from api.ts
 */
export interface DigitalAssistantRow {
  id: string;
  name: string;
  status: "Deploying..." | "Deleting..." | "Error" | "Stopped" | "Running";
  uptime: string;
  messages: string;
  actions: string;
  children?: DigitalAssistantRow[];
}

export interface Service {
  id: string;
  name: string;
  description: string;
  certified_by?: string;
  architectures?: string[];
  standalone?: boolean;
  version?: string;
}

/**
 * Deploy options component interface
 */
export interface DeployComponent {
  type: string;
  name?: string; // Display name from API (e.g., "Large Language Model (LLM)")
  description?: string; // Description from API
  providers: Array<{
    id: string;
    name: string;
    description?: string;
    default?: boolean;
    schema?: string;
    version?: string;
    resources?: {
      cpu?: number;
      memory?: number;
      storage?: number;
      accelerators?: Record<string, number>;
    };
    [key: string]: unknown;
  }>;
}

/**
 * Deploy options response interface (for digital assistant)
 */
export interface DeployOptions {
  version: string;
  global_components: DeployComponent[];
  services: DeployOptionsService[];
}

/**
 * Service deploy options response interface (for individual services)
 */
export interface ServiceDeployOptions {
  id: string;
  name: string;
  description?: string;
  version: string;
  components: DeployComponent[];
  resources?: {
    cpu: number;
    memory: number;
    storage?: number;
    accelerators?: Record<string, number>;
  };
}

/**
 * Provider schema response interface
 */
export interface ProviderSchemaProperty {
  default?: string;
  description?: string;
  title?: string;
  type?: string;
  format?: string;
  oneOf?: Array<{
    const: string;
    description?: string;
    title?: string;
  }>;
}

export interface ProviderSchema {
  $schema?: string;
  properties: {
    model?: ProviderSchemaProperty;
    [key: string]: ProviderSchemaProperty | undefined;
  };
  required?: string[];
  type: string;
}

/**
 * LLM option with model information
 */
export interface LLMOption {
  id: string;
  text: string;
  providerId: string;
  providerName: string;
}

/**
 * Deployment payload interface
 */
export interface DeploymentPayload {
  name: string;
  catalog_id: string;
  version: string;
  deployment_type: "service";
  services: Array<{
    catalog_id: string;
    version: string;
    components: Array<{
      component_type: string;
      provider_id: string;
      version: string;
      params?: Record<string, unknown>;
    }>;
  }>;
  global_components?: {
    [key: string]: string;
  };
}

/**
 * Fetch all available services
 * GET /services
 */
export const fetchServices = async (): Promise<Service[]> => {
  const response = await api.get<Service[]>("/services");
  return response.data;
};

/**
 * Fetch details about a specific service
 * GET /services/{id}
 */
export const fetchServiceDetails = async (
  serviceId: string,
): Promise<Service> => {
  const response = await api.get<Service>(`/services/${serviceId}`);
  return response.data;
};

/**
 * Fetch deploy options for a specific service
 * GET /services/{id}/deploy-options
 */
export const fetchServiceDeployOptions = async (
  serviceId: string,
): Promise<ServiceDeployOptions> => {
  const response = await api.get<ServiceDeployOptions>(
    `/services/${serviceId}/deploy-options`,
  );
  return response.data;
};

// Resources API - Fetch system resource availability
export async function fetchResources(): Promise<ResourcesResponse> {
  const response = await api.get<ResourcesResponse>("/resources");
  return response.data;
}

/**
 * Fetch deploy options for digital assistant (all services)
 * This endpoint should be defined based on your backend API
 * For now, using a placeholder endpoint
 */
export const fetchDigitalAssistantDeployOptions =
  async (): Promise<DeployOptions> => {
    // TODO: Replace with actual endpoint for digital assistant deploy options
    // This might be something like /applications/digital-assistant/deploy-options
    const response = await api.get<DeployOptions>("/deploy-options");
    return response.data;
  };

/**
 * Deploy an application (digital assistant or service)
 * POST /applications
 */
export const deployApplication = async (
  payload: DeploymentPayload,
): Promise<void> => {
  await api.post("/applications", payload);
};

/**
 * Fetch provider schema parameters
 * GET /components/{componentType}/providers/{providerId}/params
 */
export const fetchProviderSchema = async (
  componentType: string,
  providerId: string,
): Promise<ProviderSchema> => {
  const response = await api.get<ProviderSchema>(
    `/components/${componentType}/providers/${providerId}/params`,
  );
  return response.data;
};

/**
 * Fetch LLM options with model information from provider schemas
 * This function fetches all provider schemas for LLM components and extracts model information
 * Also caches the schemas in the store for later use
 *
 * @param serviceId - The service ID
 * @param setProviderSchema - Function to cache provider schemas in the store
 * @param deployOptions - Pre-fetched deploy options (to avoid redundant API call)
 */
export const fetchLLMOptionsWithModels = async (
  serviceId: string,
  setProviderSchema?: (
    serviceId: string,
    componentType: string,
    providerId: string,
    schema: ProviderSchema,
  ) => void,
  deployOptions?: ServiceDeployOptions,
): Promise<LLMOption[]> => {
  try {
    // Use provided deploy options if available, otherwise fetch them
    const options =
      deployOptions || (await fetchServiceDeployOptions(serviceId));

    // Find the LLM component
    const llmComponent = options.components.find(
      (component) => component.type === "llm",
    );

    if (!llmComponent || !llmComponent.providers) {
      return [];
    }

    // Fetch schema for each provider and extract model information
    const llmOptionsPromises = llmComponent.providers.map(async (provider) => {
      try {
        // If provider has a schema URL, fetch it
        if (provider.schema) {
          const schema = await fetchProviderSchema("llm", provider.id);

          // Cache the schema in the store if setter is provided
          if (setProviderSchema) {
            setProviderSchema(serviceId, "llm", provider.id, schema);
          }

          // Extract ALL models from schema (check oneOf first, then default)
          if (schema.properties.model?.oneOf) {
            // Return all model options from oneOf
            return schema.properties.model.oneOf.map((option) => ({
              id: option.const,
              text: option.title || option.const,
              providerId: provider.id,
              providerName: provider.name,
            }));
          }

          // Fallback: Extract model default value from schema
          const modelDefault = schema.properties.model?.default;
          if (modelDefault) {
            return [
              {
                id: modelDefault,
                text: modelDefault,
                providerId: provider.id,
                providerName: provider.name,
              },
            ];
          }
        }

        // Fallback: if no schema or no model in schema, return provider info
        return [
          {
            id: provider.id,
            text: provider.name,
            providerId: provider.id,
            providerName: provider.name,
          },
        ];
      } catch (error) {
        console.error(
          `Failed to fetch schema for provider ${provider.id}:`,
          error,
        );
        // Return provider info as fallback
        return [
          {
            id: provider.id,
            text: provider.name,
            providerId: provider.id,
            providerName: provider.name,
          },
        ];
      }
    });

    const allOptionsArrays = await Promise.all(llmOptionsPromises);
    // Flatten the array of arrays
    const allOptions = allOptionsArrays.flat();

    // Keep ALL model-provider mappings - important for filtering inference backends by model
    // Return ALL options (not deduplicated) so we have all model-provider mappings
    // The UI will deduplicate for display, but filtering needs all mappings
    return allOptions;
  } catch (error) {
    console.error("Failed to fetch LLM options with models:", error);
    return [];
  }
};

/**
 * Fetch component models with schemas for any component type
 * This function fetches all provider schemas for a component and extracts model information
 * Also caches the schemas in the store for later use
 *
 * @param serviceId - The service ID
 * @param componentType - The component type (e.g., "embedding", "reranker")
 * @param setProviderSchema - Function to cache provider schemas in the store
 * @param deployOptions - Pre-fetched deploy options (to avoid redundant API call)
 */
export const fetchComponentModelsWithSchemas = async (
  serviceId: string,
  componentType: string,
  setProviderSchema?: (
    serviceId: string,
    componentType: string,
    providerId: string,
    schema: ProviderSchema,
  ) => void,
  deployOptions?: ServiceDeployOptions,
): Promise<LLMOption[]> => {
  try {
    // Use provided deploy options if available, otherwise fetch them
    const options =
      deployOptions || (await fetchServiceDeployOptions(serviceId));

    // Find the component
    const component = options.components.find((c) => c.type === componentType);

    if (!component || !component.providers) {
      return [];
    }

    // Fetch schema for each provider and extract model information
    const modelOptionsPromises = component.providers.map(async (provider) => {
      try {
        // If provider has a schema URL, fetch it
        if (provider.schema) {
          const schema = await fetchProviderSchema(componentType, provider.id);

          // Cache the schema in the store if setter is provided
          if (setProviderSchema) {
            setProviderSchema(serviceId, componentType, provider.id, schema);
          }

          // Extract model options from schema
          // Check if schema has a model property with oneOf options
          if (schema.properties.model?.oneOf) {
            // Return all model options from oneOf
            return schema.properties.model.oneOf.map((option) => ({
              id: option.const,
              text: option.title || option.const,
              providerId: provider.id,
              providerName: provider.name,
            }));
          }

          // Fallback: Extract model default value from schema
          const modelDefault = schema.properties.model?.default;
          if (modelDefault) {
            return [
              {
                id: modelDefault,
                text: modelDefault,
                providerId: provider.id,
                providerName: provider.name,
              },
            ];
          }
        }

        // Fallback: if no schema or no model in schema
        // Return empty array - components without schemas don't have model parameters
        // The provider selection will be handled separately in the UI
        return [];
      } catch (error) {
        console.error(
          `Failed to fetch schema for ${componentType} provider ${provider.id}:`,
          error,
        );
        // Return empty array as fallback - components without schemas don't have model parameters
        return [];
      }
    });

    const allOptionsArrays = await Promise.all(modelOptionsPromises);
    // Flatten the array of arrays
    const allOptions = allOptionsArrays.flat();

    // Deduplicate by model ID - keep only unique models
    const uniqueOptions = allOptions.reduce((acc, option) => {
      // Check if this model ID already exists
      const exists = acc.some((existing) => existing.id === option.id);
      if (!exists) {
        acc.push(option);
      }
      return acc;
    }, [] as LLMOption[]);

    return uniqueOptions;
  } catch (error) {
    console.error(
      `Failed to fetch ${componentType} options with models:`,
      error,
    );
    return [];
  }
};

// ============================================================================
// UTILITY FUNCTIONS FROM api.ts
// ============================================================================

/**
 * Calculate uptime from created timestamp
 */
export function calculateUptime(createdAt: string): string {
  const created = new Date(createdAt);
  const now = new Date();
  const diffMs = now.getTime() - created.getTime();

  // Calculate time components
  const totalSeconds = Math.floor(diffMs / 1000);
  const totalMinutes = Math.floor(totalSeconds / 60);
  const totalHours = Math.floor(totalMinutes / 60);
  const totalDays = Math.floor(totalHours / 24);

  // Extract remaining components
  const minutes = totalMinutes % 60;
  const hours = totalHours % 24;

  // Format based on duration - show only most significant unit, rounded up
  if (totalDays > 0) {
    // Round up if there are any hours
    const days = hours > 0 ? totalDays + 1 : totalDays;
    return days === 1 ? "1 day" : `${days} days`;
  } else if (totalHours > 0) {
    // Round up if there are any minutes
    const hrs = minutes > 0 ? totalHours + 1 : totalHours;
    return hrs === 1 ? "1 hour" : `${hrs} hours`;
  } else if (totalMinutes > 0) {
    // Round up if there are any seconds
    const mins = totalSeconds % 60 > 0 ? totalMinutes + 1 : totalMinutes;
    return mins === 1 ? "1 minute" : `${mins} minutes`;
  } else {
    // Show seconds for very recent deployments
    return totalSeconds === 1
      ? "1 second"
      : totalSeconds > 0
        ? `${totalSeconds} seconds`
        : "Just now";
  }
}

/**
 * Fetch provider parameters schema
 */
export async function fetchProviderParams(
  componentType: string,
  providerId: string,
): Promise<{
  properties?: Record<
    string,
    {
      type?: string;
      default?: unknown;
      title?: string;
      description?: string;
      format?: string;
      oneOf?: Array<{ const: string; title: string; description?: string }>;
      [key: string]: unknown;
    }
  >;
}> {
  const response = await api.get(
    SERVICE_ENDPOINTS.GET_COMPONENT_PROVIDER_PARAMS(componentType, providerId),
  );
  return response.data;
}

/**
 * Transform Application to DigitalAssistantRow for table display
 */
export function transformApplicationToRow(
  app: Application,
): DigitalAssistantRow {
  return {
    id: app.id,
    name: app.name,
    status: app.status as DigitalAssistantRow["status"],
    uptime: calculateUptime(app.created_at),
    messages: app.message || "",
    actions: "actions",
    children: app.services.map((service) => ({
      id: service.id,
      name: `${service.type} (service)`,
      status: service.status as DigitalAssistantRow["status"],
      uptime: "",
      messages: "",
      actions: "actions",
    })),
  };
}

// Made with Bob
