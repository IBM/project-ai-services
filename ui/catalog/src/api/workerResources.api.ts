import { api } from "@/api/axios";
import { WORKERS_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type { WorkerApiResponse, WorkerListResponse } from "@/types/api.types";
import type { WorkerResourceRow } from "@/components/WorkerResourcesTable/types";

export function transformWorkerToRow(
  worker: WorkerApiResponse,
): WorkerResourceRow {
  return {
    id: worker.id,
    name: worker.name,
    status: worker.status,
    runtime_type: worker.runtime_type,
    messages: worker.message ?? "",
    actions: "actions",
  };
}

export async function fetchAllWorkerResources(): Promise<WorkerApiResponse[]> {
  const response = await api.get<WorkerApiResponse[]>(
    WORKERS_ENDPOINTS.LIST_WORKERS,
  );
  return response.data;
}

export async function fetchWorkerResources(
  page = 1,
  pageSize = 20,
): Promise<WorkerListResponse> {
  const allItems = await fetchAllWorkerResources();
  const start = (page - 1) * pageSize;

  return {
    data: allItems.slice(start, start + pageSize),
    total: allItems.length,
    page,
    page_size: pageSize,
  };
}
