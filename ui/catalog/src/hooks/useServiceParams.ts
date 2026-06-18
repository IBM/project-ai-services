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
 * Uses Zustand store (no cache expiration - fetches only if not in cache)
 */
export function useServiceParams(serviceId: string): UseServiceParamsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hasFetched = useRef(false);

  const { getServiceParams, setServiceParams } = useDeployStore();

  const params = getServiceParams(serviceId);

  useEffect(() => {
    // Don't fetch if no serviceId
    if (!serviceId) {
      return;
    }

    // Fetch only if we don't have data and haven't already started fetching
    if (params || hasFetched.current) {
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
  }, [serviceId, params, setServiceParams]);

  return { params, isLoading, error };
}
