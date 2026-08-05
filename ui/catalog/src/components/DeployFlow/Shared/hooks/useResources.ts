import { useState, useEffect } from "react";
import { fetchResources } from "@/api/applications.api";
import { dedupe } from "@/utils/requestManager";
import type { ResourcesResponse } from "@/types/api.types";

interface UseResourcesResult {
  resources: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
}

export const useResources = (): UseResourcesResult => {
  const [resources, setResources] = useState<ResourcesResponse | null>(null);
  const [resourcesLoading, setResourcesLoading] = useState<boolean>(true);
  const [resourcesError, setResourcesError] = useState<string | null>(null);

  useEffect(() => {
    dedupe("fetchResources", fetchResources)
      .then((data) => {
        setResources(data);
        setResourcesLoading(false);
      })
      .catch((err) => {
        setResourcesError(
          err instanceof Error ? err.message : "Failed to load resources",
        );
        setResourcesLoading(false);
      });
  }, []);

  return { resources, resourcesLoading, resourcesError };
};
