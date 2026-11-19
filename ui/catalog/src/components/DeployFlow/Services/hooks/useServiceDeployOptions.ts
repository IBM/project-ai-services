import { useEffect, useRef } from "react";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import {
  fetchServiceDeployOptions,
  fetchLLMOptionsWithModels,
  fetchComponentModelsWithSchemas,
  fetchServiceSchemaParams,
} from "@/api/applications.api";
import { COMPONENT_TYPES } from "@/constants";
import type { ProviderSchema, ServiceDeployOptions } from "@/types/api.types";

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
    getServiceSchema,
    setServiceSchema,
    setServiceSchemaLoading,
    setServiceSchemaError,
  } = useServiceDeployStore();

  // Keyed by "serviceId:runtime" to prevent stale cache hits across runtime switches
  const hasFetchedOptions = useRef<Record<string, boolean>>({});

  // Get cached data for this service+runtime
  const deployOptions = serviceId
    ? getServiceDeployOptions(serviceId, runtime)
    : null;
  const serviceSchema = serviceId ? getServiceSchema(serviceId, runtime) : null;
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
  const serviceSchemaLoading = useServiceDeployStore((state) =>
    serviceId
      ? state.serviceSchemasLoading[`${serviceId}:${runtime}`] || false
      : false,
  );
  const serviceSchemaError = useServiceDeployStore((state) =>
    serviceId
      ? state.serviceSchemasError[`${serviceId}:${runtime}`] || null
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

    // Loads the model list for one component type and records loading/error state.
    // LLM has its own fetcher (it falls back to provider entries when a schema has
    // no model); every other type — embedding, vector store, reranker, custom —
    // uses the generic schema-driven fetcher.
    const loadComponentModels = (
      options: ServiceDeployOptions,
      componentType: string,
    ) => {
      setComponentModelsError(serviceId, componentType, runtime, null);
      setComponentModelsLoading(serviceId, componentType, runtime, true);

      const onSchema = (
        svcId: string,
        compType: string,
        providerId: string,
        schema: ProviderSchema,
      ) => setProviderSchema(svcId, compType, providerId, runtime, schema);

      const request =
        componentType === COMPONENT_TYPES.LLM
          ? fetchLLMOptionsWithModels(serviceId, onSchema, options, runtime)
          : fetchComponentModelsWithSchemas(
              serviceId,
              componentType,
              onSchema,
              options,
              runtime,
            );

      return request
        .then((models) =>
          setComponentModels(serviceId, componentType, runtime, models),
        )
        .catch((err) =>
          setComponentModelsError(
            serviceId,
            componentType,
            runtime,
            err instanceof Error
              ? err.message
              : `Failed to load ${componentType} models`,
          ),
        );
    };

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
        .then((deployData) => {
          setServiceDeployOptions(serviceId, runtime, deployData);

          const hasComponents = (deployData.components?.length ?? 0) > 0;

          // Fetch service-level schema when the deploy options declare a schema URL.
          // This is non-blocking — form initialization is guarded in ServicesDeployFlow.
          if (deployData.schema) {
            setServiceSchemaLoading(serviceId, runtime, true);
            fetchServiceSchemaParams(deployData.schema)
              .then((schema) => setServiceSchema(serviceId, runtime, schema))
              .catch((err) => {
                const msg =
                  err instanceof Error
                    ? err.message
                    : "Failed to load service schema";
                setServiceSchemaError(serviceId, runtime, msg);
              });
          }

          // Short-circuit if no components (Scenario B / D) — skip all model fetches.
          if (!hasComponents) return;

          // Fetch models for every component that has providers, in parallel.
          // Each component tracks its own loading/error state, so one failure
          // does not block the others.
          deployData.components
            .filter((component) => component.providers.length > 0)
            .forEach((component) => {
              void loadComponentModels(deployData, component.type);
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

    // Fetch schema if deployOptions declares a schema URL but no schema is cached yet,
    // or retry if a previous fetch errored (e.g. on rehydrate from persisted storage or after network error).
    const cachedSchema = getServiceSchema(serviceId, runtime);
    const schemaLoading =
      storeState.serviceSchemasLoading[`${serviceId}:${runtime}`];
    if (deployOptions.schema && !cachedSchema && !schemaLoading) {
      setServiceSchemaError(serviceId, runtime, null);
      setServiceSchemaLoading(serviceId, runtime, true);
      fetchServiceSchemaParams(deployOptions.schema)
        .then((schema) => setServiceSchema(serviceId, runtime, schema))
        .catch((err) => {
          const msg =
            err instanceof Error
              ? err.message
              : "Failed to load service schema";
          setServiceSchemaError(serviceId, runtime, msg);
        });
    }

    // Retry every component whose previous model fetch errored.
    deployOptions.components
      ?.filter((component) => component.providers.length > 0)
      .forEach((component) => {
        if (
          storeState.componentModelsError[
            `${serviceId}:${component.type}:${runtime}`
          ]
        ) {
          void loadComponentModels(deployOptions, component.type);
        }
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
    getServiceSchema,
    setServiceSchema,
    setServiceSchemaLoading,
    setServiceSchemaError,
  ]);

  return {
    deployOptions,
    serviceSchema,
    serviceSchemaError,
    llmModels,
    isLoading:
      deployOptionsLoading ||
      llmModelsLoading ||
      shouldBeLoading ||
      serviceSchemaLoading,
    error: deployOptionsError,
    llmError: llmModelsError,
  };
};
