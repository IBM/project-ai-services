import { useEffect, useRef } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchResources } from "@/api/digitalAssistants";

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes

/**
 * Custom hook to fetch and cache system resources
 * Uses Zustand store to cache data and avoid redundant API calls
 */
export const useResources = () => {
  const {
    resources,
    resourcesLoading,
    resourcesError,
    resourcesFetchedAt,
    setResources,
    setResourcesLoading,
    setResourcesError,
  } = useDeployStore();

  const hasFetched = useRef(false);

  // Determine if we should be in loading state
  // Loading if: no data AND no error AND not currently loading (will start loading in useEffect)
  const shouldBeLoading = !resources && !resourcesError && !resourcesLoading;

  useEffect(() => {
    // Check if cache is stale
    const isStale = resourcesFetchedAt
      ? Date.now() - resourcesFetchedAt > CACHE_DURATION
      : true;

    // Only fetch if we don't have data or if cache is stale, and we haven't already started fetching
    if ((!resources || isStale) && !hasFetched.current && !resourcesLoading) {
      hasFetched.current = true;
      setResourcesLoading(true);
      setResourcesError(null);

      fetchResources()
        .then((data) => {
          setResources(data);
        })
        .catch((err) => {
          const errorMessage =
            err instanceof Error ? err.message : "Failed to load resources";
          setResourcesError(errorMessage);
        })
        .finally(() => {
          hasFetched.current = false;
        });
    }
  }, [
    resources,
    resourcesFetchedAt,
    resourcesLoading,
    setResources,
    setResourcesLoading,
    setResourcesError,
  ]);

  return {
    resources,
    isLoading: resourcesLoading || shouldBeLoading,
    error: resourcesError,
  };
};
