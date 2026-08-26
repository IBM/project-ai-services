import { useEffect, useRef } from "react";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import {
  fetchServiceDeployOptions,
  fetchLLMOptionsWithModels,
  fetchComponentModelsWithSchemas,
} from "@/api/applications.api";
import { COMPONENT_TYPES } from "@/constants";

/**
 * Custom hook to fetch and cache service deploy options, LLM models, and component models.
 * Uses Zustand store to cache data per service and avoid redundant API calls.
 * On reopen, retries only errored models
 */
export const useServiceDeployOptions = (
  serviceId: string | null,
  open: boolean,
) => {
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

  const hasFetchedOptions = useRef<Record<string, boolean>>({});

  // Get cached data for this service
  const deployOptions = serviceId ? getServiceDeployOptions(serviceId) : null;
  const llmModels = serviceId
    ? getComponentModels(serviceId, COMPONENT_TYPES.LLM)
    : [];

  // Get loading and error states from store
  const deployOptionsLoading = useServiceDeployStore((state) =>
    serviceId ? state.serviceDeployOptionsLoading[serviceId] || false : false,
  );
  const deployOptionsError = useServiceDeployStore((state) =>
    serviceId ? state.serviceDeployOptionsError[serviceId] || null : null,
  );
  const llmModelsLoading = useServiceDeployStore((state) =>
    serviceId
      ? state.componentModelsLoading[`${serviceId}:${COMPONENT_TYPES.LLM}`] ||
        false
      : false,
  );
  const llmModelsError = useServiceDeployStore((state) =>
    serviceId
      ? state.componentModelsError[`${serviceId}:${COMPONENT_TYPES.LLM}`] ||
        null
      : null,
  );

  // Determine if we should be in loading state
  const shouldBeLoading =
    serviceId && !deployOptions && !deployOptionsError && !deployOptionsLoading;

  // Fetch deploy options (and all component models) when not yet cached.
  // On reopen, retries only errored models without re-fetching deploy options.
  useEffect(() => {
    if (!open || !serviceId) return;

    const storeState = useServiceDeployStore.getState();

    // --- Path A: deploy options not cached yet — full fetch ---
    if (
      !deployOptions &&
      !hasFetchedOptions.current[serviceId] &&
      !deployOptionsLoading
    ) {
      hasFetchedOptions.current[serviceId] = true;
      setServiceDeployOptionsLoading(serviceId, true);
      setComponentModelsLoading(serviceId, COMPONENT_TYPES.LLM, true);
      setServiceDeployOptionsError(serviceId, null);
      setComponentModelsError(serviceId, COMPONENT_TYPES.LLM, null);

      // First, fetch deploy options to know which components exist
      fetchServiceDeployOptions(serviceId)
        .then(async (deployData) => {
          setServiceDeployOptions(serviceId, deployData);

          // Identify Step 1 components (exclude llm and reranker)
          const step1Components =
            deployData.components?.filter(
              (component) =>
                component.type !== COMPONENT_TYPES.LLM &&
                component.type !== COMPONENT_TYPES.RERANKER &&
                component.providers.length > 0,
            ) || [];

          // Identify Step 2 inference components (llm and reranker)
          const inferenceComponents =
            deployData.components?.filter(
              (component) =>
                (component.type === COMPONENT_TYPES.LLM ||
                  component.type === COMPONENT_TYPES.RERANKER) &&
                component.providers.length > 0,
            ) || [];

          // STAGE 1: Fetch Step 1 component models in parallel (Promise.allSettled so a single failure does not block the rest).
          const step1Results = await Promise.allSettled(
            step1Components.map(async (component) => {
              setComponentModelsLoading(serviceId, component.type, true);
              const models = await fetchComponentModelsWithSchemas(
                serviceId,
                component.type,
                setProviderSchema,
                deployData,
              );
              setComponentModels(serviceId, component.type, models);
              return { type: component.type, models };
            }),
          );

          step1Results.forEach((result, index) => {
            if (result.status === "rejected") {
              const component = step1Components[index];
              const errorMessage =
                result.reason instanceof Error
                  ? result.reason.message
                  : `Failed to load ${component.type} models`;
              setComponentModelsError(serviceId, component.type, errorMessage);
            }
          });

          // STAGE 2: Fetch LLM and reranker models in background (for Step 2).
          inferenceComponents.forEach((component) => {
            const fetchFn =
              component.type === COMPONENT_TYPES.LLM
                ? fetchLLMOptionsWithModels(
                    serviceId,
                    setProviderSchema,
                    deployData,
                  )
                : fetchComponentModelsWithSchemas(
                    serviceId,
                    component.type,
                    setProviderSchema,
                    deployData,
                  );

            fetchFn
              .then((models) => {
                setComponentModels(serviceId, component.type, models);
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
              });
          });
        })
        .catch((err) => {
          const errorMessage =
            err instanceof Error
              ? err.message
              : "Failed to load deploy options";
          setServiceDeployOptionsError(serviceId, errorMessage);
          setComponentModelsError(serviceId, COMPONENT_TYPES.LLM, errorMessage);
        })
        .finally(() => {
          hasFetchedOptions.current[serviceId] = false;
        });

      return;
    }

    // --- Path B: deploy options cached — retry only errored models on reopen ---
    if (!deployOptions) return;

    const llmError = storeState.componentModelsError[`${serviceId}:llm`];
    if (llmError) {
      setComponentModelsError(serviceId, "llm", null);
      setComponentModelsLoading(serviceId, "llm", true);
      fetchLLMOptionsWithModels(serviceId, setProviderSchema, deployOptions)
        .then((llmData) => setComponentModels(serviceId, "llm", llmData))
        .catch((err) => {
          setComponentModelsError(
            serviceId,
            "llm",
            err instanceof Error ? err.message : "Failed to load LLM models",
          );
        });
    }

    const step1Components =
      deployOptions.components?.filter(
        (c) => !["llm", "reranker"].includes(c.type) && c.providers.length > 0,
      ) ?? [];
    step1Components.forEach((component) => {
      const err =
        storeState.componentModelsError[`${serviceId}:${component.type}`];
      if (!err) return;
      setComponentModelsError(serviceId, component.type, null);
      setComponentModelsLoading(serviceId, component.type, true);
      fetchComponentModelsWithSchemas(
        serviceId,
        component.type,
        setProviderSchema,
        deployOptions,
      )
        .then((models) => setComponentModels(serviceId, component.type, models))
        .catch((retryErr) => {
          setComponentModelsError(
            serviceId,
            component.type,
            retryErr instanceof Error
              ? retryErr.message
              : `Failed to load ${component.type} models`,
          );
        });
    });
  }, [
    open,
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
    error: deployOptionsError,
    llmError: llmModelsError,
  };
};
