import { useEffect, useRef } from "react";

const AUTO_REFRESH_INTERVAL_MS = 120_000; // 2 minutes

interface UseAutoRefreshOptions {
  fetchFn: () => void | Promise<void>;
  hasData: boolean;
  isPaused?: boolean;
  refreshTrigger?: number;
}

export function useAutoRefresh({
  fetchFn,
  hasData,
  isPaused,
  refreshTrigger,
}: UseAutoRefreshOptions): void {
  const isMountedRef = useRef(false);
  const prevFetchFnRef = useRef<(() => void) | null>(null);
  const prevRefreshTriggerRef = useRef(refreshTrigger);
  const isFetchingRef = useRef(false);
  const isPausedRef = useRef(isPaused);

  // Keep isPausedRef in sync after every render without touching it during render
  useEffect(() => {
    isPausedRef.current = isPaused;
  });

  // Mount fetch + fetchFn-change re-fetch + refreshTrigger-driven re-fetch
  useEffect(() => {
    // First run (mount): always fetch
    if (!isMountedRef.current) {
      isMountedRef.current = true;
      prevFetchFnRef.current = fetchFn;
      fetchFn();
      return;
    }

    // fetchFn reference changed (e.g. catalogId became available and
    // useCallback produced a new function) — re-fetch with the new fn
    if (prevFetchFnRef.current !== fetchFn) {
      prevFetchFnRef.current = fetchFn;
      fetchFn();
      return;
    }

    // Subsequent runs driven by refreshTrigger change only
    if (
      refreshTrigger !== undefined &&
      refreshTrigger !== prevRefreshTriggerRef.current
    ) {
      prevRefreshTriggerRef.current = refreshTrigger;
      fetchFn();
    }
  }, [fetchFn, refreshTrigger]);

  // 2-minute auto-refresh interval — only active while there is data.
  // Each tick is skipped when paused (delete modal open / deletion in-flight)
  // or when a previous interval-fetch is still running.
  useEffect(() => {
    if (!hasData) return;

    const intervalId = setInterval(() => {
      // Skip tick: delete flow is active or a previous fetch hasn't finished
      if (isPausedRef.current || isFetchingRef.current) return;

      isFetchingRef.current = true;
      void Promise.resolve(prevFetchFnRef.current?.()).finally(() => {
        isFetchingRef.current = false;
      });
    }, AUTO_REFRESH_INTERVAL_MS);

    return () => clearInterval(intervalId);
  }, [hasData]);
}
