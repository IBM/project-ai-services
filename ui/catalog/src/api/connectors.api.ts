import { api } from "@/api/axios";
import { CONNECTORS_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type {
  DataSourceConnectorApiResponse,
  DataSourceConnectorsListResponse,
} from "@/types/api.types";
import type { DataSourceConnectorRow } from "@/components/DataSourceConnectorsTable/types";

export function transformConnectorToRow(
  connector: DataSourceConnectorApiResponse,
): DataSourceConnectorRow {
  return {
    id: connector.id,
    name: connector.name,
    status: connector.status,
    type: connector.provider.name,
    services: connector.connected_services,
    // Suppress the message column for healthy connectors — no warning to show.
    messages: connector.status === "connected" ? "" : (connector.message ?? ""),
    actions: "actions",
  };
}

export async function fetchDataSourceConnectors(
  page = 1,
  pageSize = 20,
): Promise<DataSourceConnectorsListResponse> {
  const res = await api.get<DataSourceConnectorsListResponse>(
    CONNECTORS_ENDPOINTS.LIST_CONNECTORS,
    { params: { page, page_size: pageSize } },
  );
  return res.data;
}

export async function fetchAllDataSourceConnectors(): Promise<
  DataSourceConnectorApiResponse[]
> {
  let currentPage = 1;
  let hasNext = true;
  const allData: DataSourceConnectorApiResponse[] = [];

  while (hasNext) {
    const response = await fetchDataSourceConnectors(currentPage, 100);
    allData.push(...response.data);
    hasNext = response.pagination?.has_next ?? false;
    currentPage++;
  }

  return allData;
}
