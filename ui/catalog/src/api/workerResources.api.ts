import { api } from "@/api/axios";
import { WORKERS_ENDPOINTS } from "@/constants/api-endpoints.constants";
import { WORKER_RUNTIME_LABELS } from "@/constants/app.constants";
import type {
  WorkerApiResponse,
  WorkerListResponse,
  WorkerRegisterResponse,
} from "@/types/api.types";
import type { WorkerResourceRow } from "@/components/WorkerResourcesTable/types";

export async function deregisterWorker(id: string): Promise<void> {
  await api.delete(WORKERS_ENDPOINTS.DEREGISTER_WORKER(id));
}

export function transformWorkerToRow(
  worker: WorkerApiResponse,
): WorkerResourceRow {
  return {
    id: worker.id,
    name: worker.name,
    status: worker.status,
    runtime_type: worker.runtime_type,
    runtime_label:
      WORKER_RUNTIME_LABELS[worker.runtime_type]?.short ?? worker.runtime_type,
    services_count: worker.application_ids?.length ?? 0,
    message: worker.message ?? "",
    actions: "actions",
  };
}

export async function fetchWorkerResources(
  page = 1,
  pageSize = 20,
): Promise<WorkerListResponse> {
  const res = await api.get<WorkerListResponse>(
    WORKERS_ENDPOINTS.LIST_WORKERS,
    {
      params: { page, page_size: pageSize },
    },
  );
  return res.data;
}

export async function fetchAllWorkerResources(): Promise<WorkerApiResponse[]> {
  let currentPage = 1;
  let hasNext = true;
  const allData: WorkerApiResponse[] = [];

  while (hasNext) {
    const response = await fetchWorkerResources(currentPage, 100);
    allData.push(...response.data);
    hasNext = response.pagination.has_next;
    currentPage++;
  }

  return allData;
}

export async function registerWorker(
  workerName: string,
): Promise<WorkerRegisterResponse> {
  const response = await api.post<WorkerRegisterResponse>(
    WORKERS_ENDPOINTS.REGISTER_WORKER,
    { worker_name: workerName },
  );
  return response.data;
}
