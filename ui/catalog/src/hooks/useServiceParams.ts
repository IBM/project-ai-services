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
 * Uses Zustand store to cache data (no time-based expiration)
 * Service params are static configuration data that only change when service definitions are updated
 */
export function useServiceParams(serviceId: string): UseServiceParamsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hasFetched = useRef(false);

  const { getServiceParams, setServiceParams } = useDeployStore();

  const params = getServiceParams(serviceId);

  useEffect(() => {
    // Only fetch if we don't have data and we haven't already started fetching
    // No time-based expiration - service params are static configuration data
    if (params || hasFetched.current || !serviceId) {
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

// Made with Bob
