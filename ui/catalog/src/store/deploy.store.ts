import { create } from "zustand";
import type {
  DeployOptionsResponse,
  ResourcesResponse,
} from "@/types/digitalAssistants";

interface ProviderParamsCache {
  data: Record<string, unknown>;
  fetchedAt: number;
}

interface DeployState {
  // Deploy options cache
  deployOptions: DeployOptionsResponse | null;
  deployOptionsLoading: boolean;
  deployOptionsError: string | null;
  deployOptionsFetchedAt: number | null;

  // Resources cache
  resources: ResourcesResponse | null;
  resourcesLoading: boolean;
  resourcesError: string | null;
  resourcesFetchedAt: number | null;

  // Provider params cache - keyed by "componentType:providerId"
  providerParams: Record<string, ProviderParamsCache>;

  // Actions
  setDeployOptions: (data: DeployOptionsResponse) => void;
  setDeployOptionsLoading: (loading: boolean) => void;
  setDeployOptionsError: (error: string | null) => void;
  clearDeployOptions: () => void;

  setResources: (data: ResourcesResponse) => void;
  setResourcesLoading: (loading: boolean) => void;
  setResourcesError: (error: string | null) => void;
  clearResources: () => void;

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

  // Check if cache is stale (older than 5 minutes)
  isDeployOptionsStale: () => boolean;
  isResourcesStale: () => boolean;
}

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes

export const useDeployStore = create<DeployState>((set, get) => ({
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

  // Deploy options actions
  setDeployOptions: (data) =>
    set({
      deployOptions: data,
      deployOptionsError: null,
      deployOptionsFetchedAt: Date.now(),
      deployOptionsLoading: false,
    }),

  setDeployOptionsLoading: (loading) => set({ deployOptionsLoading: loading }),

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

  // Cache staleness checks
  isDeployOptionsStale: () => {
    const { deployOptionsFetchedAt } = get();
    if (!deployOptionsFetchedAt) return true;
    return Date.now() - deployOptionsFetchedAt > CACHE_DURATION;
  },

  isResourcesStale: () => {
    const { resourcesFetchedAt } = get();
    if (!resourcesFetchedAt) return true;
    return Date.now() - resourcesFetchedAt > CACHE_DURATION;
  },
}));
