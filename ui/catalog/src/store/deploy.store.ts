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
  // Architectures - persisted (configuration data)
  architectures: ArchitectureSummary[];
  selectedArchitectureId: string | null;
  architecturesLoading: boolean;
  architecturesError: string | null;

  // Services - persisted (configuration data with descriptions)
  serviceSummaries: ServiceSummary[];
  serviceSummariesLoading: boolean;
  serviceSummariesError: string | null;

  // Architecture details - persisted (configuration data)
  architectureDetails: ArchitectureDetailsResponse | null;
  architectureDetailsLoading: boolean;
  architectureDetailsError: string | null;

  // Deploy options - persisted (configuration data)
  deployOptions: DeployOptionsResponse | null;
  deployOptionsLoading: boolean;
  deployOptionsError: string | null;

  // Resources cache - not persisted (dynamic data)
  resources: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
  resourcesFetchedAt: number | null;

  // Provider params cache - not persisted (dynamic data)
  providerParams: Record<string, ProviderParamsCache>;

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
  isProviderParamsStale: (componentType: string, providerId: string) => boolean;
  clearProviderParams: () => void;

  // Check if cache is stale (older than 5 minutes) - only for resources
  isResourcesStale: () => boolean;
}

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes

export const useDeployStore = create<DeployState>()(
  persist(
    (set, get) => ({
      // Architectures state
      architectures: [],
      selectedArchitectureId: null,
      architecturesLoading: false,
      architecturesError: null,

      // Service summaries state
      serviceSummaries: [],
      serviceSummariesLoading: false,
      serviceSummariesError: null,

      // Architecture details state
      architectureDetails: null,
      architectureDetailsLoading: false,
      architectureDetailsError: null,

      // Deploy options state
      deployOptions: null,
      deployOptionsLoading: false,
      deployOptionsError: null,

      // Resources state
      resources: null,
      resourcesLoading: false,
      resourcesError: null,
      resourcesFetchedAt: null,

      // Provider params state
      providerParams: {},

      // Architectures actions
      setArchitectures: (data) =>
        set({
          architectures: data,
          selectedArchitectureId: data.length > 0 ? data[0].id : null,
          architecturesError: null,
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
        }),

      // Service summaries actions
      setServiceSummaries: (data) =>
        set({
          serviceSummaries: data,
          serviceSummariesError: null,
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
        }),

      // Architecture details actions
      setArchitectureDetails: (data) =>
        set({
          architectureDetails: data,
          architectureDetailsError: null,
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
        }),

      // Deploy options actions
      setDeployOptions: (data) =>
        set({
          deployOptions: data,
          deployOptionsError: null,
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

        // Check if stale
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

      // Cache staleness check - only for resources (5 minutes)
      isResourcesStale: () => {
        const { resourcesFetchedAt } = get();
        if (!resourcesFetchedAt) return true;
        return Date.now() - resourcesFetchedAt > CACHE_DURATION;
      },
    }),
    {
      name: "deploy-storage",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        // Only persist architectures, serviceSummaries, architectureDetails, and deployOptions
        architectures: state.architectures,
        selectedArchitectureId: state.selectedArchitectureId,
        serviceSummaries: state.serviceSummaries,
        architectureDetails: state.architectureDetails,
        deployOptions: state.deployOptions,
      }),
    },
  ),
);
