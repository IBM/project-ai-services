import { useEffect, useRef } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchDeployOptions } from "@/api/digitalAssistants";

/**
 * Custom hook to fetch and cache deploy options
 * Uses Zustand store with 15-minute cache expiration
 * Deploy options can change when service versions or component providers are updated
 */
export const useDeployOptions = () => {
  const {
    selectedArchitectureId,
    deployOptions,
    deployOptionsLoading,
    deployOptionsError,
    isDeployOptionsStale,
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
    // Don't fetch if we don't have an architecture ID yet
    if (!selectedArchitectureId) {
      return;
    }

    // Check if cache is stale (older than 15 minutes)
    const isStale = isDeployOptionsStale();

    // Fetch if we don't have data or if cache is stale, and we haven't already started fetching
    if (
      (!deployOptions || isStale) &&
      !hasFetched.current &&
      !deployOptionsLoading
    ) {
      hasFetched.current = true;
      setDeployOptionsLoading(true);
      setDeployOptionsError(null);

      fetchDeployOptions(selectedArchitectureId)
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
    selectedArchitectureId,
    deployOptions,
    deployOptionsLoading,
    isDeployOptionsStale,
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
