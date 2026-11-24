import { api } from "@/api/axios";
import { MODEL_MANAGEMENT_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type {
  CatalogModel,
  FetchModelsPageParams,
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
export async function fetchCatalogModel(
  modelId: string,
): Promise<CatalogModel> {
  const response = await api.get<CatalogModel>(
    MODEL_MANAGEMENT_ENDPOINTS.GET_CATALOG_MODEL(modelId),
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

// Fetches a single page of model deployments (optionally filtered)
export async function fetchModelsPage(
  params: FetchModelsPageParams,
): Promise<ModelDeploymentsListResponse> {
  const queryParams: Record<string, unknown> = {
    page: params.page,
    page_size: params.pageSize,
    ...(params.providerIds?.length ? { provider_id: params.providerIds } : {}),
    ...(params.models?.length ? { model: params.models } : {}),
  };
  const response = await api.get<ModelDeploymentsListResponse>(
    MODEL_MANAGEMENT_ENDPOINTS.LIST_MODELS,
    {
      params: queryParams,
      // `indexes: null` serialises arrays as repeated params
      paramsSerializer: { indexes: null },
    },
  );
  return response.data;
}

// Fetches all models across all pages — used for CSV export and filter options
export async function fetchAllModels(
  providerIds?: string[],
  models?: string[],
): Promise<ModelDeployment[]> {
  let currentPage = 1;
  let hasNext = true;
  const allData: ModelDeployment[] = [];

  while (hasNext) {
    const { data, pagination } = await fetchModelsPage({
      page: currentPage,
      pageSize: 100,
      providerIds,
      models,
    });
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
