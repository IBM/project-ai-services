import { useCallback, useEffect } from "react";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { fetchServices } from "@/api/applications.api";

/**
 * Fetches available services.
 *
 * @param autoFetch - When true, fetches services on mount.
 *                    Pass `false` to defer fetching until `refetch()` is called.
 */
export const useServices = (autoFetch = true) => {
  const {
    services,
    servicesLoading,
    servicesError,
    setServices,
    setServicesLoading,
    setServicesError,
  } = useServiceDeployStore();

  // Read servicesLoading via getState() to keep it out of deps and prevent
  // refetch from being recreated on every loading state change.
  const refetch = useCallback(async () => {
    if (useServiceDeployStore.getState().servicesLoading) return;

    setServicesError(null);
    setServicesLoading(true);

    try {
      const data = await fetchServices();
      setServices(data);
    } catch (err) {
      const errorMessage =
        err instanceof Error ? err.message : "Failed to load services";

      setServicesError(errorMessage);
    }
  }, [setServices, setServicesLoading, setServicesError]);

  useEffect(() => {
    if (!autoFetch) return;

    void refetch();
  }, [autoFetch, refetch]);

  return {
    services: services ?? [],
    isLoading: servicesLoading || (!services && !servicesError),
    error: servicesError,
    refetch,
  };
};
