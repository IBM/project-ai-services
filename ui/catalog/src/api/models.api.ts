import { api } from "@/api/axios";
import { MODEL_MANAGEMENT_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type {
  CatalogModel,
  ModelDeployment,
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

// ─── Static data ──────────────────────────────────────────────────────────────
// API integration will be done in a future PR once the backend is ready.

// Same shape as the GET /models list response.
const MOCK_MODELS_RESPONSE: ModelDeploymentsListResponse = {
  data: [
    {
      id: "7f3a1c2d-8e4b-4f5a-9d6e-1a2b3c4d5e6f",
      deployment_type: "local",
      name: "granite-llm",
      component_type: {
        id: "llm",
        name: "Large Language Model",
        short_name: "LLM",
      },
      provider: { id: "vllm-spyre", name: "vLLM (Spyre)" },
      worker: { name: "lpar-1", runtime_type: "podman" },
      model: "ibm-granite/granite-3.3-8b-instruct",
      profile: {
        id: "compact-4k",
        name: "Compact 4k",
        description: "1 card, 100 GiB RAM, 4k context",
        params: {
          resources: {
            memory: 107374182400,
            accelerators: { "ibm.com/spyre_pf": 1 },
          },
          maxModelLen: 4096,
          maxBatchSize: 32,
          shmSize: "2Gi",
        },
      },
      status: "Running",
      connected_services: 2,
      created_at: "2026-07-01T10:00:00Z",
      updated_at: "2026-07-01T10:05:00Z",
    },
    {
      id: "c1d2e3f4-a5b6-7890-cdef-123456789abc",
      deployment_type: "remote",
      name: "prod-watsonx",
      component_type: {
        id: "llm",
        name: "Large Language Model",
        short_name: "LLM",
      },
      provider: { id: "watsonx", name: "WatsonX" },
      worker: null,
      model: "ibm/granite-4-h-small",
      profile: null,
      api_base: "https://us-south.ml.cloud.ibm.com",
      project_id: "my-watsonx-project-id",
      status: "Error",
      message:
        "Failed to execute deployment: failed to extract images: failed to download image: retry failed after 3 attempts with err: stream: worker Local:",
      connected_services: 2,
      created_at: "2026-07-01T11:00:00Z",
      updated_at: "2026-07-01T11:05:00Z",
    },
    {
      id: "c1d2e3f4-a5b6-7890-cdef-345678905678",
      deployment_type: "remote",
      name: "embedding-model",
      component_type: {
        id: "embedding",
        name: "Embedding Model",
        short_name: "Embedding",
      },
      provider: { id: "vllm-cpu", name: "AI Inference on CPU" },
      worker: null,
      model: "granite-embedding-278m-multilingual",
      profile: null,
      api_base: "https://us-south.ml.cloud.ibm.com",
      project_id: "my-embedding-project-id",
      status: "Initializing",
      message: "Downloading model weights...",
      connected_services: 0,
      created_at: "2026-07-01T11:00:00Z",
      updated_at: "2026-07-01T11:05:00Z",
    },
  ],
  pagination: {
    page: 1,
    page_size: 20,
    total_items: 3,
    total_pages: 1,
    has_next: false,
    has_prev: false,
  },
};

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
    componentType: model.component_type.short_name,
    backend: model.provider.name,
    workerResource: model.worker?.name ?? "—",
    servicesCount: model.connected_services,
    messages: model.message ?? "",
    actions: "actions",
  };
}

// ─── Data access ──────────────────────────────────────────────────────────────

export async function fetchModelsPage(): Promise<ModelDeploymentsListResponse> {
  // TODO: API integration will be done once the backend is ready.
  return MOCK_MODELS_RESPONSE;
}

export async function fetchAllModels(): Promise<ModelDeployment[]> {
  // TODO: API integration will be done once the backend is ready.
  return MOCK_MODELS_RESPONSE.data;
}
