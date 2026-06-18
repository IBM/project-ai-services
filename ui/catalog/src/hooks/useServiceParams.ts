import { useEffect, useRef, useState } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchServiceParams } from "@/api/digitalAssistants";

interface UseServiceParamsResult {
  params: Record<string, unknown> | null;
  isLoading: boolean;
  error: string | null;
}

/**
 * Hook to fetch and cache service-level parameters
 * Uses Zustand store with 15-minute cache expiration
 * Service params can change when service definitions are updated
 */
export function useServiceParams(serviceId: string): UseServiceParamsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hasFetched = useRef(false);

  const { getServiceParams, setServiceParams, isServiceParamsStale } =
    useDeployStore();

  const params = getServiceParams(serviceId);
  const isStale = isServiceParamsStale(serviceId);

  useEffect(() => {
    // Don't fetch if no serviceId
    if (!serviceId) {
      return;
    }

    // Fetch if we don't have data or if cache is stale, and we haven't already started fetching
    if ((params && !isStale) || hasFetched.current) {
      return;
    }

    const fetchParams = async () => {
      hasFetched.current = true;
      setIsLoading(true);
      setError(null);

      try {
        const response = await fetchServiceParams(serviceId);
        setServiceParams(serviceId, response);
      } catch (err) {
        const errorMessage =
          err instanceof Error ? err.message : "Failed to fetch service params";
        setError(errorMessage);
        console.error(`Error fetching params for service ${serviceId}:`, err);
      } finally {
        setIsLoading(false);
        hasFetched.current = false;
      }
    };

    fetchParams();
  }, [serviceId, params, isStale, setServiceParams]);

  return { params, isLoading, error };
}
