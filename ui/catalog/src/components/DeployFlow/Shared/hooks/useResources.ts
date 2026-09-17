import { useState, useEffect } from "react";
import { fetchResources } from "@/api/applications.api";
import type { ResourcesResponse } from "@/types/api.types";
import { dedupe } from "@/utils/requestManager";

interface UseResourcesResult {
  resources: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
}

// No caching, re-fetched on every mount intentionally — available resources reflect live cluster state.
// Pass workerName to scope the query to a specific worker's available capacity.
// undefined queries the local runtime (no ?worker= param sent).
export const useResources = (workerName?: string): UseResourcesResult => {
  const [resources, setResources] = useState<ResourcesResponse | null>(null);
  const [resourcesLoading, setResourcesLoading] = useState<boolean>(true);
  const [resourcesError, setResourcesError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    // Scope the dedupe key per worker so switching workers always fires a fresh fetch.
    dedupe(`fetchResources:${workerName ?? "local"}`, () =>
      fetchResources(workerName),
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
