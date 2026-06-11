import { api } from "@/api/axios";
import { DIGITAL_ASSISTANTS_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type {
  ArchitectureSummary,
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

// Architectures API - Fetch available architectures
export async function fetchArchitectures(): Promise<ArchitectureSummary[]> {
  const response = await api.get<ArchitectureSummary[]>(
    DIGITAL_ASSISTANTS_ENDPOINTS.LIST_ARCHITECTURES,
  );
  return response.data;
}

// Deploy Options API - Fetch deployment configuration options
export async function fetchDeployOptions(
  architectureId: string,
): Promise<DeployOptionsResponse> {
  const response = await api.get<DeployOptionsResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.DEPLOY_OPTIONS(architectureId),
  );
  return response.data;
}

// Fetch provider parameters schema
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

// Resources API - Fetch system resource availability
export async function fetchResources(): Promise<ResourcesResponse> {
  const response = await api.get<ResourcesResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.RESOURCES,
  );
  return response.data;
}

// Applications API - Manage deployed digital assistants
export async function fetchApplications(
  params: FetchApplicationsParams = {},
): Promise<ApplicationListResponse> {
  const response = await api.get<ApplicationListResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.APPLICATIONS,
    {
      params: {
        deployment_type: "architectures",
        ...params,
      },
    },
  );
  return response.data;
}

export async function fetchApplicationById(id: string): Promise<Application> {
  const response = await api.get<Application>(
    DIGITAL_ASSISTANTS_ENDPOINTS.APPLICATION_BY_ID(id),
  );
  return response.data;
}

export async function deployApplication(
  payload: DeploymentPayload,
): Promise<DeployApplicationResponse> {
  const response = await api.post<DeployApplicationResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.APPLICATIONS,
    payload,
  );
  return response.data;
}

export async function deleteApplication(
  id: string,
  force: boolean = false,
): Promise<DeleteApplicationResponse> {
  const response = await api.delete<DeleteApplicationResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.APPLICATION_BY_ID(id),
    {
      params: { force },
    },
  );
  return response.data;
}

// Utility Functions - Data transformation
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

  // Format based on duration
  if (totalDays > 0) {
    // Show days + hours (e.g., "3d 4hr")
    return hours > 0 ? `${totalDays}d ${hours}hr` : `${totalDays}d`;
  } else if (totalHours > 0) {
    // Show hours + minutes (e.g., "2hr 10min")
    return minutes > 0 ? `${totalHours}hr ${minutes}min` : `${totalHours}hr`;
  } else if (totalMinutes > 0) {
    // Show minutes only (e.g., "5min")
    return `${totalMinutes}min`;
  } else {
    // Show seconds for very recent deployments (e.g., "45sec")
    return totalSeconds > 0 ? `${totalSeconds}sec` : "Just now";
  }
}

export function transformApplicationToRow(
  app: Application,
): DigitalAssistantRow {
  return {
    id: app.id,
    name: app.name,
    status: app.status as DigitalAssistantRow["status"],
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
