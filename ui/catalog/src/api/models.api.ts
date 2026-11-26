import { api } from "@/api/axios";
import { MODEL_MANAGEMENT_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type {
  CatalogModel,
  CatalogModelDetails,
  GenerateModelKeyResponse,
  ModelDeployment,
  ModelDeploymentDetail,
  ModelDeploymentsListResponse,
} from "@/types/api.types";
import type { ModelDeploymentRow } from "@/components/ModelsTable/types";
import { formatDateTime } from "@/utils/time";

export async function fetchCatalogModels(): Promise<CatalogModel[]> {
  const response = await api.get<CatalogModel[]>(
    MODEL_MANAGEMENT_ENDPOINTS.GET_CATALOG_MODELS,
  );
  return response.data;
}

// Fetches a single catalog model by its model identifier (e.g. ibm-granite/granite-3.3-8b-instruct)
export async function fetchCatalogModelDetails(
  id: string,
): Promise<CatalogModelDetails> {
  const response = await api.get<CatalogModelDetails>(
    MODEL_MANAGEMENT_ENDPOINTS.GET_CATALOG_MODEL_DETAILS(id),
  );
  return response.data;
}

// ─── Transform ────────────────────────────────────────────────────────────────

export function transformModelDeploymentToRow(
  model: ModelDeployment,
): ModelDeploymentRow {
  return {
    id: model.id,
    name: model.name,
    status: model.status,
    createdAt: formatDateTime(model.created_at),
    model: model.model,
    componentType: model.component_type.short_name || model.component_type.id,
    backend: model.provider.name,
    workerResource: model.worker?.name ?? "—",
    servicesCount: model.connected_services,
    messages: model.message ?? "",
    actions: "actions",
  };
}

// ─── Data access ──────────────────────────────────────────────────────────────

// Fetches a single page of model deployments
export async function fetchModelsPage(
  page = 1,
  pageSize = 20,
): Promise<ModelDeploymentsListResponse> {
  const response = await api.get<ModelDeploymentsListResponse>(
    MODEL_MANAGEMENT_ENDPOINTS.LIST_MODELS,
    { params: { page, page_size: pageSize } },
  );
  return response.data;
}

// Fetches all model deployments across all pages — used for CSV export
export async function fetchAllModels(): Promise<ModelDeployment[]> {
  let currentPage = 1;
  let hasNext = true;
  const allData: ModelDeployment[] = [];

  while (hasNext) {
    const { data, pagination } = await fetchModelsPage(currentPage, 100);
    allData.push(...data);
    hasNext = pagination?.has_next ?? false;
    currentPage++;
  }

  return allData;
}

// Fetches the detail of a single model deployment by ID
export async function fetchModelDeployment(
  id: string,
): Promise<ModelDeploymentDetail> {
  const response = await api.get<ModelDeploymentDetail>(
    MODEL_MANAGEMENT_ENDPOINTS.GET_MODEL(id),
  );
  return response.data;
}

// Generates a new public key for a model; any existing key is revoked and the new key is returned only once
export async function generateModelKey(
  id: string,
  options?: { signal?: AbortSignal },
): Promise<GenerateModelKeyResponse> {
  const response = await api.post<GenerateModelKeyResponse>(
    MODEL_MANAGEMENT_ENDPOINTS.GENERATE_MODEL_KEY(id),
    undefined,
    { signal: options?.signal },
  );
  return response.data;
}
