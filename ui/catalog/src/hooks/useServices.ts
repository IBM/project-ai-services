import { useEffect, useRef, useCallback } from "react";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { fetchServices } from "@/services/deployment.api";

/**
 * Custom hook to fetch and cache available services
 * Uses Zustand store to cache data and avoid redundant API calls
 * Note: Services are static data and don't need refetching
 *
 * @param autoFetch - If true, automatically fetches on mount. If false, only returns cached data.
 */
export const useServices = (autoFetch: boolean = false) => {
  const {
    services,
    servicesLoading,
    servicesError,
    setServices,
    setServicesLoading,
    setServicesError,
  } = useServiceDeployStore();

  const hasFetched = useRef(false);

  // Determine if we should be in loading state
  const shouldBeLoading = !services && !servicesError && !servicesLoading;

  // Manual fetch function that can be called by components
  const refetch = useCallback(async () => {
    if (servicesLoading) return; // Don't fetch if already loading

    setServicesLoading(true);
    setServicesError(null);

    try {
      const data = await fetchServices();
      setServices(data);
    } catch (err) {
      const errorMessage =
        err instanceof Error ? err.message : "Failed to load services";
      setServicesError(errorMessage);
    }
  }, [servicesLoading, setServices, setServicesLoading, setServicesError]);

  useEffect(() => {
    // Only proceed if autoFetch is enabled
    if (!autoFetch) {
      return;
    }

    // Check if we have valid cached data
    const hasValidCache = services && services.length > 0;

    // Only fetch if we don't have cached data and we're not already fetching
    const shouldFetch =
      !hasValidCache && !hasFetched.current && !servicesLoading;

    if (shouldFetch) {
      hasFetched.current = true;
      refetch().finally(() => {
        hasFetched.current = false;
      });
    }
  }, [autoFetch, services, servicesLoading, refetch]);

  return {
    services: services || [],
    isLoading: servicesLoading || shouldBeLoading,
    error: servicesError,
    refetch,
  };
};

// Made with Bob
