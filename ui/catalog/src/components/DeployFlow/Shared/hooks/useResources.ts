import { useState, useEffect } from "react";
import { fetchResources } from "@/api/applications.api";
import type { ResourcesResponse } from "@/types/api.types";
import { dedupe } from "@/utils/requestManager";
import { LOCAL_WORKER_NAME } from "@/constants";

interface UseResourcesResult {
  resources: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
}

// No caching, re-fetched on every mount intentionally — available resources reflect live cluster state.
// Pass workerName to scope the query to a specific remote worker's available capacity.
// "local" and undefined both query the local runtime (no ?worker= param sent).
export const useResources = (workerName?: string): UseResourcesResult => {
  const [resources, setResources] = useState<ResourcesResponse | null>(null);
  const [resourcesLoading, setResourcesLoading] = useState<boolean>(true);
  const [resourcesError, setResourcesError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    // Send ?worker= only for named remote workers; local/undefined uses the server default.
    const remoteWorker =
      workerName && workerName !== LOCAL_WORKER_NAME ? workerName : undefined;

    // Scope the dedupe key per worker so switching workers always fires a fresh fetch.
    dedupe(`fetchResources:${remoteWorker ?? "local"}`, () =>
      fetchResources(remoteWorker),
    )
      .then((data) => {
        if (!cancelled) {
          setResources(data);
          setResourcesLoading(false);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setResourcesError(
            err instanceof Error ? err.message : "Failed to load resources",
          );
          setResourcesLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workerName]);

  return { resources, resourcesLoading, resourcesError };
};
