import { useState, useEffect, useRef, useCallback } from "react";
import { fetchAllWorkerResources } from "@/api/workerResources.api";
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
  const hasFetched = useRef(false);
  const refetch = useCallback(() => {
    if (!enabled) return;
    setWorkersState((prev) => ({ ...prev, isLoading: true, error: null }));
    fetchAllWorkerResources()
      .then((workers) =>
        setWorkersState({ workers, isLoading: false, error: null }),
      )
      .catch((err) =>
        setWorkersState({
          workers: [],
          isLoading: false,
          error: err instanceof Error ? err.message : "Failed to load workers",
        }),
      );
  }, [enabled]);
  useEffect(() => {
    if (!enabled) {
      hasFetched.current = false;
      return;
    }
    if (hasFetched.current) return;
    hasFetched.current = true;
    fetchAllWorkerResources()
      .then((workers) =>
        setWorkersState({ workers, isLoading: false, error: null }),
      )
      .catch((err) =>
        setWorkersState({
          workers: [],
          isLoading: false,
          error: err instanceof Error ? err.message : "Failed to load workers",
        }),
      );
  }, [enabled]);
  return { ...workersState, refetch };
};
