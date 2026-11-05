import { useModelsStore } from "@/store/models.store";
import { fetchCatalogModels } from "@/api/models.api";
import { useStoreFetch } from "@/hooks/useStoreFetch";

const getModelsLoading = () => useModelsStore.getState().modelsLoading;

export const useModels = (autoFetch = true) => {
  const {
    models,
    modelsLoading,
    modelsError,
    setModels,
    setModelsLoading,
    setModelsError,
  } = useModelsStore();

  const { data, isLoading, error, refetch } = useStoreFetch({
    data: models,
    loading: modelsLoading,
    error: modelsError,
    setData: setModels,
    setLoading: setModelsLoading,
    setError: setModelsError,
    getLoading: getModelsLoading,
    fetcher: fetchCatalogModels,
    fallbackError: "Failed to load models",
    autoFetch,
  });

  return { models: data, isLoading, error, refetch };
};
