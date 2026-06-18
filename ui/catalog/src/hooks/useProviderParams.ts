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
 * Uses Zustand store (no cache expiration - fetches only if not in cache)
 */
export function useProviderParams(
  componentType: string,
  providerId: string,
): UseProviderParamsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const hasFetched = useRef(false);

  const { getProviderParams, setProviderParams } = useDeployStore();

  const params = getProviderParams(componentType, providerId);

  useEffect(() => {
    // Fetch only if we don't have data and haven't already started fetching
    if (params || hasFetched.current) {
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
        hasFetched.current = false;
      }
    };

    fetchParams();
  }, [componentType, providerId, params, setProviderParams]);

  return { params, isLoading, error };
}

/**
 * Hook to fetch provider params for multiple providers at once
 * Uses Zustand store (no cache expiration - fetches only if not in cache)
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

  const { getProviderParams, setProviderParams } = useDeployStore();

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

    // Find providers that need fetching (not cached)
    const providersToFetch = providerIds.filter((providerId) => {
      return !getProviderParams(componentType, providerId);
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
      hasFetched.current = false;
    };

    fetchAllParams();
  }, [componentType, providerIds, getProviderParams, setProviderParams]);

  return { paramsMap, isLoading, errors };
}

/**
 * Hook to fetch provider params for multiple component types at once
 * This is the truly dynamic solution that respects Rules of Hooks
 * Uses Zustand store (no cache expiration - fetches only if not in cache)
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
  const hasFetched = useRef(false);

  const { getProviderParams, setProviderParams } = useDeployStore();

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
    if (hasFetched.current || Object.keys(componentTypesWithIds).length === 0) {
      return;
    }

    // Find all providers that need fetching (not cached) across all component types
    const providersToFetch: Array<{
      componentType: string;
      providerId: string;
    }> = [];

    for (const [componentType, providerIds] of Object.entries(
      componentTypesWithIds,
    )) {
      for (const providerId of providerIds) {
        if (!getProviderParams(componentType, providerId)) {
          providersToFetch.push({ componentType, providerId });
        }
      }
    }

    if (providersToFetch.length === 0) {
      return;
    }

    const fetchAllParams = async () => {
      hasFetched.current = true;
      setIsLoading(true);
      setErrorsByType({});

      // Fetch all params in parallel
      const results = await Promise.allSettled(
        providersToFetch.map(async ({ componentType, providerId }) => {
          const response = await fetchProviderParams(componentType, providerId);
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
      hasFetched.current = false;
    };

    fetchAllParams();
  }, [componentTypesWithIds, getProviderParams, setProviderParams]);

  return { paramsByType, isLoading, errorsByType };
}
