import { useState, useEffect, useRef } from "react";
import { api } from "@/api/axios";
import {
  SERVICE_ENDPOINTS,
  DIGITAL_ASSISTANTS_ENDPOINTS,
} from "@/constants/api-endpoints.constants";
import { DEFAULT_RUNTIME } from "@/constants";
import { useDeployStore } from "@/store/deploy.store";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";

/**
 * Returns whether the "Data sources" tab should be shown for a deployment.
 * Checks the deploy store cache first (zero extra calls when the deploy flow
 * has already fetched the options). Falls back to a single API call on a miss.
 */
export interface DeploymentDatasourceSupportResult {
  acceptsDatasource: boolean | undefined;
  error: string | null;
  clearError: () => void;
}

export function useDeploymentDatasourceSupport(
  deploymentSource: string,
  catalogIds: string[] | undefined,
): DeploymentDatasourceSupportResult {
  const isDA = deploymentSource === "Digital assistants";
  const selectedArchitectureId = useDeployStore(
    (s) => s.selectedArchitectureId,
  );
  // Select via hook so React is aware of these dependencies, then hold in refs
  // so the main effect's dependency array doesn't re-fire on unrelated store updates.
  const getDeployOptions = useDeployStore((s) => s.getDeployOptions);
  const getServiceDeployOptions = useServiceDeployStore(
    (s) => s.getServiceDeployOptions,
  );
  const getDeployOptionsRef = useRef(getDeployOptions);
  const getServiceDeployOptionsRef = useRef(getServiceDeployOptions);
  useEffect(() => {
    getDeployOptionsRef.current = getDeployOptions;
  }, [getDeployOptions]);
  useEffect(() => {
    getServiceDeployOptionsRef.current = getServiceDeployOptions;
  }, [getServiceDeployOptions]);
  const [acceptsDatasource, setAcceptsDatasource] = useState<
    boolean | undefined
  >(undefined);
  const [error, setError] = useState<string | null>(null);

  // Prevents re-firing the API call on re-renders that change only function
  const fetchedKeyRef = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const set = (v: boolean) => {
      if (!cancelled) setAcceptsDatasource(v);
    };
    const setErr = (msg: string) => {
      if (!cancelled) setError(msg);
    };

    if (isDA) {
      if (!selectedArchitectureId) {
        set(false);
        return;
      }

      // Cache hit — zero API calls
      const cached = getDeployOptionsRef.current(
        selectedArchitectureId,
        DEFAULT_RUNTIME,
      );
      if (cached) {
        set(cached.services.some((s) => s.accepts_datasource === true));
        return;
      }
      // Guard against duplicate in-flight requests for the same key within a
      // single mount.
      const fetchKey = `da:${selectedArchitectureId}`;
      if (fetchedKeyRef.current === fetchKey) return;
      fetchedKeyRef.current = fetchKey;

      api
        .get(
          DIGITAL_ASSISTANTS_ENDPOINTS.DEPLOY_OPTIONS(selectedArchitectureId),
        )
        .then((r) =>
          set(
            r.data.services.some(
              (s: { accepts_datasource?: boolean }) =>
                s.accepts_datasource === true,
            ),
          ),
        )
        .catch((err) => {
          set(false);
          setErr(
            err?.response?.data?.error ||
              err?.response?.data?.message ||
              err?.message ||
              "Failed to fetch deployment options.",
          );
        });
    } else {
      // Services always have a single catalog entry.
      const catalogId = catalogIds?.[0];
      if (catalogId === undefined) return;

      // Cache hit — zero API calls
      const cached = getServiceDeployOptionsRef.current(
        catalogId,
        DEFAULT_RUNTIME,
      );
      if (cached) {
        set(cached.accepts_datasource === true);
        return;
      }

      // Guard against duplicate in-flight requests (see DA branch comment)
      const fetchKey = `svc:${catalogId}`;
      if (fetchedKeyRef.current === fetchKey) return;
      fetchedKeyRef.current = fetchKey;

      api
        .get(SERVICE_ENDPOINTS.GET_SERVICE_DEPLOY_OPTIONS(catalogId))
        .then((r) => set(r.data.accepts_datasource === true))
        .catch((err) => {
          set(false);
          setErr(
            err?.response?.data?.error ||
              err?.response?.data?.message ||
              err?.message ||
              "Failed to fetch service deployment options.",
          );
        });
    }

    return () => {
      cancelled = true;
      // Reset the in-flight guard so a re-mount
      fetchedKeyRef.current = null;
    };
  }, [isDA, selectedArchitectureId, catalogIds]);

  return {
    acceptsDatasource,
    error,
    clearError: () => setError(null),
  };
}
