import { api } from "@/api/axios";
import {
  DIGITAL_ASSISTANTS_ENDPOINTS,
  APPLICATION_ENDPOINTS,
  SERVICE_ENDPOINTS,
} from "@/constants/api-endpoints.constants";
import { COMPONENT_TYPES } from "@/constants";
import { WORKER_RUNTIME_LABELS } from "@/constants/app.constants";
import type {
  ArchitectureSummary,
  ServiceSummary,
  ArchitectureDetailsResponse,
  DeployOptionsResponse,
  ApplicationListResponse,
  Application,
  FetchApplicationsParams,
  PaginationMetadata,
  DeleteApplicationResponse,
  DeployApplicationResponse,
  ResourcesResponse,
  Service,
  ServiceDeployOptions,
  DeployOptions,
  ProviderSchema,
  LLMOption,
  DeploymentPayload,
  ApplicationDatasourceApiItem,
  ApplicationDatasourcesListResponse,
  ConnectDatasourceError,
  ConnectDatasourcesResponse,
} from "@/types/api.types";
import type { DigitalAssistantRow } from "@/pages/DigitalAssistants/types";
import type { DeployedServicesRow } from "@/components/DeployedServicesTable/types";
import type { ApplicationDatasourceRow } from "@/components/DeploymentDetails/components/ApplicationDatasourcesTable/types";

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
  runtime?: string,
): Promise<DeployOptionsResponse> {
  const response = await api.get<DeployOptionsResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.DEPLOY_OPTIONS(architectureId),
    { params: runtime ? { runtime } : undefined },
  );
  return response.data;
}

// Fetches deploy options for a specific service
export async function fetchServiceDeployOptions(
  serviceId: string,
  runtime?: string,
): Promise<ServiceDeployOptions> {
  const response = await api.get<ServiceDeployOptions>(
    SERVICE_ENDPOINTS.GET_SERVICE_DEPLOY_OPTIONS(serviceId),
    { params: runtime ? { runtime } : undefined },
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
export async function fetchServiceParams(
  serviceId: string,
  runtime?: string,
): Promise<ProviderSchema> {
  const response = await api.get(
    DIGITAL_ASSISTANTS_ENDPOINTS.SERVICE_PARAMS(serviceId),
    { params: runtime ? { runtime } : undefined },
  );
  return response.data;
}

// Fetches provider schema parameters for a component provider
export async function fetchProviderSchema(
  componentType: string,
  providerId: string,
  runtime?: string,
): Promise<ProviderSchema> {
  const response = await api.get<ProviderSchema>(
    SERVICE_ENDPOINTS.GET_COMPONENT_PROVIDER_PARAMS(componentType, providerId),
    { params: runtime ? { runtime } : undefined },
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
  runtime?: string,
): Promise<LLMOption[]> {
  const options =
    deployOptions || (await fetchServiceDeployOptions(serviceId, runtime));

  const llmComponent = options.components.find(
    (component) => component.type === COMPONENT_TYPES.LLM,
  );

  if (!llmComponent || !llmComponent.providers) {
    return [];
  }

  const llmOptionsPromises = llmComponent.providers.map(async (provider) => {
    if (!provider.schema) {
      return [
        {
          id: provider.id,
          text: provider.name,
          providerId: provider.id,
          providerName: provider.name,
        },
      ];
    }

    const schema = await fetchProviderSchema(
      COMPONENT_TYPES.LLM,
      provider.id,
      runtime,
    );

    if (setProviderSchema) {
      setProviderSchema(serviceId, COMPONENT_TYPES.LLM, provider.id, schema);
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

    return [
      {
        id: provider.id,
        text: provider.name,
        providerId: provider.id,
        providerName: provider.name,
      },
    ];
  });

  const allOptionsArrays = await Promise.all(llmOptionsPromises);
  return allOptionsArrays.flat();
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
  runtime?: string,
): Promise<LLMOption[]> {
  const options =
    deployOptions || (await fetchServiceDeployOptions(serviceId, runtime));

  const component = options.components.find((c) => c.type === componentType);

  if (!component || !component.providers) {
    return [];
  }

  const modelOptionsPromises = component.providers.map(async (provider) => {
    if (!provider.schema) {
      return [];
    }

    const schema = await fetchProviderSchema(
      componentType,
      provider.id,
      runtime,
    );

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

    return [];
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

// Fetches available resources for deployments.
// Pass workerName to query a specific remote worker; omit for the local runtime.
export async function fetchResources(
  workerName?: string,
): Promise<ResourcesResponse> {
  const response = await api.get<ResourcesResponse>(
    DIGITAL_ASSISTANTS_ENDPOINTS.RESOURCES,
    { params: workerName ? { worker: workerName } : undefined },
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
    workerResource: app.worker?.name ?? "",
    workerType:
      WORKER_RUNTIME_LABELS[app.worker?.runtime_type ?? ""]?.short ??
      app.worker?.runtime_type ??
      "",
    messages: app.status === "Running" ? "" : app.message || "",
    actions: "actions",
    children: app.services.map((service) => ({
      id: service.id,
      name: `${service.type} (service)`,
      status: service.status as DigitalAssistantRow["status"],
      uptime: "",
      workerResource: "",
      workerType: "",
      messages: "",
      actions: "actions",
    })),
  };
}

// Transforms an Application object into a DeployedServicesRow for the services table
export function transformDeployedServiceToRow(
  app: Application,
): DeployedServicesRow {
  return {
    id: app.id,
    name: app.name,
    status: app.status as DeployedServicesRow["status"],
    type: app.type,
    uptime: calculateUptime(app.created_at),
    workerResource: app.worker?.name ?? "",
    workerType:
      WORKER_RUNTIME_LABELS[app.worker?.runtime_type ?? ""]?.short ??
      app.worker?.runtime_type ??
      "",
    service: app.type || "",
    messages: app.message || "",
    actions: "actions",
  };
}

// Fetches a single page of deployed services (deployment_type=services)
export async function fetchDeployedServicesPage(params: {
  page: number;
  pageSize: number;
  catalogId?: string;
}): Promise<{ data: Application[]; pagination: PaginationMetadata }> {
  const response = await api.get<ApplicationListResponse>(
    APPLICATION_ENDPOINTS.GET_APPLICATIONS,
    {
      params: {
        deployment_type: "services",
        page: params.page,
        page_size: params.pageSize,
        ...(params.catalogId ? { catalog_id: params.catalogId } : {}),
      },
    },
  );
  return {
    data: response.data.data,
    pagination: response.data.pagination,
  };
}

// Fetches all deployed services across all pages — used for CSV export
export async function fetchAllDeployedServices(
  catalogId?: string,
): Promise<Application[]> {
  let currentPage = 1;
  let hasNext = true;
  const allData: Application[] = [];

  while (hasNext) {
    const response = await api.get<ApplicationListResponse>(
      APPLICATION_ENDPOINTS.GET_APPLICATIONS,
      {
        params: {
          deployment_type: "services",
          page: currentPage,
          page_size: 100,
          ...(catalogId ? { catalog_id: catalogId } : {}),
        },
      },
    );
    allData.push(...response.data.data);
    hasNext = response.data.pagination?.has_next ?? false;
    currentPage++;
  }

  return allData;
}

// Transforms an ApplicationDatasourceApiItem into an ApplicationDatasourceRow for display
export function transformDatasourceToRow(
  item: ApplicationDatasourceApiItem,
): ApplicationDatasourceRow {
  const raw = item.last_sync ?? "";
  return {
    id: item.id,
    name: item.name,
    source_type: item.provider.name,
    status: item.status,
    files:
      item.files === null || item.files === undefined || item.files === 0
        ? "-"
        : String(item.files),
    // Show "In progress" whenever a sync is active, regardless of whether a
    // previous sync timestamp exists. Only fall back to calculateUptime once
    // the sync has completed (status is no longer "syncing").
    last_sync:
      item.status === "syncing"
        ? "In progress"
        : raw
          ? calculateUptime(raw)
          : "—",
    messages: item.message ?? "",
    actions: "",
  };
}

// Fetches a single page of datasources for a given application
export async function fetchApplicationDatasources(
  applicationId: string,
  page: number,
  pageSize: number,
): Promise<{
  rows: ApplicationDatasourceRow[];
  pagination: PaginationMetadata;
}> {
  const response = await api.get<ApplicationDatasourcesListResponse>(
    APPLICATION_ENDPOINTS.APPLICATION_DATASOURCES(applicationId),
    { params: { page, page_size: pageSize } },
  );
  return {
    rows: response.data.data.map(transformDatasourceToRow),
    pagination: response.data.pagination,
  };
}

// Fetches all datasources for a given application — used for CSV export
export async function fetchAllApplicationDatasources(
  applicationId: string,
): Promise<ApplicationDatasourceRow[]> {
  let currentPage = 1;
  let hasNext = true;
  const allData: ApplicationDatasourceApiItem[] = [];

  while (hasNext) {
    const response = await api.get<ApplicationDatasourcesListResponse>(
      APPLICATION_ENDPOINTS.APPLICATION_DATASOURCES(applicationId),
      { params: { page: currentPage, page_size: 100 } },
    );
    allData.push(...response.data.data);
    hasNext = response.data.pagination?.has_next ?? false;
    currentPage++;
  }

  return allData.map(transformDatasourceToRow);
}

// Connects one or more data source connectors to an application
/**
 * Connects one or more datasources to an application.
 *
 * Returns an array of per-datasource errors for any connections that failed
 * (HTTP 207 Multi-Status). An empty array means every datasource connected
 * successfully (HTTP 204 No Content).
 *
 * Throws for network errors or unexpected non-2xx responses.
 */
export async function connectApplicationDatasources(
  applicationId: string,
  datasourceIds: string[],
): Promise<ConnectDatasourceError[]> {
  const response = await api.put<ConnectDatasourcesResponse | null>(
    APPLICATION_ENDPOINTS.APPLICATION_DATASOURCES(applicationId),
    { datasource_ids: datasourceIds },
    // Accept 207 without throwing so we can inspect the body ourselves.
    { validateStatus: (status) => status === 204 || status === 207 },
  );

  // 204 No Content — all succeeded
  if (response.status === 204 || !response.data) return [];

  // 207 Multi-Status — at least one datasource failed
  return response.data.errors ?? [];
}
// Removes a single datasource from an application
export async function removeApplicationDatasource(
  applicationId: string,
  datasourceId: string,
): Promise<void> {
  await api.delete(
    APPLICATION_ENDPOINTS.REMOVE_APPLICATION_DATASOURCE(
      applicationId,
      datasourceId,
    ),
  );
}
