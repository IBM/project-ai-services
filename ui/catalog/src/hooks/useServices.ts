import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { fetchServices } from "@/api/applications.api";
import { useStoreFetch } from "@/hooks/useStoreFetch";

const getServicesLoading = () =>
  useServiceDeployStore.getState().servicesLoading;

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

  const { data, isLoading, error, refetch } = useStoreFetch({
    data: services,
    loading: servicesLoading,
    error: servicesError,
    setData: setServices,
    setLoading: setServicesLoading,
    setError: setServicesError,
    getLoading: getServicesLoading,
    fetcher: fetchServices,
    fallbackError: "Failed to load services",
    autoFetch,
  });

  return { services: data, isLoading, error, refetch };
};
