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
 * Uses Zustand store to cache data per service+runtime and avoid redundant API calls.
 * On reopen, retries only errored models.
 */
export const useServiceDeployOptions = (
  serviceId: string | null,
  open: boolean,
  runtime: string,
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

  // Keyed by "serviceId:runtime" to prevent stale cache hits across runtime switches
  const hasFetchedOptions = useRef<Record<string, boolean>>({});

  // Get cached data for this service+runtime
  const deployOptions = serviceId
    ? getServiceDeployOptions(serviceId, runtime)
    : null;
  const llmModels = serviceId
    ? getComponentModels(serviceId, COMPONENT_TYPES.LLM, runtime)
    : [];

  // Get loading and error states from store
  const deployOptionsLoading = useServiceDeployStore((state) =>
    serviceId
      ? state.serviceDeployOptionsLoading[`${serviceId}:${runtime}`] || false
      : false,
  );
  const deployOptionsError = useServiceDeployStore((state) =>
    serviceId
      ? state.serviceDeployOptionsError[`${serviceId}:${runtime}`] || null
      : null,
  );
  const llmModelsLoading = useServiceDeployStore((state) =>
    serviceId
      ? state.componentModelsLoading[
          `${serviceId}:${COMPONENT_TYPES.LLM}:${runtime}`
        ] || false
      : false,
  );
  const llmModelsError = useServiceDeployStore((state) =>
    serviceId
      ? state.componentModelsError[
          `${serviceId}:${COMPONENT_TYPES.LLM}:${runtime}`
        ] || null
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
    const fetchKey = `${serviceId}:${runtime}`;

    // --- Path A: deploy options not cached yet — full fetch ---
    if (
      !deployOptions &&
      !hasFetchedOptions.current[fetchKey] &&
      !deployOptionsLoading
    ) {
      hasFetchedOptions.current[fetchKey] = true;
      setServiceDeployOptionsLoading(serviceId, runtime, true);
      setComponentModelsLoading(serviceId, COMPONENT_TYPES.LLM, runtime, true);
      setServiceDeployOptionsError(serviceId, runtime, null);
      setComponentModelsError(serviceId, COMPONENT_TYPES.LLM, runtime, null);

      // First, fetch deploy options to know which components exist
      fetchServiceDeployOptions(serviceId, runtime)
        .then(async (deployData) => {
          setServiceDeployOptions(serviceId, runtime, deployData);

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

          // STAGE 1: Fetch Step 1 component models in parallel (Promise.allSettled so a
          // single failure does not block the rest).
          const step1Results = await Promise.allSettled(
            step1Components.map(async (component) => {
              setComponentModelsLoading(
                serviceId,
                component.type,
                runtime,
                true,
              );
              const models = await fetchComponentModelsWithSchemas(
                serviceId,
                component.type,
                (svcId, compType, providerId, schema) =>
                  setProviderSchema(
                    svcId,
                    compType,
                    providerId,
                    runtime,
                    schema,
                  ),
                deployData,
                runtime,
              );
              setComponentModels(serviceId, component.type, runtime, models);
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
              setComponentModelsError(
                serviceId,
                component.type,
                runtime,
                errorMessage,
              );
            }
          });

          // STAGE 2: Fetch LLM and reranker models in background (for Step 2).
          inferenceComponents.forEach((component) => {
            const fetchFn =
              component.type === COMPONENT_TYPES.LLM
                ? fetchLLMOptionsWithModels(
                    serviceId,
                    (svcId, compType, providerId, schema) =>
                      setProviderSchema(
                        svcId,
                        compType,
                        providerId,
                        runtime,
                        schema,
                      ),
                    deployData,
                    runtime,
                  )
                : fetchComponentModelsWithSchemas(
                    serviceId,
                    component.type,
                    (svcId, compType, providerId, schema) =>
                      setProviderSchema(
                        svcId,
                        compType,
                        providerId,
                        runtime,
                        schema,
                      ),
                    deployData,
                    runtime,
                  );

            fetchFn
              .then((models) => {
                setComponentModels(serviceId, component.type, runtime, models);
              })
              .catch((err) => {
                const errorMessage =
                  err instanceof Error
                    ? err.message
                    : `Failed to load ${component.type} models`;
                setComponentModelsError(
                  serviceId,
                  component.type,
                  runtime,
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
          setServiceDeployOptionsError(serviceId, runtime, errorMessage);
          setComponentModelsError(
            serviceId,
            COMPONENT_TYPES.LLM,
            runtime,
            errorMessage,
          );
        })
        .finally(() => {
          hasFetchedOptions.current[fetchKey] = false;
        });

      return;
    }

    // --- Path B: deploy options cached — retry only errored models on reopen ---
    if (!deployOptions) return;

    const llmError =
      storeState.componentModelsError[
        `${serviceId}:${COMPONENT_TYPES.LLM}:${runtime}`
      ];
    if (llmError) {
      setComponentModelsError(serviceId, COMPONENT_TYPES.LLM, runtime, null);
      setComponentModelsLoading(serviceId, COMPONENT_TYPES.LLM, runtime, true);
      fetchLLMOptionsWithModels(
        serviceId,
        (svcId, compType, providerId, schema) =>
          setProviderSchema(svcId, compType, providerId, runtime, schema),
        deployOptions,
        runtime,
      )
        .then((llmData) =>
          setComponentModels(serviceId, COMPONENT_TYPES.LLM, runtime, llmData),
        )
        .catch((err) => {
          setComponentModelsError(
            serviceId,
            COMPONENT_TYPES.LLM,
            runtime,
            err instanceof Error ? err.message : "Failed to load LLM models",
          );
        });
    }

    const step1Components =
      deployOptions.components?.filter(
        (c) =>
          c.type !== COMPONENT_TYPES.LLM &&
          c.type !== COMPONENT_TYPES.RERANKER &&
          c.providers.length > 0,
      ) ?? [];
    step1Components.forEach((component) => {
      const err =
        storeState.componentModelsError[
          `${serviceId}:${component.type}:${runtime}`
        ];
      if (!err) return;
      setComponentModelsError(serviceId, component.type, runtime, null);
      setComponentModelsLoading(serviceId, component.type, runtime, true);
      fetchComponentModelsWithSchemas(
        serviceId,
        component.type,
        (svcId, compType, providerId, schema) =>
          setProviderSchema(svcId, compType, providerId, runtime, schema),
        deployOptions,
        runtime,
      )
        .then((models) =>
          setComponentModels(serviceId, component.type, runtime, models),
        )
        .catch((retryErr) => {
          setComponentModelsError(
            serviceId,
            component.type,
            runtime,
            retryErr instanceof Error
              ? retryErr.message
              : `Failed to load ${component.type} models`,
          );
        });
    });
  }, [
    open,
    serviceId,
    runtime,
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
