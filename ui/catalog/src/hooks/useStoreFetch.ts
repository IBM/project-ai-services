import { useCallback, useEffect } from "react";

interface UseStoreFetchOptions<T> {
  data: T[] | null;
  loading: boolean;
  error: string | null;
  setData: (data: T[]) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  // Reads loading from the store so refetch doesn't depend on it.
  getLoading: () => boolean;
  fetcher: () => Promise<T[]>;
  fallbackError: string;
  autoFetch?: boolean;
}

// Shared fetch-into-store logic for list hooks. Skips if a fetch is in flight.
export const useStoreFetch = <T>({
  data,
  loading,
  error,
  setData,
  setLoading,
  setError,
  getLoading,
  fetcher,
  fallbackError,
  autoFetch = true,
}: UseStoreFetchOptions<T>) => {
  const refetch = useCallback(async () => {
    if (getLoading()) return;

    setError(null);
    setLoading(true);

    try {
      setData(await fetcher());
    } catch (err) {
      setError(err instanceof Error ? err.message : fallbackError);
    }
  }, [getLoading, fetcher, fallbackError, setData, setLoading, setError]);

  useEffect(() => {
    if (!autoFetch) return;

    void refetch();
  }, [autoFetch, refetch]);

  return {
    data: data ?? [],
    isLoading: loading || (!data && !error),
    error,
    refetch,
  };
};
