import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type {
  ArchitectureSummary,
  ServiceSummary,
  ArchitectureDetailsResponse,
  DeployOptionsResponse,
  ResourcesResponse,
} from "@/types/digitalAssistants";

interface ProviderParamsCache {
  data: Record<string, unknown>;
  fetchedAt: number;
}

interface DeployOptionsCache {
  data: DeployOptionsResponse;
  fetchedAt: number;
}

interface DeployState {
  // Cache version for invalidating stale schemas
  cacheVersion: string;

  // Architectures - persisted with 30-minute cache
  architectures: ArchitectureSummary[];
  selectedArchitectureId: string | null;
  architecturesLoading: boolean;
  architecturesError: string | null;
  architecturesFetchedAt: number | null;

  // Services - persisted with 30-minute cache
  serviceSummaries: ServiceSummary[];
  serviceSummariesLoading: boolean;
  serviceSummariesError: string | null;
  serviceSummariesFetchedAt: number | null;

  // Architecture details - persisted with 30-minute cache
  architectureDetails: ArchitectureDetailsResponse | null;
  architectureDetailsLoading: boolean;
  architectureDetailsError: string | null;
  architectureDetailsFetchedAt: number | null;

  // Deploy options - persisted with 15-minute cache, keyed by architecture ID
  deployOptions: Record<string, DeployOptionsCache>;
  deployOptionsLoading: boolean;
  deployOptionsError: string | null;

  // Resources cache - not persisted (dynamic data)
  resources: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
  resourcesFetchedAt: number | null;

  // Provider params cache - persisted with 1-hour cache
  providerParams: Record<string, ProviderParamsCache>;

  // Service params cache - persisted with 1-hour cache
  serviceParams: Record<string, ProviderParamsCache>;

  // Architecture actions
  setArchitectures: (data: ArchitectureSummary[]) => void;
  setSelectedArchitectureId: (id: string | null) => void;
  setArchitecturesLoading: (loading: boolean) => void;
  setArchitecturesError: (error: string | null) => void;
  clearArchitectures: () => void;

  // Service summaries actions
  setServiceSummaries: (data: ServiceSummary[]) => void;
  setServiceSummariesLoading: (loading: boolean) => void;
  setServiceSummariesError: (error: string | null) => void;
  getServiceDescription: (serviceId: string) => string;
  clearServiceSummaries: () => void;

  // Architecture details actions
  setArchitectureDetails: (data: ArchitectureDetailsResponse) => void;
  setArchitectureDetailsLoading: (loading: boolean) => void;
  setArchitectureDetailsError: (error: string | null) => void;
  clearArchitectureDetails: () => void;

  // Deploy options actions
  setDeployOptions: (
    architectureId: string,
    data: DeployOptionsResponse,
  ) => void;
  getDeployOptions: (architectureId: string) => DeployOptionsResponse | null;
  setDeployOptionsLoading: (loading: boolean) => void;
  setDeployOptionsError: (error: string | null) => void;
  clearDeployOptions: () => void;

  // Resources actions
  setResources: (data: ResourcesResponse) => void;
  setResourcesLoading: (loading: boolean) => void;
  setResourcesError: (error: string | null) => void;
  clearResources: () => void;

  // Provider params actions
  setProviderParams: (
    componentType: string,
    providerId: string,
    data: Record<string, unknown>,
  ) => void;
  getProviderParams: (
    componentType: string,
    providerId: string,
  ) => Record<string, unknown> | null;
  clearProviderParams: () => void;

  // Service params actions
  setServiceParams: (serviceId: string, data: Record<string, unknown>) => void;
  getServiceParams: (serviceId: string) => Record<string, unknown> | null;
  clearServiceParams: () => void;

  // Check if cache is stale
  isArchitecturesStale: () => boolean;
  isServiceSummariesStale: () => boolean;
  isArchitectureDetailsStale: () => boolean;
  isDeployOptionsStale: (architectureId: string) => boolean;
  isResourcesStale: () => boolean;
  isProviderParamsStale: (componentType: string, providerId: string) => boolean;
  isServiceParamsStale: (serviceId: string) => boolean;

  // Clear all deploy store data
  clearAll: () => void;

  // Initialize store and validate cache version
  initialize: () => void;
}

// Cache version - increment when making breaking changes to cached data structure
const CACHE_VERSION = "1.0.0";

// Cache durations
const CATALOG_CACHE_DURATION = 30 * 60 * 1000; // 30 minutes for catalog metadata
const DEPLOY_OPTIONS_CACHE_DURATION = 15 * 60 * 1000; // 15 minutes for deploy options
const PARAMS_CACHE_DURATION = 60 * 60 * 1000; // 1 hour for provider/service params
const RESOURCES_CACHE_DURATION = 5 * 60 * 1000; // 5 minutes for resources (dynamic data)

export const useDeployStore = create<DeployState>()(
  persist(
    (set, get) => ({
      // Cache version
      cacheVersion: CACHE_VERSION,

      // Architectures state
      architectures: [],
      selectedArchitectureId: null,
      architecturesLoading: false,
      architecturesError: null,
      architecturesFetchedAt: null,

      // Service summaries state
      serviceSummaries: [],
      serviceSummariesLoading: false,
      serviceSummariesError: null,
      serviceSummariesFetchedAt: null,

      // Architecture details state
      architectureDetails: null,
      architectureDetailsLoading: false,
      architectureDetailsError: null,
      architectureDetailsFetchedAt: null,

      // Deploy options state
      deployOptions: {},
      deployOptionsLoading: false,
      deployOptionsError: null,

      // Resources state
      resources: null,
      resourcesLoading: false,
      resourcesError: null,
      resourcesFetchedAt: null,

      // Provider params state
      providerParams: {},

      // Service params state
      serviceParams: {},

      // Architectures actions
      setArchitectures: (data) =>
        set({
          architectures: data,
          selectedArchitectureId: data.length > 0 ? data[0].id : null,
          architecturesError: null,
          architecturesLoading: false,
          architecturesFetchedAt: Date.now(),
        }),

      setSelectedArchitectureId: (id) => set({ selectedArchitectureId: id }),

      setArchitecturesLoading: (loading) =>
        set({ architecturesLoading: loading }),

      setArchitecturesError: (error) =>
        set({ architecturesError: error, architecturesLoading: false }),

      clearArchitectures: () =>
        set({
          architectures: [],
          selectedArchitectureId: null,
          architecturesError: null,
        }),

      // Service summaries actions
      setServiceSummaries: (data) =>
        set({
          serviceSummaries: data,
          serviceSummariesError: null,
          serviceSummariesLoading: false,
          serviceSummariesFetchedAt: Date.now(),
        }),

      setServiceSummariesLoading: (loading) =>
        set({ serviceSummariesLoading: loading }),

      setServiceSummariesError: (error) =>
        set({ serviceSummariesError: error, serviceSummariesLoading: false }),

      getServiceDescription: (serviceId) => {
        const service = get().serviceSummaries.find((s) => s.id === serviceId);
        return service?.description || "";
      },

      clearServiceSummaries: () =>
        set({
          serviceSummaries: [],
          serviceSummariesError: null,
          serviceSummariesFetchedAt: null,
        }),

      // Architecture details actions
      setArchitectureDetails: (data) =>
        set({
          architectureDetails: data,
          architectureDetailsError: null,
          architectureDetailsLoading: false,
          architectureDetailsFetchedAt: Date.now(),
        }),

      setArchitectureDetailsLoading: (loading) =>
        set({ architectureDetailsLoading: loading }),

      setArchitectureDetailsError: (error) =>
        set({
          architectureDetailsError: error,
          architectureDetailsLoading: false,
        }),

      clearArchitectureDetails: () =>
        set({
          architectureDetails: null,
          architectureDetailsError: null,
          architectureDetailsFetchedAt: null,
        }),

      // Deploy options actions
      setDeployOptions: (architectureId, data) =>
        set((state) => ({
          deployOptions: {
            ...state.deployOptions,
            [architectureId]: {
              data,
              fetchedAt: Date.now(),
            },
          },
          deployOptionsError: null,
          deployOptionsLoading: false,
        })),

      getDeployOptions: (architectureId) => {
        const cached = get().deployOptions[architectureId];
        return cached ? cached.data : null;
      },

      setDeployOptionsLoading: (loading) =>
        set({ deployOptionsLoading: loading }),

      setDeployOptionsError: (error) =>
        set({ deployOptionsError: error, deployOptionsLoading: false }),

      clearDeployOptions: () =>
        set({
          deployOptions: {},
          deployOptionsError: null,
        }),

      // Resources actions
      setResources: (data) =>
        set({
          resources: data,
          resourcesError: null,
          resourcesFetchedAt: Date.now(),
          resourcesLoading: false,
        }),

      setResourcesLoading: (loading) => set({ resourcesLoading: loading }),

      setResourcesError: (error) =>
        set({ resourcesError: error, resourcesLoading: false }),

      clearResources: () =>
        set({
          resources: null,
          resourcesError: null,
          resourcesFetchedAt: null,
        }),

      // Provider params actions
      setProviderParams: (componentType, providerId, data) => {
        const key = `${componentType}:${providerId}`;
        set((state) => ({
          providerParams: {
            ...state.providerParams,
            [key]: {
              data,
              fetchedAt: Date.now(),
            },
          },
        }));
      },

      getProviderParams: (componentType, providerId) => {
        const key = `${componentType}:${providerId}`;
        const cached = get().providerParams[key];
        return cached ? cached.data : null;
      },

      clearProviderParams: () => set({ providerParams: {} }),

      // Service params actions
      setServiceParams: (serviceId, data) => {
        set((state) => ({
          serviceParams: {
            ...state.serviceParams,
            [serviceId]: {
              data,
              fetchedAt: Date.now(),
            },
          },
        }));
      },

      getServiceParams: (serviceId) => {
        const cached = get().serviceParams[serviceId];
        return cached ? cached.data : null;
      },

      clearServiceParams: () => set({ serviceParams: {} }),

      // Cache staleness checks

      isArchitecturesStale: () => {
        const { architecturesFetchedAt } = get();
        if (!architecturesFetchedAt) return true;
        return Date.now() - architecturesFetchedAt > CATALOG_CACHE_DURATION;
      },

      isServiceSummariesStale: () => {
        const { serviceSummariesFetchedAt } = get();
        if (!serviceSummariesFetchedAt) return true;
        return Date.now() - serviceSummariesFetchedAt > CATALOG_CACHE_DURATION;
      },

      isArchitectureDetailsStale: () => {
        const { architectureDetailsFetchedAt } = get();
        if (!architectureDetailsFetchedAt) return true;
        return (
          Date.now() - architectureDetailsFetchedAt > CATALOG_CACHE_DURATION
        );
      },

      isDeployOptionsStale: (architectureId) => {
        const cached = get().deployOptions[architectureId];
        if (!cached || !cached.fetchedAt) return true;
        return Date.now() - cached.fetchedAt > DEPLOY_OPTIONS_CACHE_DURATION;
      },

      isResourcesStale: () => {
        const { resourcesFetchedAt } = get();
        if (!resourcesFetchedAt) return true;
        return Date.now() - resourcesFetchedAt > RESOURCES_CACHE_DURATION;
      },

      isProviderParamsStale: (componentType, providerId) => {
        const key = `${componentType}:${providerId}`;
        const cached = get().providerParams[key];
        if (!cached || !cached.fetchedAt) return true;
        return Date.now() - cached.fetchedAt > PARAMS_CACHE_DURATION;
      },

      isServiceParamsStale: (serviceId) => {
        const cached = get().serviceParams[serviceId];
        if (!cached || !cached.fetchedAt) return true;
        return Date.now() - cached.fetchedAt > PARAMS_CACHE_DURATION;
      },

      // Clear all deploy store data
      clearAll: () => {
        set({
          cacheVersion: CACHE_VERSION,
          architectures: [],
          selectedArchitectureId: null,
          architecturesError: null,
          architecturesFetchedAt: null,
          serviceSummaries: [],
          serviceSummariesError: null,
          serviceSummariesFetchedAt: null,
          architectureDetails: null,
          architectureDetailsError: null,
          architectureDetailsFetchedAt: null,
          deployOptions: {},
          deployOptionsError: null,
          resources: null,
          resourcesError: null,
          resourcesFetchedAt: null,
          providerParams: {},
          serviceParams: {},
        });
      },

      // Initialize store and validate cache version at runtime
      initialize: () => {
        const state = get();
        // If cache version doesn't match current version, clear all cached data
        // This handles cases where the app is updated without a page reload
        if (state.cacheVersion !== CACHE_VERSION) {
          console.warn(
            `Cache version mismatch: expected ${CACHE_VERSION}, found ${state.cacheVersion}. Clearing cache.`,
          );
          get().clearAll();
        }
      },
    }),
    {
      name: "deploy-storage",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        // Persist configuration data with timestamps for cache invalidation
        cacheVersion: state.cacheVersion,
        architectures: state.architectures,
        selectedArchitectureId: state.selectedArchitectureId,
        architecturesFetchedAt: state.architecturesFetchedAt,
        serviceSummaries: state.serviceSummaries,
        serviceSummariesFetchedAt: state.serviceSummariesFetchedAt,
        architectureDetails: state.architectureDetails,
        architectureDetailsFetchedAt: state.architectureDetailsFetchedAt,
        deployOptions: state.deployOptions,
        providerParams: state.providerParams,
        serviceParams: state.serviceParams,
      }),
      // Version check: clear cache if version mismatch
      version: 1,
      migrate: (persistedState: unknown) => {
        // If cache version doesn't match, clear all cached data
        const state = persistedState as { cacheVersion?: string } | null;
        if (state?.cacheVersion !== CACHE_VERSION) {
          return {
            cacheVersion: CACHE_VERSION,
            architectures: [],
            selectedArchitectureId: null,
            architecturesFetchedAt: null,
            serviceSummaries: [],
            serviceSummariesFetchedAt: null,
            architectureDetails: null,
            architectureDetailsFetchedAt: null,
            deployOptions: {},
            providerParams: {},
            serviceParams: {},
          };
        }
        return persistedState;
      },
    },
  ),
);
