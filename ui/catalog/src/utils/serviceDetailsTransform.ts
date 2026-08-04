import type { ServiceDetailsData } from "@/services/serviceDetails.api";
import type { ServiceDetailData } from "@/components/ServiceDetailPanel/ServiceDetailPanel";

/**
 * Minimal transformation - preserves server structure for dynamic rendering
 * Only extracts top-level metadata, keeps 'about' sections intact
 */
export function transformServiceDetails(
  apiData: ServiceDetailsData,
): ServiceDetailData {
  return {
    id: apiData.id,
    title: apiData.name,
    description: apiData.description,
    certifiedBy: apiData.certified_by,
    tags: apiData.architectures,
    standalone: apiData.standalone,
    // Pass through the about sections directly for dynamic rendering
    about: apiData.about || [],
  };
}

// Made with Bob
