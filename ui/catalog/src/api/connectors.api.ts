import { api } from "@/api/axios";
import { CONNECTORS_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type {
  ConnectorParamsSchema,
  ConnectorType,
  CreateDatasourceRequest,
  CreateDatasourceResponse,
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

// ---------------------------------------------------------------------------
// Fetch all datasource connector types
// ---------------------------------------------------------------------------

export async function fetchConnectorTypes(): Promise<ConnectorType[]> {
  const res = await api.get<ConnectorType[]>(
    CONNECTORS_ENDPOINTS.GET_CONNECTOR_TYPES,
    {
      params: { type: "datasource" },
    },
  );
  return res.data;
}

// ---------------------------------------------------------------------------
// Fetch the JSON-Schema params for a specific connector provider
// ---------------------------------------------------------------------------

export async function fetchConnectorParams(
  connectorId: string,
): Promise<ConnectorParamsSchema> {
  const res = await api.get<ConnectorParamsSchema>(
    CONNECTORS_ENDPOINTS.GET_CONNECTOR_PARAMS(connectorId),
  );
  return res.data;
}

// ---------------------------------------------------------------------------
// Create a new datasource connector
// ---------------------------------------------------------------------------

export async function createDataSourceConnector(
  payload: CreateDatasourceRequest,
): Promise<CreateDatasourceResponse> {
  const res = await api.post<CreateDatasourceResponse>(
    CONNECTORS_ENDPOINTS.CREATE_DATASOURCE,
    payload,
  );
  return res.data;
}
