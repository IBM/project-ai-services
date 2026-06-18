import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import type { ProviderSchema } from "@/services/deployment.api";

/**
 * Custom hook to get provider schema from the store
 * Schemas are pre-fetched and cached when LLM options are loaded
 * This avoids redundant API calls when switching between inference methods
 */
export const useProviderSchema = (
  serviceId: string | null,
  componentType: string | null,
  providerId: string | null,
): { schema: ProviderSchema | null } => {
  const getProviderSchema = useServiceDeployStore(
    (state) => state.getProviderSchema,
  );

  // Get schema from store if all parameters are provided
  if (!serviceId || !componentType || !providerId) {
    return { schema: null };
  }

  const schema = getProviderSchema(serviceId, componentType, providerId);
  return { schema };
};

// Made with Bob
