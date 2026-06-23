import { api } from "@/api/axios";
import {
  DIGITAL_ASSISTANTS_ENDPOINTS,
  APPLICATION_ENDPOINTS,
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
} from "@/types/digitalAssistants";
import type { DeploymentPayload } from "@/utils/deploymentTransform";
import type { DigitalAssistantRow } from "@/pages/DigitalAssistants/types";

// Fetches the list of available digital assistant architectures
export async function fetchArchitectures(): Promise<ArchitectureSummary[]> {
  const response = await api.get<ArchitectureSummary[]>(
    DIGITAL_ASSISTANTS_ENDPOINTS.LIST_ARCHITECTURES,
  );
  return response.data;
}

// Fetches the list of available services for digital assistants
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

// Fetches deployment options available for a specific architecture
export async function fetchDeployOptions(
  architectureId: string,
): Promise<DeployOptionsResponse> {
  const response = await api.get<DeployOptionsResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.DEPLOY_OPTIONS(architectureId),
  );
  return response.data;
}

// Fetches configuration parameters for a specific provider component
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
    DIGITAL_ASSISTANTS_ENDPOINTS.PROVIDER_PARAMS(componentType, providerId),
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

// Fetches available resources for digital assistant deployments
export async function fetchResources(): Promise<ResourcesResponse> {
  const response = await api.get<ResourcesResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.RESOURCES,
  );
  return response.data;
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
    return hours > 0 ? `${totalDays}d ${hours}hr` : `${totalDays}d`;
  } else if (totalHours > 0) {
    return minutes > 0 ? `${totalHours}hr ${minutes}min` : `${totalHours}hr`;
  } else if (totalMinutes > 0) {
    return `${totalMinutes}min`;
  } else {
    return totalSeconds > 0 ? `${totalSeconds}sec` : "Just now";
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
