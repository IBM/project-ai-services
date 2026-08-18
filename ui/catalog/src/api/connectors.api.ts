import type {
  DataSourceConnectorApiResponse,
  DataSourceConnectorsListResponse,
  DataSourceConnectorRow,
} from "@/components/DataSourceConnectorsTable/types";

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
    messages: connector.status === "Connected" ? "" : (connector.message ?? ""),
    actions: "actions",
  };
}

export async function fetchDataSourceConnectors(
  page = 1,
  pageSize = 10,
): Promise<DataSourceConnectorsListResponse> {
  const { default: mockData } =
    await import("@/pages/Connectors/datasource-connectors-mock-data.json");

  const allItems = mockData.data as DataSourceConnectorApiResponse[];
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

export async function fetchAllDataSourceConnectors(): Promise<
  DataSourceConnectorApiResponse[]
> {
  const { default: mockData } =
    await import("@/pages/Connectors/datasource-connectors-mock-data.json");
  return mockData.data as DataSourceConnectorApiResponse[];
}
