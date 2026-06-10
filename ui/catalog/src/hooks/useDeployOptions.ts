import { useEffect, useRef } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchDeployOptions } from "@/api/digitalAssistants";

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes

/**
 * Custom hook to fetch and cache deploy options
 * Uses Zustand store to cache data and avoid redundant API calls
 */
export const useDeployOptions = () => {
  const {
    deployOptions,
    deployOptionsLoading,
    deployOptionsError,
    deployOptionsFetchedAt,
    setDeployOptions,
    setDeployOptionsLoading,
    setDeployOptionsError,
  } = useDeployStore();

  const hasFetched = useRef(false);

  // Determine if we should be in loading state
  // Loading if: no data AND no error AND not currently loading (will start loading in useEffect)
  const shouldBeLoading =
    !deployOptions && !deployOptionsError && !deployOptionsLoading;

  useEffect(() => {
    // Check if cache is stale
    const isStale = deployOptionsFetchedAt
      ? Date.now() - deployOptionsFetchedAt > CACHE_DURATION
      : true;

    // Only fetch if we don't have data or if cache is stale, and we haven't already started fetching
    if (
      (!deployOptions || isStale) &&
      !hasFetched.current &&
      !deployOptionsLoading
    ) {
      hasFetched.current = true;
      setDeployOptionsLoading(true);
      setDeployOptionsError(null);

      fetchDeployOptions()
        .then((data) => {
          setDeployOptions(data);
        })
        .catch((err) => {
          const errorMessage =
            err instanceof Error
              ? err.message
              : "Failed to load deploy options";
          setDeployOptionsError(errorMessage);
        })
        .finally(() => {
          hasFetched.current = false;
        });
    }
  }, [
    deployOptions,
    deployOptionsFetchedAt,
    deployOptionsLoading,
    setDeployOptions,
    setDeployOptionsLoading,
    setDeployOptionsError,
  ]);

  return {
    deployOptions,
    isLoading: deployOptionsLoading || shouldBeLoading,
    error: deployOptionsError,
  };
};
