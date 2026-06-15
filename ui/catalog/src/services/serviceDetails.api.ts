import { api } from "@/api/axios";
import { SERVICE_ENDPOINTS } from "@/constants/api-endpoints.constants";

export interface AboutSection {
  title: string;
  value?: string;
  values?: string[] | AboutSubSection[];
  sections?: AboutSection[];
  ctaLabel?: string;
  url?: string;
}

export interface AboutSubSection {
  title: string;
  value: string;
}

export interface ServiceDetailsData {
  id: string;
  name: string;
  description: string;
  type: string;
  certified_by?: string;
  architectures?: string[];
  standalone?: boolean;
  about?: AboutSection[];
}

export async function fetchServiceDetails(
  serviceId: string,
): Promise<ServiceDetailsData> {
  const response = await api.get<ServiceDetailsData>(
    SERVICE_ENDPOINTS.GET_SERVICE_DETAILS(serviceId),
  );
  return response.data;
}

// Made with Bob
