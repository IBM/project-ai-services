import { useEffect, useState } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchServiceParams } from "@/api/digitalAssistants";
import { dedupe } from "@/utils/requestManager";

interface UseServiceParamsResult {
  params: Record<string, unknown> | null;
  isLoading: boolean;
  error: string | null;
}

/**
 * Hook to fetch and cache service-level parameters
 * Uses Zustand store with 1-hour cache expiration
 * Uses request de-duping to prevent race conditions
 */
export function useServiceParams(serviceId: string): UseServiceParamsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { getServiceParams, setServiceParams, isServiceParamsStale } =
    useDeployStore();

  const params = getServiceParams(serviceId);

  useEffect(() => {
    // Don't fetch if no serviceId
    if (!serviceId) {
      return;
    }

    // Check if cache is stale
    const isStale = isServiceParamsStale(serviceId);

    // Fetch only if we don't have data or cache is stale
    // dedupe() handles preventing duplicate in-flight requests
    if (!params || isStale) {
      const fetchParams = async () => {
        setIsLoading(true);
        setError(null);

        try {
          const requestKey = `serviceParams:${serviceId}`;
          const response = await dedupe(requestKey, () =>
            fetchServiceParams(serviceId),
          );
          setServiceParams(serviceId, response);
        } catch (err) {
          const errorMessage =
            err instanceof Error
              ? err.message
              : "Failed to fetch service params";
          setError(errorMessage);
          console.error(`Error fetching params for service ${serviceId}:`, err);
        } finally {
          setIsLoading(false);
        }
      };

      fetchParams();
    }
  }, [serviceId, params, setServiceParams, isServiceParamsStale]);

  return { params, isLoading, error };
}
