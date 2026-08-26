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
    messages: "",
    actions: "actions",
  };
}

export async function fetchWorkerResources(
  page = 1,
  pageSize = 20,
): Promise<WorkerListResponse> {
  const { default: mockData } =
    await import("@/pages/WorkerResources/worker-resources-mock-data.json");

  const allItems = mockData.data as WorkerApiResponse[];
  const start = (page - 1) * pageSize;
  const end = start + pageSize;
  const pageItems = allItems.slice(start, end);

  return {
    data: pageItems,
    total: allItems.length,
    page,
    page_size: pageSize,
  };
}

export async function fetchAllWorkerResources(): Promise<WorkerApiResponse[]> {
  const { default: mockData } =
    await import("@/pages/WorkerResources/worker-resources-mock-data.json");
  return mockData.data as WorkerApiResponse[];
}
