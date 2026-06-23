import { useEffect, useState } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchProviderParams } from "@/api/digitalAssistants";
import { dedupe } from "@/utils/requestManager";

interface UseProviderParamsResult {
  params: Record<string, unknown> | null;
  isLoading: boolean;
  error: string | null;
}

/**
 * Hook to fetch and cache provider parameters
 * Uses Zustand store with 1-hour cache expiration
 * Uses request de-duping to prevent race conditions
 */
export function useProviderParams(
  componentType: string,
  providerId: string,
): UseProviderParamsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { getProviderParams, setProviderParams, isProviderParamsStale } =
    useDeployStore();

  const params = getProviderParams(componentType, providerId);

  useEffect(() => {
    // Check if cache is stale
    const isStale = isProviderParamsStale(componentType, providerId);

    // Fetch only if we don't have data or cache is stale
    // dedupe() handles preventing duplicate in-flight requests per key
    if (!params || isStale) {
      const fetchParams = async () => {
        setIsLoading(true);
        setError(null);

        try {
          const requestKey = `providerParams:${componentType}:${providerId}`;
          const response = await dedupe(requestKey, () =>
            fetchProviderParams(componentType, providerId),
          );
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
    }
  }, [
    componentType,
    providerId,
    params,
    setProviderParams,
    isProviderParamsStale,
  ]);

  return { params, isLoading, error };
}

/**
 * Hook to fetch provider params for multiple providers at once
 * Uses Zustand store with 1-hour cache expiration
 * Uses request de-duping to prevent race conditions
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
    if (providerIds.length === 0) {
      return;
    }

    // Find providers that need fetching (not cached or stale)
    const providersToFetch = providerIds.filter((providerId) => {
      const cached = getProviderParams(componentType, providerId);
      const isStale = isProviderParamsStale(componentType, providerId);
      return !cached || isStale;
    });

    if (providersToFetch.length === 0) {
      return;
    }

    const fetchAllParams = async () => {
      setIsLoading(true);
      setErrors({});

      const results = await Promise.allSettled(
        providersToFetch.map(async (providerId) => {
          const requestKey = `providerParams:${componentType}:${providerId}`;
          const response = await dedupe(requestKey, () =>
            fetchProviderParams(componentType, providerId),
          );
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

/**
 * Hook to fetch provider params for multiple component types at once
 * This is the truly dynamic solution that respects Rules of Hooks
 * Uses Zustand store with 1-hour cache expiration
 * Uses request de-duping to prevent race conditions
 */
export function useMultiTypeProviderParams(
  componentTypesWithIds: Record<string, string[]>,
): {
  paramsByType: Record<string, Record<string, Record<string, unknown>>>;
  isLoading: boolean;
  errorsByType: Record<string, Record<string, string>>;
} {
  const [isLoading, setIsLoading] = useState(false);
  const [errorsByType, setErrorsByType] = useState<
    Record<string, Record<string, string>>
  >({});

  const { getProviderParams, setProviderParams, isProviderParamsStale } =
    useDeployStore();

  // Build params map from cache for all component types
  const paramsByType: Record<
    string,
    Record<string, Record<string, unknown>>
  > = {};
  for (const [componentType, providerIds] of Object.entries(
    componentTypesWithIds,
  )) {
    paramsByType[componentType] = {};
    for (const providerId of providerIds) {
      const cached = getProviderParams(componentType, providerId);
      if (cached) {
        paramsByType[componentType][providerId] = cached;
      }
    }
  }

  useEffect(() => {
    if (Object.keys(componentTypesWithIds).length === 0) {
      return;
    }

    // Find all providers that need fetching (not cached or stale) across all component types
    const providersToFetch: Array<{
      componentType: string;
      providerId: string;
    }> = [];

    for (const [componentType, providerIds] of Object.entries(
      componentTypesWithIds,
    )) {
      for (const providerId of providerIds) {
        const cached = getProviderParams(componentType, providerId);
        const isStale = isProviderParamsStale(componentType, providerId);
        if (!cached || isStale) {
          providersToFetch.push({ componentType, providerId });
        }
      }
    }

    if (providersToFetch.length === 0) {
      return;
    }

    const fetchAllParams = async () => {
      setIsLoading(true);
      setErrorsByType({});

      const results = await Promise.allSettled(
        providersToFetch.map(async ({ componentType, providerId }) => {
          const requestKey = `providerParams:${componentType}:${providerId}`;
          const response = await dedupe(requestKey, () =>
            fetchProviderParams(componentType, providerId),
          );
          return { componentType, providerId, response };
        }),
      );

      const newErrorsByType: Record<string, Record<string, string>> = {};

      results.forEach((result, index) => {
        const { componentType, providerId } = providersToFetch[index];

        if (result.status === "fulfilled") {
          setProviderParams(componentType, providerId, result.value.response);
        } else {
          const errorMessage =
            result.reason instanceof Error
              ? result.reason.message
              : "Failed to fetch params";

          if (!newErrorsByType[componentType]) {
            newErrorsByType[componentType] = {};
          }
          newErrorsByType[componentType][providerId] = errorMessage;

          console.warn(
            `Failed to fetch params for ${componentType}/${providerId}:`,
            result.reason,
          );
        }
      });

      setErrorsByType(newErrorsByType);
      setIsLoading(false);
    };

    fetchAllParams();
  }, [
    componentTypesWithIds,
    getProviderParams,
    setProviderParams,
    isProviderParamsStale,
  ]);

  return { paramsByType, isLoading, errorsByType };
}
