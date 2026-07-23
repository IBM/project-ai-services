/**
 * useAutoRefresh
 *
 * Runs a fetch function on mount and sets up a 2-minute auto-refresh interval
 * while there is data to refresh. Optionally re-runs the fetch when an external
 * `refreshTrigger` number changes (used by Deployed Services to let a parent
 * component force a refresh).
 *
 * Edge cases handled:
 *   (1) Skip a tick while a fetch is already in-flight so slow responses
 *       don't stack or apply out of order.
 *   (2) Pause while delete is active so an interval refresh doesn't clobber
 *       the "Deleting…" row state or fire mid-confirmation. Pass
 *       `isPaused: state.isDeleteDialogOpen || state.isDeleting`.
 *
 * This hook owns only the *lifecycle* of fetching — when to call the fetch
 * function. It never owns the fetch function itself, which stays in each table
 * so that table-specific API calls, transforms, and dispatch sequences are
 * unchanged.
 *
 * Usage:
 *
 *   useAutoRefresh({
 *     fetchFn: fetchDeployedServices,
 *     hasData: state.rowsData.length > 0,
 *     isPaused: state.isDeleteDialogOpen || state.isDeleting,
 *     refreshTrigger,          // optional — omit for Digital Assistants
 *   });
 */

import { useEffect, useRef } from "react";

const AUTO_REFRESH_INTERVAL_MS = 120_000; // 2 minutes

interface UseAutoRefreshOptions {
  /** The fetch function to call. Must be stable (useCallback or similar). */
  fetchFn: () => void;
  /**
   * Whether there is currently data in the table. The auto-refresh interval
   * is only started when this is true, matching the behaviour in both tables
   * before this hook was introduced.
   */
  hasData: boolean;
  /**
   * When true the interval tick is skipped entirely. Pass
   * `state.isDeleteDialogOpen || state.isDeleting` so that a background
   * refresh never clobbers the optimistic "Deleting…" row state or fires
   * while the confirmation modal is open.
   */
  isPaused?: boolean;
  /**
   * Optional external trigger. When this number changes the fetch function
   * is called immediately regardless of the interval. Used by Deployed
   * Services where a parent component increments a counter to force refresh.
   * Digital Assistants omits this prop.
   */
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
  /** True while an interval-initiated fetch is still in-flight (edge case 1). */
  const isFetchingRef = useRef(false);
  /**
   * Ref mirror of isPaused so the interval callback always reads the latest
   * value without needing to restart the timer on every render (edge case 2).
   */
  const isPausedRef = useRef(isPaused);
  isPausedRef.current = isPaused;

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
      void Promise.resolve(fetchFn()).finally(() => {
        isFetchingRef.current = false;
      });
    }, AUTO_REFRESH_INTERVAL_MS);

    return () => clearInterval(intervalId);
    // fetchFn intentionally excluded — the interval reads it via the closure
    // captured at setup time; restarting the timer on every render would reset
    // the 2-minute countdown unnecessarily.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasData]);
}
