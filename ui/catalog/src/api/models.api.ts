import { api } from "@/api/axios";
import { MODEL_MANAGEMENT_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type { CatalogAboutSection } from "@/types/api.types";

export interface ModelProvider {
  id: string;
  name: string;
  deployment_type: "local" | "remote";
}

export interface SupportedProvider {
  id: string;
  name: string;
  deployment_type: "local" | "remote";
  accelerator_type: string | null;
}

export interface ComponentType {
  id: string;
  name: string;
  short_name: string;
}

export interface CatalogModel {
  id: string;
  name: string;
  component_type: ComponentType;
  certified_by: string;
  description: string;
  providers: ModelProvider[];
}

export interface CatalogModelDetails {
  id: string;
  name: string;
  component_type: ComponentType;
  certified_by: string;
  license?: string;
  description: string;
  about?: CatalogAboutSection[];
  supported_providers: SupportedProvider[];
}

export async function fetchCatalogModels(): Promise<CatalogModel[]> {
  const response = await api.get<CatalogModel[]>(
    MODEL_MANAGEMENT_ENDPOINTS.GET_CATALOG_MODELS,
  );
  return response.data;
}

export async function fetchCatalogModelDetails(
  id: string,
): Promise<CatalogModelDetails> {
  const response = await api.get<CatalogModelDetails>(
    MODEL_MANAGEMENT_ENDPOINTS.GET_CATALOG_MODEL_DETAILS(id),
  );
  return response.data;
}
