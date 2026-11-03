import { api } from "@/api/axios";
import { MODEL_MANAGEMENT_ENDPOINTS } from "@/constants/api-endpoints.constants";

export interface ModelProvider {
  id: string;
  name: string;
  deployment_type: "local" | "remote";
}

export interface CatalogModel {
  id: string;
  name: string;
  component_type: string;
  certified_by: string;
  description: string;
  providers: ModelProvider[];
}

export async function fetchCatalogModels(): Promise<CatalogModel[]> {
  const response = await api.get<CatalogModel[]>(
    MODEL_MANAGEMENT_ENDPOINTS.GET_CATALOG_MODELS,
  );
  return response.data;
}
