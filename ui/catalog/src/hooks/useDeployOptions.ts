import { useEffect, useRef } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchDeployOptions } from "@/api/digitalAssistants";

/**
 * Custom hook to fetch and cache deploy options
 * Uses Zustand store to cache data (no time-based expiration)
 * Deploy options are static catalog metadata that only change when architecture definitions are updated
 */
export const useDeployOptions = () => {
  const {
    selectedArchitectureId,
    deployOptions,
    deployOptionsLoading,
    deployOptionsError,
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

    // Only fetch if we don't have data and we haven't already started fetching
    // No time-based expiration - deployOptions are static catalog metadata
    if (!deployOptions && !hasFetched.current && !deployOptionsLoading) {
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
