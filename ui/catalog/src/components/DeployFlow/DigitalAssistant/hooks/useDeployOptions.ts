import { useEffect } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { fetchDeployOptions } from "@/api/applications.api";
import { dedupe } from "@/utils/requestManager";

export const useDeployOptions = () => {
  const selectedArchitectureId = useDeployStore(
    (s) => s.selectedArchitectureId,
  );
  const deployOptionsLoading = useDeployStore((s) => s.deployOptionsLoading);
  const deployOptionsError = useDeployStore((s) => s.deployOptionsError);
  const deployOptions = useDeployStore((s) =>
    selectedArchitectureId
      ? (s.deployOptions[selectedArchitectureId]?.data ?? null)
      : null,
  );

  const shouldBeLoading =
    !deployOptions && !deployOptionsError && !deployOptionsLoading;

  useEffect(() => {
    if (!selectedArchitectureId) return;

    const {
      isDeployOptionsStale,
      setDeployOptionsLoading,
      setDeployOptionsError,
    } = useDeployStore.getState();

    const isStale = isDeployOptionsStale(selectedArchitectureId);

    if ((!deployOptions || isStale) && !deployOptionsLoading) {
      setDeployOptionsLoading(true);
      setDeployOptionsError(null);

      dedupe(`deployOptions:${selectedArchitectureId}`, () =>
        fetchDeployOptions(selectedArchitectureId),
      )
        .then((data) => {
          useDeployStore
            .getState()
            .setDeployOptions(selectedArchitectureId, data);
        })
        .catch((err) => {
          useDeployStore
            .getState()
            .setDeployOptionsError(
              err instanceof Error
                ? err.message
                : "Failed to load deploy options",
            );
        });
    }
  }, [selectedArchitectureId, deployOptions, deployOptionsLoading]);

  return {
    deployOptions,
    isLoading: deployOptionsLoading || shouldBeLoading,
    error: deployOptionsError,
  };
};
