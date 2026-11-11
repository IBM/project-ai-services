import { api } from "@/api/axios";
import { SERVICE_ENDPOINTS } from "@/constants/api-endpoints.constants";
import type { CatalogAboutSection } from "@/types/api.types";

export interface ServiceDetailsData {
  id: string;
  name: string;
  description: string;
  type: string;
  certified_by?: string;
  architectures?: string[];
  standalone?: boolean;
  about?: CatalogAboutSection[];
}

export async function fetchServiceDetails(
  serviceId: string,
): Promise<ServiceDetailsData> {
  const response = await api.get<ServiceDetailsData>(
    SERVICE_ENDPOINTS.GET_SERVICE_DETAILS(serviceId),
  );
  return response.data;
}
