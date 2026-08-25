import { useEffect, useRef, useState } from "react";
import { useDeployStore } from "@/store/deploy.store";
import {
  fetchDeployOptions,
  fetchProviderSchema,
  fetchServiceParams,
} from "@/api/applications.api";
import { dedupe } from "@/utils/requestManager";

// Fetches deploy options and all component provider schemas eagerly on mount. Failed schemas are skipped, reopening the tearsheet re-triggers them; StepOne warns if any are still missing.
export const useDeployOptions = (open: boolean) => {
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
  } = useDeployStore();

  // Tracks keys currently in-flight so StepOne can distinguish loading from failed.
  const inFlightKeys = useRef<Set<string>>(new Set());
  const [inflightCount, setInflightCount] = useState(0);

  // Get deploy options for the selected architecture
  const deployOptions = selectedArchitectureId
    ? getDeployOptions(selectedArchitectureId)
    : null;

  const shouldBeLoading =
    !!selectedArchitectureId &&
    !deployOptions &&
    !deployOptionsError &&
    !deployOptionsLoading;

  // Step 1 — fetch deploy options; re-runs when the tearsheet reopens after an error.
  useEffect(() => {
    if (!open || !selectedArchitectureId) return;

    const isStale = isDeployOptionsStale(selectedArchitectureId);

    if ((!deployOptions || isStale) && !deployOptionsLoading) {
      setDeployOptionsLoading(true);
      setDeployOptionsError(null);

      const requestKey = `deployOptions:${selectedArchitectureId}`;
      dedupe(requestKey, () => fetchDeployOptions(selectedArchitectureId))
        .then((data) => {
          useDeployStore
            .getState()
            .setDeployOptions(selectedArchitectureId, data);
        })
        .catch((err) => {
          setDeployOptionsError(
            err instanceof Error
              ? err.message
              : "Failed to load deploy options",
          );
        });
    }
  }, [
    open,
    selectedArchitectureId,
    deployOptions,
    deployOptionsLoading,
    isDeployOptionsStale,
    setDeployOptions,
    setDeployOptionsLoading,
    setDeployOptionsError,
  ]);

  // Step 2 — eagerly fetch all provider schemas once deploy options land.
  // In-flight tracking (and therefore isProviderParamsLoading) is scoped to
  // global-component providers only — those are the schemas StepOne reads.
  // Service-component and service-level schemas are fetched in the background
  // without blocking the StepOne Next button.
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
        const cached = getProviderParams(componentType, providerId);
        const hasError =
          !!useDeployStore.getState().providerParamsError[
            `${componentType}:${providerId}`
          ];
        return (
          !cached ||
          isProviderParamsStale(componentType, providerId) ||
          hasError
        );
      },
    );

    const backgroundPairsToFetch = backgroundPairs.filter(
      ({ componentType, providerId }) => {
        const cached = getProviderParams(componentType, providerId);
        const hasError =
          !!useDeployStore.getState().providerParamsError[
            `${componentType}:${providerId}`
          ];
        return (
          !cached ||
          isProviderParamsStale(componentType, providerId) ||
          hasError
        );
      },
    );

    // Collect service IDs that need their service-level schema fetched
    const serviceIds = deployOptions.services.map((s) => s.id);
    const serviceIdsToFetch = serviceIds.filter((serviceId) => {
      const cached = getServiceParams(serviceId);
      const hasError =
        !!useDeployStore.getState().serviceParamsError[serviceId];
      return !cached || isServiceParamsStale(serviceId) || hasError;
    });

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

    void Promise.allSettled([
      ...globalPairsToFetch.map(({ componentType, providerId }) => {
        const key = `${componentType}:${providerId}`;
        // Clear any stale error before retrying so the banner doesn't flash while in-flight
        clearProviderParamsError(componentType, providerId);
        return dedupe(`providerParams:${key}`, () =>
          fetchProviderSchema(componentType, providerId),
        )
          .then((schema) => {
            setProviderParams(componentType, providerId, schema);
          })
          .catch((err) => {
            setProviderParamsError(
              componentType,
              providerId,
              err instanceof Error ? err.message : "Failed to load schema",
            );
          })
          .finally(() => markDone(key));
      }),
      ...backgroundPairsToFetch.map(({ componentType, providerId }) => {
        const key = `${componentType}:${providerId}`;
        clearProviderParamsError(componentType, providerId);
        return dedupe(`providerParams:${key}`, () =>
          fetchProviderSchema(componentType, providerId),
        )
          .then((schema) => {
            setProviderParams(componentType, providerId, schema);
          })
          .catch((err) => {
            setProviderParamsError(
              componentType,
              providerId,
              err instanceof Error ? err.message : "Failed to load schema",
            );
          });
      }),
      ...serviceIdsToFetch.map((serviceId) => {
        clearServiceParamsError(serviceId);
        return dedupe(`serviceParams:${serviceId}`, () =>
          fetchServiceParams(serviceId),
        )
          .then((data) => {
            setServiceParams(serviceId, data);
          })
          .catch((err) => {
            setServiceParamsError(
              serviceId,
              err instanceof Error
                ? err.message
                : "Failed to load service schema",
            );
          });
      }),
    ]);
  }, [
    open,
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
  ]);

  const isProviderParamsLoading = inflightCount > 0;

  return {
    deployOptions,
    isLoading: deployOptionsLoading || shouldBeLoading,
    isProviderParamsLoading,
    error: deployOptionsError,
  };
};
