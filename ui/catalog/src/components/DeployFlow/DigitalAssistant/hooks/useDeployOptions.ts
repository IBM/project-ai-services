import { useEffect, useRef, useState } from "react";
import { useDeployStore } from "@/store/deploy.store";
import type { LLMOption, ProviderSchema } from "@/types/api.types";
import {
  fetchDeployOptions,
  fetchProviderSchema,
  fetchServiceParams,
} from "@/api/applications.api";
import { dedupe } from "@/utils/requestManager";
import { WORKER_RUNTIME_LABELS } from "@/constants";

/** Extracts LLMOption entries for a single provider from its resolved schema. */
function extractModelsFromSchema(
  schema: ProviderSchema,
  providerId: string,
  providerName: string,
): LLMOption[] {
  const properties = schema.properties as Record<
    string,
    { oneOf?: Array<{ const: string; title?: string }>; default?: string }
  >;
  if (properties.model?.oneOf) {
    return properties.model.oneOf.map((opt) => ({
      id: opt.const,
      text: opt.title || opt.const,
      providerId,
      providerName,
    }));
  }
  if (properties.model?.default) {
    return [
      {
        id: properties.model.default,
        text: properties.model.default,
        providerId,
        providerName,
      },
    ];
  }
  return [];
}

// Fetches deploy options and all component provider schemas eagerly on mount for both runtimes.
// Failed schemas are skipped; reopening the tearsheet re-triggers them.
// StepOne warns if any are still missing.
export const useDeployOptions = (open: boolean, runtime: string) => {
  const {
    selectedArchitectureId,
    getDeployOptions,
    deployOptionsLoading,
    deployOptionsError,
    isDeployOptionsStale,
    setDeployOptions,
    setDeployOptionsLoading,
    setDeployOptionsError,
    getProviderParams,
    setProviderParams,
    setProviderParamsError,
    clearProviderParamsError,
    isProviderParamsStale,
    getServiceParams,
    setServiceParams,
    setServiceParamsError,
    clearServiceParamsError,
    isServiceParamsStale,
    setGlobalComponentModels,
  } = useDeployStore();

  // Tracks keys currently in-flight so StepOne can distinguish loading from failed.
  const inFlightKeys = useRef<Set<string>>(new Set());
  const [inflightCount, setInflightCount] = useState(0);

  // Deploy options for the active runtime
  const deployOptions = selectedArchitectureId
    ? getDeployOptions(selectedArchitectureId, runtime)
    : null;

  const shouldBeLoading =
    !!selectedArchitectureId &&
    !deployOptions &&
    !deployOptionsError &&
    !deployOptionsLoading;

  // Step 1 — fetch deploy options for BOTH runtimes eagerly so switching tiles is instant.
  useEffect(() => {
    if (!open || !selectedArchitectureId) return;

    Object.keys(WORKER_RUNTIME_LABELS).forEach((rt) => {
      const isStale = isDeployOptionsStale(selectedArchitectureId, rt);
      const cached = getDeployOptions(selectedArchitectureId, rt);

      if ((!cached || isStale) && !deployOptionsLoading) {
        setDeployOptionsLoading(true);
        setDeployOptionsError(null);

        const requestKey = `deployOptions:${selectedArchitectureId}:${rt}`;
        dedupe(requestKey, () => fetchDeployOptions(selectedArchitectureId, rt))
          .then((data) => {
            useDeployStore
              .getState()
              .setDeployOptions(selectedArchitectureId, rt, data);
          })
          .catch((err) => {
            // Only surface the error for the active runtime
            if (rt === runtime) {
              setDeployOptionsError(
                err instanceof Error
                  ? err.message
                  : "Failed to load deploy options",
              );
            }
          });
      }
    });
  }, [
    open,
    selectedArchitectureId,
    runtime,
    deployOptionsLoading,
    isDeployOptionsStale,
    getDeployOptions,
    setDeployOptions,
    setDeployOptionsLoading,
    setDeployOptionsError,
  ]);

  // Step 2 — eagerly fetch all provider schemas once deploy options land for the active runtime.
  // In-flight tracking is scoped to global-component providers only — those are the schemas
  // StepOne reads. Service-component and service-level schemas are fetched in the background.
  useEffect(() => {
    if (!open || !deployOptions) return;

    const seen = new Set<string>();
    const globalPairs: Array<{ componentType: string; providerId: string }> =
      [];
    const backgroundPairs: Array<{
      componentType: string;
      providerId: string;
    }> = [];

    const visitGlobal = (componentType: string, providerId: string) => {
      const key = `${componentType}:${providerId}`;
      if (!seen.has(key)) {
        seen.add(key);
        globalPairs.push({ componentType, providerId });
      }
    };

    const visitBackground = (componentType: string, providerId: string) => {
      const key = `${componentType}:${providerId}`;
      if (!seen.has(key)) {
        seen.add(key);
        backgroundPairs.push({ componentType, providerId });
      }
    };

    deployOptions.global_components.forEach((c) =>
      c.providers.forEach((p) => visitGlobal(c.type, p.id)),
    );
    deployOptions.services.forEach((s) =>
      s.components.forEach((c) =>
        c.providers.forEach((p) => visitBackground(c.type, p.id)),
      ),
    );

    const globalPairsToFetch = globalPairs.filter(
      ({ componentType, providerId }) => {
        const cached = getProviderParams(componentType, providerId, runtime);
        const hasError =
          !!useDeployStore.getState().providerParamsError[
            `${runtime}:${componentType}:${providerId}`
          ];
        return (
          !cached ||
          isProviderParamsStale(componentType, providerId, runtime) ||
          hasError
        );
      },
    );

    const backgroundPairsToFetch = backgroundPairs.filter(
      ({ componentType, providerId }) => {
        const cached = getProviderParams(componentType, providerId, runtime);
        const hasError =
          !!useDeployStore.getState().providerParamsError[
            `${runtime}:${componentType}:${providerId}`
          ];
        return (
          !cached ||
          isProviderParamsStale(componentType, providerId, runtime) ||
          hasError
        );
      },
    );

    // Collect service IDs that need their service-level schema fetched
    const serviceIds = deployOptions.services.map((s) => s.id);
    const serviceIdsToFetch = serviceIds.filter((serviceId) => {
      const cached = getServiceParams(serviceId, runtime);
      const hasError =
        !!useDeployStore.getState().serviceParamsError[
          `${runtime}:${serviceId}`
        ];
      return !cached || isServiceParamsStale(serviceId, runtime) || hasError;
    });

    // No fetches needed — build model lists from cache; no .finally callbacks will fire.
    if (globalPairsToFetch.length === 0) {
      const modelsByType: Record<string, LLMOption[]> = {};
      deployOptions.global_components.forEach((component) => {
        modelsByType[component.type] = [];
        component.providers.forEach((provider) => {
          const cached = getProviderParams(
            component.type,
            provider.id,
            runtime,
          );
          if (!cached) return;
          modelsByType[component.type].push(
            ...extractModelsFromSchema(cached, provider.id, provider.name),
          );
        });
      });
      Object.entries(modelsByType).forEach(([ct, models]) => {
        setGlobalComponentModels(ct, runtime, models);
      });
    }

    if (
      globalPairsToFetch.length === 0 &&
      backgroundPairsToFetch.length === 0 &&
      serviceIdsToFetch.length === 0
    )
      return;

    const markDone = (key: string) => {
      inFlightKeys.current.delete(key);
      setInflightCount(inFlightKeys.current.size);
    };

    // Only global-component pairs enter the in-flight set so isProviderParamsLoading
    // reflects only what StepOne needs, not the full schema set.
    globalPairsToFetch.forEach(({ componentType, providerId }) => {
      inFlightKeys.current.add(`${componentType}:${providerId}`);
    });
    setInflightCount(inFlightKeys.current.size);

    // Collects resolved global-component schemas to build model lists once all settle.
    const globalSchemaResults: Array<{
      componentType: string;
      providerId: string;
      providerName: string;
      schema: ProviderSchema | null;
    }> = [];

    void Promise.allSettled([
      ...globalPairsToFetch.map(({ componentType, providerId }) => {
        const key = `${componentType}:${providerId}`;
        const provider = deployOptions.global_components
          .find((c) => c.type === componentType)
          ?.providers.find((p) => p.id === providerId);
        // Clear any stale error before retrying so the banner doesn't flash while in-flight
        clearProviderParamsError(componentType, providerId, runtime);
        return dedupe(`providerParams:${runtime}:${key}`, () =>
          fetchProviderSchema(componentType, providerId, runtime),
        )
          .then((schema) => {
            setProviderParams(componentType, providerId, runtime, schema);
            globalSchemaResults.push({
              componentType,
              providerId,
              providerName: provider?.name ?? providerId,
              schema,
            });
          })
          .catch((err) => {
            setProviderParamsError(
              componentType,
              providerId,
              runtime,
              err instanceof Error ? err.message : "Failed to load schema",
            );
            globalSchemaResults.push({
              componentType,
              providerId,
              providerName: provider?.name ?? providerId,
              schema: null,
            });
          })
          .finally(() => {
            markDone(key);
            // Once all global schemas have settled, build model lists per component type.
            if (inFlightKeys.current.size === 0) {
              const modelsByType: Record<string, LLMOption[]> = {};
              globalSchemaResults.forEach(
                ({
                  componentType: ct,
                  providerId: pid,
                  providerName,
                  schema,
                }) => {
                  if (!modelsByType[ct]) modelsByType[ct] = [];
                  if (!schema) return;
                  modelsByType[ct].push(
                    ...extractModelsFromSchema(schema, pid, providerName),
                  );
                },
              );
              Object.entries(modelsByType).forEach(([ct, models]) => {
                setGlobalComponentModels(ct, runtime, models);
              });
            }
          });
      }),
      ...backgroundPairsToFetch.map(({ componentType, providerId }) => {
        const key = `${componentType}:${providerId}`;
        clearProviderParamsError(componentType, providerId, runtime);
        return dedupe(`providerParams:${runtime}:${key}`, () =>
          fetchProviderSchema(componentType, providerId, runtime),
        )
          .then((schema) => {
            setProviderParams(componentType, providerId, runtime, schema);
          })
          .catch((err) => {
            setProviderParamsError(
              componentType,
              providerId,
              runtime,
              err instanceof Error ? err.message : "Failed to load schema",
            );
          });
      }),
      ...serviceIdsToFetch.map((serviceId) => {
        clearServiceParamsError(serviceId, runtime);
        return dedupe(`serviceParams:${runtime}:${serviceId}`, () =>
          fetchServiceParams(serviceId, runtime),
        )
          .then((data) => {
            setServiceParams(serviceId, runtime, data);
          })
          .catch((err) => {
            setServiceParamsError(
              serviceId,
              runtime,
              err instanceof Error
                ? err.message
                : "Failed to load service schema",
            );
          });
      }),
    ]);
  }, [
    open,
    runtime,
    deployOptions,
    getProviderParams,
    setProviderParams,
    setProviderParamsError,
    clearProviderParamsError,
    isProviderParamsStale,
    getServiceParams,
    setServiceParams,
    setServiceParamsError,
    clearServiceParamsError,
    isServiceParamsStale,
    setGlobalComponentModels,
  ]);

  const isProviderParamsLoading = inflightCount > 0;

  return {
    deployOptions,
    isLoading: deployOptionsLoading || shouldBeLoading,
    isProviderParamsLoading,
    error: deployOptionsError,
  };
};
