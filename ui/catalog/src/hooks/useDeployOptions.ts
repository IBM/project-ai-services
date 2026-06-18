import { useEffect } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchDeployOptions } from "@/api/digitalAssistants";
import { dedupe } from "@/utils/requestManager";

/**
 * Custom hook to fetch and cache deploy options
 * Uses Zustand store with 15-minute cache expiration
 * Deploy options can change when service versions or component providers are updated
 *
 * Uses request de-duping to prevent race conditions when multiple components
 * request the same data simultaneously
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

    // Fetch if we don't have data or if cache is stale
    // dedupe() handles preventing duplicate in-flight requests
    if ((!deployOptions || isStale) && !deployOptionsLoading) {
      setDeployOptionsLoading(true);
      setDeployOptionsError(null);

      // Use dedupe to prevent simultaneous requests
      const requestKey = `deployOptions:${selectedArchitectureId}`;
      dedupe(requestKey, () => fetchDeployOptions(selectedArchitectureId))
        .then((data) => {
          setDeployOptions(data);
        })
        .catch((err) => {
          const errorMessage =
            err instanceof Error
              ? err.message
              : "Failed to load deploy options";
          setDeployOptionsError(errorMessage);
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
