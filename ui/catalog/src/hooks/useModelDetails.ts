import { useEffect, useState } from "react";
import {
  fetchCatalogModelDetails,
  fetchModelDeployment,
} from "@/api/models.api";
import type { ModelDeploymentDetail } from "@/types/api.types";

interface ModelDetailsState {
  id: string;
  detail: ModelDeploymentDetail | null;
  certifiedBy: string | null;
  error: string | null;
}

export const useModelDetails = (id: string) => {
  // State is tagged with the id it was loaded for, so data from a previous model is never returned
  const [state, setState] = useState<ModelDetailsState | null>(null);

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const data = await fetchModelDeployment(id);
        if (cancelled) return;
        setState({ id, detail: data, certifiedBy: null, error: null });

        try {
          const catalogModel = await fetchCatalogModelDetails(data.model);
          if (!cancelled) {
            setState((prev) =>
              prev?.id === id
                ? { ...prev, certifiedBy: catalogModel.certified_by || null }
                : prev,
            );
          }
        } catch {
          // The certified badge is optional; the page works without it
        }
      } catch (err) {
        if (cancelled) return;
        setState({
          id,
          detail: null,
          certifiedBy: null,
          error:
            err instanceof Error ? err.message : "Failed to load model details",
        });
      }
    };

    load();

    return () => {
      cancelled = true;
    };
  }, [id]);

  const current = state?.id === id ? state : null;

  return {
    detail: current?.detail ?? null,
    certifiedBy: current?.certifiedBy ?? null,
    isLoading: current === null,
    error: current?.error ?? null,
  };
};
