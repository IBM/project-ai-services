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
export const useResources = (): UseResourcesResult => {
  const [resources, setResources] = useState<ResourcesResponse | null>(null);
  const [resourcesLoading, setResourcesLoading] = useState<boolean>(true);
  const [resourcesError, setResourcesError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    dedupe("fetchResources", () => fetchResources())
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
  }, []);

  return { resources, resourcesLoading, resourcesError };
};
