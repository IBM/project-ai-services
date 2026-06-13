import { useEffect, useRef, useState } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchProviderParams } from "@/api/digitalAssistants";

interface UseProviderParamsResult {
  params: Record<string, unknown> | null;
  isLoading: boolean;
  error: string | null;
}

/**
 * Hook to fetch and cache provider parameters
 * Caches results in Zustand store to avoid redundant API calls
 */
export function useProviderParams(
  componentType: string,
  providerId: string,
): UseProviderParamsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hasFetched = useRef(false);

  const { getProviderParams, setProviderParams, isProviderParamsStale } =
    useDeployStore();

  const params = getProviderParams(componentType, providerId);

  useEffect(() => {
    // Skip if already fetching or if we have fresh cached data
    if (
      hasFetched.current ||
      (params && !isProviderParamsStale(componentType, providerId))
    ) {
      return;
    }

    const fetchParams = async () => {
      hasFetched.current = true;
      setIsLoading(true);
      setError(null);

      try {
        const response = await fetchProviderParams(componentType, providerId);
        setProviderParams(componentType, providerId, response);
      } catch (err) {
        const errorMessage =
          err instanceof Error
            ? err.message
            : "Failed to fetch provider params";
        setError(errorMessage);
        console.error(
          `Error fetching params for ${componentType}/${providerId}:`,
          err,
        );
      } finally {
        setIsLoading(false);
      }
    };

    fetchParams();
  }, [
    componentType,
    providerId,
    params,
    isProviderParamsStale,
    setProviderParams,
  ]);

  return { params, isLoading, error };
}

/**
 * Hook to fetch provider params for multiple providers at once
 * Useful for batch fetching (e.g., all LLM providers)
 */
export function useBatchProviderParams(
  componentType: string,
  providerIds: string[],
): {
  paramsMap: Record<string, Record<string, unknown>>;
  isLoading: boolean;
  errors: Record<string, string>;
} {
  const [isLoading, setIsLoading] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const hasFetched = useRef(false);

  const { getProviderParams, setProviderParams, isProviderParamsStale } =
    useDeployStore();

  // Build params map from cache
  const paramsMap: Record<string, Record<string, unknown>> = {};
  for (const providerId of providerIds) {
    const cached = getProviderParams(componentType, providerId);
    if (cached) {
      paramsMap[providerId] = cached;
    }
  }

  useEffect(() => {
    if (hasFetched.current || providerIds.length === 0) {
      return;
    }

    // Find providers that need fetching (not cached or stale)
    const providersToFetch = providerIds.filter((providerId) => {
      const cached = getProviderParams(componentType, providerId);
      return !cached || isProviderParamsStale(componentType, providerId);
    });

    if (providersToFetch.length === 0) {
      return;
    }

    const fetchAllParams = async () => {
      hasFetched.current = true;
      setIsLoading(true);
      setErrors({});

      const results = await Promise.allSettled(
        providersToFetch.map(async (providerId) => {
          const response = await fetchProviderParams(componentType, providerId);
          return { providerId, response };
        }),
      );

      const newErrors: Record<string, string> = {};

      results.forEach((result, index) => {
        const providerId = providersToFetch[index];
        if (result.status === "fulfilled") {
          setProviderParams(componentType, providerId, result.value.response);
        } else {
          const errorMessage =
            result.reason instanceof Error
              ? result.reason.message
              : "Failed to fetch params";
          newErrors[providerId] = errorMessage;
          console.warn(
            `Failed to fetch params for ${componentType}/${providerId}:`,
            result.reason,
          );
        }
      });

      setErrors(newErrors);
      setIsLoading(false);
    };

    fetchAllParams();
  }, [
    componentType,
    providerIds,
    getProviderParams,
    setProviderParams,
    isProviderParamsStale,
  ]);

  return { paramsMap, isLoading, errors };
}
