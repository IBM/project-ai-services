import { useEffect } from "react";
import { useDeployStore } from "@/store/deploy.store";
import {
  fetchDeployOptions,
  fetchProviderSchema,
  fetchServiceParams,
} from "@/api/applications.api";
import { dedupe } from "@/utils/requestManager";

// Fetches deploy options and all component provider schemas eagerly on mount. Failed schemas are skipped,  reopening the tearsheet re-triggers them; StepOne warns if any are still missing.
export const useDeployOptions = () => {
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
    isProviderParamsStale,
    getServiceParams,
    setServiceParams,
    isServiceParamsStale,
  } = useDeployStore();

  // Get deploy options for the selected architecture
  const deployOptions = selectedArchitectureId
    ? getDeployOptions(selectedArchitectureId)
    : null;

  const shouldBeLoading =
    !!selectedArchitectureId &&
    !deployOptions &&
    !deployOptionsError &&
    !deployOptionsLoading;

  // Step 1 — fetch deploy options
  useEffect(() => {
    if (!selectedArchitectureId) return;

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
    selectedArchitectureId,
    deployOptions,
    deployOptionsLoading,
    isDeployOptionsStale,
    setDeployOptions,
    setDeployOptionsLoading,
    setDeployOptionsError,
  ]);

  // Step 2 — eagerly fetch all provider schemas once deploy options land
  useEffect(() => {
    if (!deployOptions) return;

    const seen = new Set<string>();
    const pairs: Array<{ componentType: string; providerId: string }> = [];

    const visit = (componentType: string, providerId: string) => {
      const key = `${componentType}:${providerId}`;
      if (!seen.has(key)) {
        seen.add(key);
        pairs.push({ componentType, providerId });
      }
    };

    deployOptions.global_components.forEach((c) =>
      c.providers.forEach((p) => visit(c.type, p.id)),
    );
    deployOptions.services.forEach((s) =>
      s.components.forEach((c) =>
        c.providers.forEach((p) => visit(c.type, p.id)),
      ),
    );

    const pairsToFetch = pairs.filter(({ componentType, providerId }) => {
      const cached = getProviderParams(componentType, providerId);
      return !cached || isProviderParamsStale(componentType, providerId);
    });

    // Collect service IDs that need their service-level schema fetched
    const serviceIds = deployOptions.services.map((s) => s.id);
    const serviceIdsToFetch = serviceIds.filter((serviceId) => {
      const cached = getServiceParams(serviceId);
      return !cached || isServiceParamsStale(serviceId);
    });

    if (pairsToFetch.length === 0 && serviceIdsToFetch.length === 0) return;

    Promise.allSettled([
      ...pairsToFetch.map(({ componentType, providerId }) =>
        dedupe(`providerParams:${componentType}:${providerId}`, () =>
          fetchProviderSchema(componentType, providerId),
        ).then((schema) => {
          setProviderParams(componentType, providerId, schema);
        }),
      ),
      ...serviceIdsToFetch.map((serviceId) =>
        dedupe(`serviceParams:${serviceId}`, () =>
          fetchServiceParams(serviceId),
        ).then((data) => {
          setServiceParams(serviceId, data);
        }),
      ),
    ]);
  }, [
    deployOptions,
    getProviderParams,
    setProviderParams,
    isProviderParamsStale,
    getServiceParams,
    setServiceParams,
    isServiceParamsStale,
  ]);

  return {
    deployOptions,
    isLoading: deployOptionsLoading || shouldBeLoading,
    error: deployOptionsError,
  };
};
