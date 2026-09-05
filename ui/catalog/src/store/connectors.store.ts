import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { ConnectorType, ConnectorParamsSchema } from "@/types/api.types";

// Bump this when ConnectorType or ConnectorParamsSchema shape changes.
const CACHE_VERSION = "1.0.0";

// Cache durations — connector catalog data is static config, same as deploy params.
const CONNECTOR_TYPES_CACHE_DURATION = 30 * 60 * 1000; // 30 minutes
const PARAMS_CACHE_DURATION = 60 * 60 * 1000; // 1 hour

interface ParamsCache {
  data: ConnectorParamsSchema;
  fetchedAt: number;
}

interface ConnectorsState {
  // Cache version for invalidating stale schemas
  cacheVersion: string;

  // Connector types — the source type dropdown options
  connectorTypes: ConnectorType[];
  connectorTypesLoading: boolean;
  connectorTypesError: string | null;
  connectorTypesFetchedAt: number | null;

  // Params schema cache — keyed by provider id (e.g. "file_system")
  paramsCache: Record<string, ParamsCache>;
  paramsCacheLoading: Record<string, boolean>;
  paramsCacheError: Record<string, string | null>;

  // Connector types actions
  setConnectorTypes: (data: ConnectorType[]) => void;
  setConnectorTypesLoading: (loading: boolean) => void;
  setConnectorTypesError: (error: string | null) => void;
  clearConnectorTypes: () => void;

  // Params cache actions
  setParams: (providerId: string, data: ConnectorParamsSchema) => void;
  setParamsLoading: (providerId: string, loading: boolean) => void;
  setParamsError: (providerId: string, error: string | null) => void;
  clearParams: () => void;

  // Selectors
  getParams: (providerId: string) => ConnectorParamsSchema | null;
  isParamsLoading: (providerId: string) => boolean;

  // Staleness checks
  isConnectorTypesStale: () => boolean;
  isParamsStale: (providerId: string) => boolean;

  // Clear all cached data
  clearAll: () => void;

  // Initialize store and validate cache version
  initialize: () => void;
}

export const useConnectorsStore = create<ConnectorsState>()(
  persist(
    (set, get) => ({
      cacheVersion: CACHE_VERSION,

      // Connector types state
      connectorTypes: [],
      connectorTypesLoading: false,
      connectorTypesError: null,
      connectorTypesFetchedAt: null,

      // Params cache state
      paramsCache: {},
      paramsCacheLoading: {},
      paramsCacheError: {},

      // Connector types actions
      setConnectorTypes: (data) =>
        set({
          connectorTypes: data,
          connectorTypesLoading: false,
          connectorTypesError: null,
          connectorTypesFetchedAt: Date.now(),
        }),

      setConnectorTypesLoading: (loading) =>
        set({ connectorTypesLoading: loading }),

      setConnectorTypesError: (error) =>
        set({ connectorTypesError: error, connectorTypesLoading: false }),

      clearConnectorTypes: () =>
        set({
          connectorTypes: [],
          connectorTypesError: null,
          connectorTypesFetchedAt: null,
        }),

      // Params cache actions
      setParams: (providerId, data) =>
        set((state) => ({
          paramsCache: {
            ...state.paramsCache,
            [providerId]: { data, fetchedAt: Date.now() },
          },
          paramsCacheLoading: {
            ...state.paramsCacheLoading,
            [providerId]: false,
          },
          paramsCacheError: { ...state.paramsCacheError, [providerId]: null },
        })),

      setParamsLoading: (providerId, loading) =>
        set((state) => ({
          paramsCacheLoading: {
            ...state.paramsCacheLoading,
            [providerId]: loading,
          },
        })),

      setParamsError: (providerId, error) =>
        set((state) => ({
          paramsCacheError: { ...state.paramsCacheError, [providerId]: error },
          paramsCacheLoading: {
            ...state.paramsCacheLoading,
            [providerId]: false,
          },
        })),

      clearParams: () => set({ paramsCache: {}, paramsCacheError: {} }),

      // Selectors
      getParams: (providerId) => get().paramsCache[providerId]?.data ?? null,

      isParamsLoading: (providerId) =>
        get().paramsCacheLoading[providerId] ?? false,

      // Staleness checks
      isConnectorTypesStale: () => {
        const { connectorTypesFetchedAt } = get();
        if (!connectorTypesFetchedAt) return true;
        return (
          Date.now() - connectorTypesFetchedAt > CONNECTOR_TYPES_CACHE_DURATION
        );
      },

      isParamsStale: (providerId) => {
        const cached = get().paramsCache[providerId];
        if (!cached?.fetchedAt) return true;
        return Date.now() - cached.fetchedAt > PARAMS_CACHE_DURATION;
      },

      // Clear all cached data
      clearAll: () =>
        set({
          cacheVersion: CACHE_VERSION,
          connectorTypes: [],
          connectorTypesError: null,
          connectorTypesFetchedAt: null,
          paramsCache: {},
          paramsCacheError: {},
        }),

      // Initialize store and validate cache version at runtime
      initialize: () => {
        const state = get();
        if (state.cacheVersion !== CACHE_VERSION) {
          console.warn(
            `Connectors cache version mismatch: expected ${CACHE_VERSION}, found ${state.cacheVersion}. Clearing cache.`,
          );
          get().clearAll();
        }
      },
    }),
    {
      name: "connectors-storage",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        // Persist configuration data with timestamps for cache invalidation
        cacheVersion: state.cacheVersion,
        connectorTypes: state.connectorTypes,
        connectorTypesFetchedAt: state.connectorTypesFetchedAt,
        paramsCache: state.paramsCache,
      }),
      version: 1,
      migrate: (persistedState: unknown) => {
        const state = persistedState as { cacheVersion?: string } | null;
        if (state?.cacheVersion !== CACHE_VERSION) {
          return {
            cacheVersion: CACHE_VERSION,
            connectorTypes: [],
            connectorTypesFetchedAt: null,
            paramsCache: {},
          };
        }
        return persistedState;
      },
    },
  ),
);
