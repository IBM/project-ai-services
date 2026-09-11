import { useState, useEffect, useRef, useCallback } from "react";
import { fetchAllWorkerResources } from "@/api/workerResources.api";
import { dedupe } from "@/utils/requestManager";
import type { WorkerApiResponse } from "@/types/api.types";

interface WorkersState {
  workers: WorkerApiResponse[];
  isLoading: boolean;
  error: string | null;
}

export const useWorkers = (enabled: boolean = true) => {
  const [workersState, setWorkersState] = useState<WorkersState>({
    workers: [],
    isLoading: enabled,
    error: null,
  });

  // Triggers the fetch effect for explicit refetches.
  const [refetchTrigger, setRefetchTrigger] = useState(0);
  const hasFetched = useRef(false);

  useEffect(() => {
    if (!enabled) {
      hasFetched.current = false;
      return;
    }

    // Prevent duplicate automatic fetches while allowing explicit refetches.
    if (refetchTrigger === 0 && hasFetched.current) return;

    hasFetched.current = true;

    void dedupe("workerResources", fetchAllWorkerResources)
      .then((workers) =>
        setWorkersState({
          workers,
          isLoading: false,
          error: null,
        }),
      )
      .catch((err) =>
        setWorkersState((prev) => ({
          ...prev,
          isLoading: false,
          error: err instanceof Error ? err.message : "Failed to load workers",
        })),
      );
  }, [enabled, refetchTrigger]);

  const refetch = useCallback(() => {
    if (!enabled) return;

    setWorkersState((prev) => ({
      ...prev,
      isLoading: true,
      error: null,
    }));

    setRefetchTrigger((count) => count + 1);
  }, [enabled]);

  return { ...workersState, refetch };
};
