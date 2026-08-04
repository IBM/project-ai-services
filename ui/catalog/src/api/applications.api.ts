import { api } from "@/api/axios";
import {
  DIGITAL_ASSISTANTS_ENDPOINTS,
  APPLICATION_ENDPOINTS,
  SERVICE_ENDPOINTS,
} from "@/constants/api-endpoints.constants";
import type {
  ArchitectureSummary,
  ServiceSummary,
  ArchitectureDetailsResponse,
  DeployOptionsResponse,
  ApplicationListResponse,
  Application,
  FetchApplicationsParams,
  DeleteApplicationResponse,
  DeployApplicationResponse,
  ResourcesResponse,
  Service,
  ServiceDeployOptions,
  DeployOptions,
  ProviderSchema,
  LLMOption,
  DeploymentPayload,
} from "@/types/api.types";
import type { DigitalAssistantRow } from "@/pages/DigitalAssistants/types";

// Fetches the list of available digital assistant architectures
export async function fetchArchitectures(): Promise<ArchitectureSummary[]> {
  const response = await api.get<ArchitectureSummary[]>(
    DIGITAL_ASSISTANTS_ENDPOINTS.LIST_ARCHITECTURES,
  );
  return response.data;
}

// Fetches the list of available services
export async function fetchServices(): Promise<ServiceSummary[]> {
  const response = await api.get<ServiceSummary[]>(
    DIGITAL_ASSISTANTS_ENDPOINTS.LIST_SERVICES,
  );
  return response.data;
}

// Fetches detailed information for a specific architecture by ID
export async function fetchArchitectureDetails(
  architectureId: string,
): Promise<ArchitectureDetailsResponse> {
  const response = await api.get<ArchitectureDetailsResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.ARCHITECTURE_DETAILS(architectureId),
  );
  return response.data;
}

// Fetches details about a specific service
export async function fetchServiceDetails(serviceId: string): Promise<Service> {
  const response = await api.get<Service>(
    SERVICE_ENDPOINTS.GET_SERVICE_DETAILS(serviceId),
  );
  return response.data;
}

// Fetches deployment options for a specific architecture
export async function fetchDeployOptions(
  architectureId: string,
): Promise<DeployOptionsResponse> {
  const response = await api.get<DeployOptionsResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.DEPLOY_OPTIONS(architectureId),
  );
  return response.data;
}

// Fetches deploy options for a specific service
export async function fetchServiceDeployOptions(
  serviceId: string,
): Promise<ServiceDeployOptions> {
  const response = await api.get<ServiceDeployOptions>(
    SERVICE_ENDPOINTS.GET_SERVICE_DEPLOY_OPTIONS(serviceId),
  );
  return response.data;
}

// Fetches deploy options for digital assistant (all services)
export async function fetchDigitalAssistantDeployOptions(): Promise<DeployOptions> {
  const response = await api.get<DeployOptions>(
    DIGITAL_ASSISTANTS_ENDPOINTS.DIGITAL_ASSISTANT_DEPLOY_OPTIONS,
  );
  return response.data;
}

// Fetches configuration parameters schema for a specific service
export async function fetchServiceParams(serviceId: string): Promise<{
  properties?: Record<
    string,
    {
      type?: string;
      default?: unknown;
      title?: string;
      description?: string;
      format?: string;
      minLength?: number;
      maxLength?: number;
      "x-ui-only"?: boolean;
      "x-ui-controls"?: string;
      "x-ui-controlled-by"?: string;
      [key: string]: unknown;
    }
  >;
  required?: string[];
}> {
  const response = await api.get(
    DIGITAL_ASSISTANTS_ENDPOINTS.SERVICE_PARAMS(serviceId),
  );
  return response.data;
}

// Fetches provider schema parameters (services flow)
export async function fetchProviderSchema(
  componentType: string,
  providerId: string,
): Promise<ProviderSchema> {
  const response = await api.get<ProviderSchema>(
    SERVICE_ENDPOINTS.GET_COMPONENT_PROVIDER_PARAMS(componentType, providerId),
  );
  return response.data;
}

// Fetches LLM options with model information from provider schemas
export async function fetchLLMOptionsWithModels(
  serviceId: string,
  setProviderSchema?: (
    serviceId: string,
    componentType: string,
    providerId: string,
    schema: ProviderSchema,
  ) => void,
  deployOptions?: ServiceDeployOptions,
): Promise<LLMOption[]> {
  try {
    const options =
      deployOptions || (await fetchServiceDeployOptions(serviceId));

    const llmComponent = options.components.find(
      (component) => component.type === "llm",
    );

    if (!llmComponent || !llmComponent.providers) {
      return [];
    }

    const llmOptionsPromises = llmComponent.providers.map(async (provider) => {
      try {
        if (provider.schema) {
          const schema = await fetchProviderSchema("llm", provider.id);

          if (setProviderSchema) {
            setProviderSchema(serviceId, "llm", provider.id, schema);
          }

          if (schema.properties.model?.oneOf) {
            return schema.properties.model.oneOf.map((option) => ({
              id: option.const,
              text: option.title || option.const,
              providerId: provider.id,
              providerName: provider.name,
            }));
          }

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
    return allOptionsArrays.flat();
  } catch (error) {
    console.error("Failed to fetch LLM options with models:", error);
    return [];
  }
}

// Fetches component models with schemas for any component type
export async function fetchComponentModelsWithSchemas(
  serviceId: string,
  componentType: string,
  setProviderSchema?: (
    serviceId: string,
    componentType: string,
    providerId: string,
    schema: ProviderSchema,
  ) => void,
  deployOptions?: ServiceDeployOptions,
): Promise<LLMOption[]> {
  try {
    const options =
      deployOptions || (await fetchServiceDeployOptions(serviceId));

    const component = options.components.find((c) => c.type === componentType);

    if (!component || !component.providers) {
      return [];
    }

    const modelOptionsPromises = component.providers.map(async (provider) => {
      try {
        if (provider.schema) {
          const schema = await fetchProviderSchema(componentType, provider.id);

          if (setProviderSchema) {
            setProviderSchema(serviceId, componentType, provider.id, schema);
          }

          if (schema.properties.model?.oneOf) {
            return schema.properties.model.oneOf.map((option) => ({
              id: option.const,
              text: option.title || option.const,
              providerId: provider.id,
              providerName: provider.name,
            }));
          }

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

        return [];
      } catch (error) {
        console.error(
          `Failed to fetch schema for ${componentType} provider ${provider.id}:`,
          error,
        );
        return [];
      }
    });

    const allOptionsArrays = await Promise.all(modelOptionsPromises);
    const allOptions = allOptionsArrays.flat();

    // Deduplicate by model ID
    return allOptions.reduce((acc, option) => {
      if (!acc.some((existing) => existing.id === option.id)) {
        acc.push(option);
      }
      return acc;
    }, [] as LLMOption[]);
  } catch (error) {
    console.error(
      `Failed to fetch ${componentType} options with models:`,
      error,
    );
    return [];
  }
}

// Fetches a list of deployed applications with optional filtering parameters
export async function fetchApplications(
  params: FetchApplicationsParams = {},
): Promise<ApplicationListResponse> {
  const response = await api.get<ApplicationListResponse>(
    APPLICATION_ENDPOINTS.GET_APPLICATIONS,
    {
      params: {
        deployment_type: "architectures",
        ...params,
      },
    },
  );
  return response.data;
}

// Fetches detailed information for a specific application by ID
export async function fetchApplicationById(id: string): Promise<Application> {
  const response = await api.get<Application>(
    APPLICATION_ENDPOINTS.GET_APPLICATION_DETAILS(id),
  );
  return response.data;
}

// Deploys a new application with the provided configuration payload
export async function deployApplication(
  payload: DeploymentPayload,
): Promise<DeployApplicationResponse> {
  const response = await api.post<DeployApplicationResponse>(
    APPLICATION_ENDPOINTS.GET_APPLICATIONS,
    payload,
  );
  return response.data;
}

// Deletes an application by ID
export async function deleteApplication(
  id: string,
): Promise<DeleteApplicationResponse> {
  const response = await api.delete<DeleteApplicationResponse>(
    APPLICATION_ENDPOINTS.DELETE_APPLICATION(id),
  );
  return response.data;
}

// Fetches available resources for deployments
export async function fetchResources(): Promise<ResourcesResponse> {
  const response = await api.get<ResourcesResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.RESOURCES,
  );
  return response.data;
}

// Calculates and formats the uptime duration from a creation timestamp
export function calculateUptime(createdAt: string): string {
  const created = new Date(createdAt);
  const now = new Date();
  const diffMs = now.getTime() - created.getTime();

  const totalSeconds = Math.floor(diffMs / 1000);
  const totalMinutes = Math.floor(totalSeconds / 60);
  const totalHours = Math.floor(totalMinutes / 60);
  const totalDays = Math.floor(totalHours / 24);

  const minutes = totalMinutes % 60;
  const hours = totalHours % 24;

  if (totalDays > 0) {
    const days = hours > 0 ? totalDays + 1 : totalDays;
    return days === 1 ? "1 day" : `${days} days`;
  } else if (totalHours > 0) {
    const hrs = minutes > 0 ? totalHours + 1 : totalHours;
    return hrs === 1 ? "1 hour" : `${hrs} hours`;
  } else if (totalMinutes > 0) {
    const mins = totalSeconds % 60 > 0 ? totalMinutes + 1 : totalMinutes;
    return mins === 1 ? "1 minute" : `${mins} minutes`;
  } else {
    return totalSeconds === 1
      ? "1 second"
      : totalSeconds > 0
        ? `${totalSeconds} seconds`
        : "Just now";
  }
}

// Transforms an Application object into a DigitalAssistantRow format for display
export function transformApplicationToRow(
  app: Application,
): DigitalAssistantRow {
  return {
    id: app.id,
    name: app.name,
    status: app.status as DigitalAssistantRow["status"],
    type: app.type,
    uptime: calculateUptime(app.created_at),
    messages: app.status === "Running" ? "" : app.message || "",
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
