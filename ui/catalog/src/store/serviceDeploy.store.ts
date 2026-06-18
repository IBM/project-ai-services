import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type {
  ServiceDeployOptions,
  LLMOption,
  Service,
  ProviderSchema,
} from "@/services/deployment.api";
import type { ServiceDetailData } from "@/components";

interface ServiceDeployState {
  // Service deploy options cache - keyed by serviceId (static data - no refetch needed)
  serviceDeployOptions: Record<string, ServiceDeployOptions>;
  serviceDeployOptionsLoading: Record<string, boolean>;
  serviceDeployOptionsError: Record<string, string | null>;

  // Component models cache - keyed by "serviceId:componentType" (static data - no refetch needed)
  // Stores model options for any component type (embedding, llm, reranker, etc.)
  componentModels: Record<string, LLMOption[]>;
  componentModelsLoading: Record<string, boolean>;
  componentModelsError: Record<string, string | null>;

  // Provider schemas cache - keyed by "serviceId:componentType:providerId" (static data - no refetch needed)
  providerSchemas: Record<string, ProviderSchema>;

  // Services cache (for StepZero) (static data - no refetch needed)
  services: Service[] | null;
  servicesLoading: boolean;
  servicesError: string | null;

  // Catalog services (for Services page Catalog tab) (static data - no refetch needed)
  catalogServices: ServiceDetailData[];
  catalogServicesLoading: boolean;
  catalogServicesError: string | null;

  // Deployed services (for Services page Deployments tab) (dynamic data - needs refetch)
  deployedServices: unknown[];
  deployedServicesLoading: boolean;
  deployedServicesError: string | null;
  deployedServicesFetchedAt: number | null;

  // Actions for service deploy options
  setServiceDeployOptions: (
    serviceId: string,
    data: ServiceDeployOptions,
  ) => void;
  setServiceDeployOptionsLoading: (serviceId: string, loading: boolean) => void;
  setServiceDeployOptionsError: (
    serviceId: string,
    error: string | null,
  ) => void;
  getServiceDeployOptions: (serviceId: string) => ServiceDeployOptions | null;
  clearServiceDeployOptions: (serviceId: string) => void;

  // Actions for component models (generic)
  setComponentModels: (
    serviceId: string,
    componentType: string,
    data: LLMOption[],
  ) => void;
  setComponentModelsLoading: (
    serviceId: string,
    componentType: string,
    loading: boolean,
  ) => void;
  setComponentModelsError: (
    serviceId: string,
    componentType: string,
    error: string | null,
  ) => void;
  getComponentModels: (serviceId: string, componentType: string) => LLMOption[];
  clearComponentModels: (serviceId: string, componentType: string) => void;

  // Actions for provider schemas
  setProviderSchema: (
    serviceId: string,
    componentType: string,
    providerId: string,
    schema: ProviderSchema,
  ) => void;
  getProviderSchema: (
    serviceId: string,
    componentType: string,
    providerId: string,
  ) => ProviderSchema | null;
  clearProviderSchemas: (serviceId: string) => void;

  setServices: (data: Service[]) => void;
  setServicesLoading: (loading: boolean) => void;
  setServicesError: (error: string | null) => void;
  clearServices: () => void;

  // Actions for catalog services
  setCatalogServices: (data: ServiceDetailData[]) => void;
  setCatalogServicesLoading: (loading: boolean) => void;
  setCatalogServicesError: (error: string | null) => void;
  clearCatalogServices: () => void;

  // Actions for deployed services
  setDeployedServices: (data: unknown[]) => void;
  setDeployedServicesLoading: (loading: boolean) => void;
  setDeployedServicesError: (error: string | null) => void;
  clearDeployedServices: () => void;

  // Cache staleness check - only for deployed services (dynamic data)
  isDeployedServicesStale: () => boolean;

  // Clear all cache
  clearAllCache: () => void;
}

const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes

// Helper function to generate composite keys
const createKey = (...parts: string[]): string => parts.join(":");

export const useServiceDeployStore = create<ServiceDeployState>()(
  persist(
    (set, get) => ({
      // Service deploy options state
      serviceDeployOptions: {},
      serviceDeployOptionsLoading: {},
      serviceDeployOptionsError: {},

      // Component models state
      componentModels: {},
      componentModelsLoading: {},
      componentModelsError: {},

      // Provider schemas state
      providerSchemas: {},

      // Services state
      services: null,
      servicesLoading: false,
      servicesError: null,

      // Catalog services state
      catalogServices: [],
      catalogServicesLoading: false,
      catalogServicesError: null,

      // Deployed services state
      deployedServices: [],
      deployedServicesLoading: false,
      deployedServicesError: null,
      deployedServicesFetchedAt: null,

      // Service deploy options actions
      setServiceDeployOptions: (serviceId, data) =>
        set((state) => ({
          serviceDeployOptions: {
            ...state.serviceDeployOptions,
            [serviceId]: data,
          },
          serviceDeployOptionsLoading: {
            ...state.serviceDeployOptionsLoading,
            [serviceId]: false,
          },
        })),

      setServiceDeployOptionsLoading: (serviceId, loading) =>
        set((state) => ({
          serviceDeployOptionsLoading: {
            ...state.serviceDeployOptionsLoading,
            [serviceId]: loading,
          },
        })),

      setServiceDeployOptionsError: (serviceId, error) =>
        set((state) => ({
          serviceDeployOptionsError: {
            ...state.serviceDeployOptionsError,
            [serviceId]: error,
          },
          serviceDeployOptionsLoading: {
            ...state.serviceDeployOptionsLoading,
            [serviceId]: false,
          },
        })),

      getServiceDeployOptions: (serviceId) => {
        const state = get();
        return state.serviceDeployOptions[serviceId] || null;
      },

      clearServiceDeployOptions: (serviceId) =>
        set((state) => {
          const newOptions = { ...state.serviceDeployOptions };
          const newErrors = { ...state.serviceDeployOptionsError };
          const newLoading = { ...state.serviceDeployOptionsLoading };
          delete newOptions[serviceId];
          delete newErrors[serviceId];
          delete newLoading[serviceId];
          return {
            serviceDeployOptions: newOptions,
            serviceDeployOptionsError: newErrors,
            serviceDeployOptionsLoading: newLoading,
          };
        }),

      // Component models actions (generic for any component type)
      setComponentModels: (serviceId, componentType, data) => {
        const key = createKey(serviceId, componentType);
        set((state) => ({
          componentModels: {
            ...state.componentModels,
            [key]: data,
          },
          componentModelsLoading: {
            ...state.componentModelsLoading,
            [key]: false,
          },
        }));
      },

      setComponentModelsLoading: (serviceId, componentType, loading) => {
        const key = createKey(serviceId, componentType);
        set((state) => ({
          componentModelsLoading: {
            ...state.componentModelsLoading,
            [key]: loading,
          },
        }));
      },

      setComponentModelsError: (serviceId, componentType, error) => {
        const key = createKey(serviceId, componentType);
        set((state) => ({
          componentModelsError: {
            ...state.componentModelsError,
            [key]: error,
          },
          componentModelsLoading: {
            ...state.componentModelsLoading,
            [key]: false,
          },
        }));
      },

      getComponentModels: (serviceId, componentType) => {
        const state = get();
        const key = createKey(serviceId, componentType);
        return state.componentModels[key] || [];
      },

      clearComponentModels: (serviceId, componentType) => {
        const key = createKey(serviceId, componentType);
        set((state) => {
          const newModels = { ...state.componentModels };
          const newErrors = { ...state.componentModelsError };
          const newLoading = { ...state.componentModelsLoading };
          delete newModels[key];
          delete newErrors[key];
          delete newLoading[key];
          return {
            componentModels: newModels,
            componentModelsError: newErrors,
            componentModelsLoading: newLoading,
          };
        });
      },

      // Provider schemas actions
      setProviderSchema: (serviceId, componentType, providerId, schema) => {
        const key = createKey(serviceId, componentType, providerId);
        set((state) => ({
          providerSchemas: {
            ...state.providerSchemas,
            [key]: schema,
          },
        }));
      },

      getProviderSchema: (serviceId, componentType, providerId) => {
        const key = createKey(serviceId, componentType, providerId);
        const state = get();
        return state.providerSchemas[key] || null;
      },

      clearProviderSchemas: (serviceId) =>
        set((state) => {
          const newSchemas = { ...state.providerSchemas };

          // Remove all schemas for this serviceId
          Object.keys(newSchemas).forEach((key) => {
            if (key.startsWith(`${serviceId}:`)) {
              delete newSchemas[key];
            }
          });

          return {
            providerSchemas: newSchemas,
          };
        }),

      // Services actions
      setServices: (data) =>
        set({
          services: data,
          servicesLoading: false,
        }),

      setServicesLoading: (loading) => set({ servicesLoading: loading }),

      setServicesError: (error) =>
        set({ servicesError: error, servicesLoading: false }),

      clearServices: () =>
        set({
          services: null,
          servicesError: null,
          servicesLoading: false,
        }),

      // Catalog services actions
      setCatalogServices: (data) =>
        set({
          catalogServices: data,
          catalogServicesLoading: false,
        }),

      setCatalogServicesLoading: (loading) =>
        set({ catalogServicesLoading: loading }),

      setCatalogServicesError: (error) =>
        set({ catalogServicesError: error, catalogServicesLoading: false }),

      clearCatalogServices: () =>
        set({
          catalogServices: [],
          catalogServicesError: null,
          catalogServicesLoading: false,
        }),

      // Deployed services actions
      setDeployedServices: (data) =>
        set({
          deployedServices: data,
          deployedServicesFetchedAt: Date.now(),
          deployedServicesLoading: false,
        }),

      setDeployedServicesLoading: (loading) =>
        set({ deployedServicesLoading: loading }),

      setDeployedServicesError: (error) =>
        set({ deployedServicesError: error, deployedServicesLoading: false }),

      clearDeployedServices: () =>
        set({
          deployedServices: [],
          deployedServicesError: null,
          deployedServicesFetchedAt: null,
          deployedServicesLoading: false,
        }),

      // Cache staleness check - only for deployed services (dynamic data)
      isDeployedServicesStale: () => {
        const { deployedServicesFetchedAt } = get();
        if (!deployedServicesFetchedAt) return true;
        return Date.now() - deployedServicesFetchedAt > CACHE_DURATION;
      },

      // Clear all cache
      clearAllCache: () =>
        set({
          serviceDeployOptions: {},
          serviceDeployOptionsLoading: {},
          serviceDeployOptionsError: {},
          componentModels: {},
          componentModelsLoading: {},
          componentModelsError: {},
          providerSchemas: {},
          services: null,
          servicesLoading: false,
          servicesError: null,
          catalogServices: [],
          catalogServicesLoading: false,
          catalogServicesError: null,
          deployedServices: [],
          deployedServicesLoading: false,
          deployedServicesError: null,
          deployedServicesFetchedAt: null,
        }),
    }),
    {
      name: "service-deploy-storage",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        // Only persist static/configuration data (no refetch needed)
        serviceDeployOptions: state.serviceDeployOptions,
        componentModels: state.componentModels,
        providerSchemas: state.providerSchemas,
        services: state.services,
        catalogServices: state.catalogServices,
        // Do NOT persist dynamic data (deployed services with timestamps)
      }),
    },
  ),
);

// Made with Bob
