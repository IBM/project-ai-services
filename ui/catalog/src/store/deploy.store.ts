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

interface DeployState {
  // Architectures - persisted with 15-minute cache
  architectures: ArchitectureSummary[];
  selectedArchitectureId: string | null;
  architecturesLoading: boolean;
  architecturesError: string | null;
  architecturesFetchedAt: number | null;

  // Services - persisted with 15-minute cache
  serviceSummaries: ServiceSummary[];
  serviceSummariesLoading: boolean;
  serviceSummariesError: string | null;
  serviceSummariesFetchedAt: number | null;

  // Architecture details - persisted with 15-minute cache
  architectureDetails: ArchitectureDetailsResponse | null;
  architectureDetailsLoading: boolean;
  architectureDetailsError: string | null;
  architectureDetailsFetchedAt: number | null;

  // Deploy options - persisted with 15-minute cache
  deployOptions: DeployOptionsResponse | null;
  deployOptionsLoading: boolean;
  deployOptionsError: string | null;
  deployOptionsFetchedAt: number | null;

  // Resources cache - not persisted (dynamic data)
  resources: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
  resourcesFetchedAt: number | null;

  // Provider params cache - persisted (configuration data)
  providerParams: Record<string, ProviderParamsCache>;

  // Service params cache - persisted (configuration data)
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
  setDeployOptions: (data: DeployOptionsResponse) => void;
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

  // Check if cache is stale (older than 15 minutes)
  isArchitecturesStale: () => boolean;
  isServiceSummariesStale: () => boolean;
  isArchitectureDetailsStale: () => boolean;
  isDeployOptionsStale: () => boolean;
  isProviderParamsStale: (componentType: string, providerId: string) => boolean;
  isServiceParamsStale: (serviceId: string) => boolean;
  isResourcesStale: () => boolean;

  // Clear all deploy store data
  clearAll: () => void;
}

const CACHE_DURATION = 15 * 60 * 1000; // 15 minutes
const RESOURCES_CACHE_DURATION = 5 * 60 * 1000; // 5 minutes for resources (dynamic data)

export const useDeployStore = create<DeployState>()(
  persist(
    (set, get) => ({
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
      deployOptions: null,
      deployOptionsLoading: false,
      deployOptionsError: null,
      deployOptionsFetchedAt: null,

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
          architecturesFetchedAt: Date.now(),
          architecturesLoading: false,
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
          architecturesFetchedAt: null,
        }),

      // Service summaries actions
      setServiceSummaries: (data) =>
        set({
          serviceSummaries: data,
          serviceSummariesError: null,
          serviceSummariesFetchedAt: Date.now(),
          serviceSummariesLoading: false,
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
          architectureDetailsFetchedAt: Date.now(),
          architectureDetailsLoading: false,
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
      setDeployOptions: (data) =>
        set({
          deployOptions: data,
          deployOptionsError: null,
          deployOptionsFetchedAt: Date.now(),
          deployOptionsLoading: false,
        }),

      setDeployOptionsLoading: (loading) =>
        set({ deployOptionsLoading: loading }),

      setDeployOptionsError: (error) =>
        set({ deployOptionsError: error, deployOptionsLoading: false }),

      clearDeployOptions: () =>
        set({
          deployOptions: null,
          deployOptionsError: null,
          deployOptionsFetchedAt: null,
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
        if (!cached) return null;

        // Check if stale (older than 15 minutes)
        if (Date.now() - cached.fetchedAt > CACHE_DURATION) {
          return null;
        }

        return cached.data;
      },

      isProviderParamsStale: (componentType, providerId) => {
        const key = `${componentType}:${providerId}`;
        const cached = get().providerParams[key];
        if (!cached) return true;
        return Date.now() - cached.fetchedAt > CACHE_DURATION;
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
        if (!cached) return null;

        // Check if stale (older than 15 minutes)
        if (Date.now() - cached.fetchedAt > CACHE_DURATION) {
          return null;
        }

        return cached.data;
      },

      isServiceParamsStale: (serviceId) => {
        const cached = get().serviceParams[serviceId];
        if (!cached) return true;
        return Date.now() - cached.fetchedAt > CACHE_DURATION;
      },

      clearServiceParams: () => set({ serviceParams: {} }),

      // Cache staleness checks (15 minutes for config data, 5 minutes for resources)
      isArchitecturesStale: () => {
        const { architecturesFetchedAt } = get();
        if (!architecturesFetchedAt) return true;
        return Date.now() - architecturesFetchedAt > CACHE_DURATION;
      },

      isServiceSummariesStale: () => {
        const { serviceSummariesFetchedAt } = get();
        if (!serviceSummariesFetchedAt) return true;
        return Date.now() - serviceSummariesFetchedAt > CACHE_DURATION;
      },

      isArchitectureDetailsStale: () => {
        const { architectureDetailsFetchedAt } = get();
        if (!architectureDetailsFetchedAt) return true;
        return Date.now() - architectureDetailsFetchedAt > CACHE_DURATION;
      },

      isDeployOptionsStale: () => {
        const { deployOptionsFetchedAt } = get();
        if (!deployOptionsFetchedAt) return true;
        return Date.now() - deployOptionsFetchedAt > CACHE_DURATION;
      },

      isResourcesStale: () => {
        const { resourcesFetchedAt } = get();
        if (!resourcesFetchedAt) return true;
        return Date.now() - resourcesFetchedAt > RESOURCES_CACHE_DURATION;
      },

      // Clear all deploy store data
      clearAll: () => {
        set({
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
          deployOptions: null,
          deployOptionsError: null,
          deployOptionsFetchedAt: null,
          resources: null,
          resourcesError: null,
          resourcesFetchedAt: null,
          providerParams: {},
          serviceParams: {},
        });
      },
    }),
    {
      name: "deploy-storage",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        // Persist configuration data with timestamps for 15-minute cache
        architectures: state.architectures,
        selectedArchitectureId: state.selectedArchitectureId,
        architecturesFetchedAt: state.architecturesFetchedAt,
        serviceSummaries: state.serviceSummaries,
        serviceSummariesFetchedAt: state.serviceSummariesFetchedAt,
        architectureDetails: state.architectureDetails,
        architectureDetailsFetchedAt: state.architectureDetailsFetchedAt,
        deployOptions: state.deployOptions,
        deployOptionsFetchedAt: state.deployOptionsFetchedAt,
        providerParams: state.providerParams,
        serviceParams: state.serviceParams,
      }),
    },
  ),
);
