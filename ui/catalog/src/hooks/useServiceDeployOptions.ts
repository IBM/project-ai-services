import { useEffect, useRef } from "react";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import {
  fetchServiceDeployOptions,
  fetchLLMOptionsWithModels,
  fetchComponentModelsWithSchemas,
} from "@/services/deployment.api";

/**
 * Custom hook to fetch and cache service deploy options, LLM models, and component models
 * Uses Zustand store to cache data per service and avoid redundant API calls
 */
export const useServiceDeployOptions = (serviceId: string | null) => {
  const {
    getServiceDeployOptions,
    setServiceDeployOptions,
    setServiceDeployOptionsLoading,
    setServiceDeployOptionsError,
    getComponentModels,
    setComponentModels,
    setComponentModelsLoading,
    setComponentModelsError,
    setProviderSchema,
  } = useServiceDeployStore();

  const hasFetched = useRef<Record<string, boolean>>({});

  // Get cached data for this service
  const deployOptions = serviceId ? getServiceDeployOptions(serviceId) : null;
  const llmModels = serviceId ? getComponentModels(serviceId, "llm") : [];

  // Get loading and error states from store
  const deployOptionsLoading = useServiceDeployStore((state) =>
    serviceId ? state.serviceDeployOptionsLoading[serviceId] || false : false,
  );
  const deployOptionsError = useServiceDeployStore((state) =>
    serviceId ? state.serviceDeployOptionsError[serviceId] || null : null,
  );
  const llmModelsLoading = useServiceDeployStore((state) =>
    serviceId
      ? state.componentModelsLoading[`${serviceId}:llm`] || false
      : false,
  );
  const llmModelsError = useServiceDeployStore((state) =>
    serviceId ? state.componentModelsError[`${serviceId}:llm`] || null : null,
  );

  // Determine if we should be in loading state
  const shouldBeLoading =
    serviceId && !deployOptions && !deployOptionsError && !deployOptionsLoading;

  useEffect(() => {
    if (!serviceId) return;

    // Only fetch if we don't have cached data and we haven't already started fetching
    if (
      !deployOptions &&
      !hasFetched.current[serviceId] &&
      !deployOptionsLoading
    ) {
      hasFetched.current[serviceId] = true;
      setServiceDeployOptionsLoading(serviceId, true);
      setComponentModelsLoading(serviceId, "llm", true);
      setServiceDeployOptionsError(serviceId, null);
      setComponentModelsError(serviceId, "llm", null);

      // First, fetch deploy options to know which components exist
      fetchServiceDeployOptions(serviceId)
        .then(async (deployData) => {
          setServiceDeployOptions(serviceId, deployData);

          // Identify Step 1 components (exclude llm and reranker)
          const step1Components =
            deployData.components?.filter(
              (component) =>
                !["llm", "reranker"].includes(component.type) &&
                component.providers.length > 0,
            ) || [];

          // STAGE 1: Fetch Step 1 component models first (for immediate UI display)
          const step1Promises = step1Components.map((component) => {
            setComponentModelsLoading(serviceId, component.type, true);
            return fetchComponentModelsWithSchemas(
              serviceId,
              component.type,
              setProviderSchema,
              deployData, // Pass cached deploy options to avoid redundant API call
            )
              .then((models) => {
                setComponentModels(serviceId, component.type, models);
                return { type: component.type, models };
              })
              .catch((err) => {
                const errorMessage =
                  err instanceof Error
                    ? err.message
                    : `Failed to load ${component.type} models`;
                setComponentModelsError(
                  serviceId,
                  component.type,
                  errorMessage,
                );
                return { type: component.type, models: [] };
              });
          });

          // Wait for Step 1 components to finish
          await Promise.all(step1Promises);

          // STAGE 2: Fetch LLM models in background (for Step 2)
          // Don't wait for this - let it load in background
          fetchLLMOptionsWithModels(serviceId, setProviderSchema, deployData) // Pass cached deploy options
            .then((llmData) => {
              setComponentModels(serviceId, "llm", llmData);
            })
            .catch((err) => {
              const errorMessage =
                err instanceof Error
                  ? err.message
                  : "Failed to load LLM models";
              setComponentModelsError(serviceId, "llm", errorMessage);
            });
        })
        .catch((err) => {
          const errorMessage =
            err instanceof Error
              ? err.message
              : "Failed to load deploy options";
          setServiceDeployOptionsError(serviceId, errorMessage);
          setComponentModelsError(serviceId, "llm", errorMessage);
        })
        .finally(() => {
          hasFetched.current[serviceId] = false;
        });
    }
  }, [
    serviceId,
    deployOptions,
    deployOptionsLoading,
    setServiceDeployOptions,
    setServiceDeployOptionsLoading,
    setServiceDeployOptionsError,
    setComponentModels,
    setComponentModelsLoading,
    setComponentModelsError,
    setProviderSchema,
  ]);

  return {
    deployOptions,
    llmModels,
    isLoading: deployOptionsLoading || llmModelsLoading || shouldBeLoading,
    error: deployOptionsError || llmModelsError,
  };
};

// Made with Bob
