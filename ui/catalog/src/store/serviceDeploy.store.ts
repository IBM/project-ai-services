import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type {
  ServiceDeployOptions,
  LLMOption,
  Service,
  ProviderSchema,
} from "@/types/api.types";
import type { ServiceDetailData } from "@/components";

interface ServiceDeployState {
  // Service deploy options cache - keyed by "serviceId:runtime"
  serviceDeployOptions: Record<string, ServiceDeployOptions>;
  serviceDeployOptionsLoading: Record<string, boolean>;
  serviceDeployOptionsError: Record<string, string | null>;

  // Component models cache - keyed by "serviceId:componentType:runtime"
  componentModels: Record<string, LLMOption[]>;
  componentModelsLoading: Record<string, boolean>;
  componentModelsError: Record<string, string | null>;

  // Provider schemas cache - keyed by "serviceId:componentType:providerId:runtime"
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
    runtime: string,
    data: ServiceDeployOptions,
  ) => void;
  setServiceDeployOptionsLoading: (
    serviceId: string,
    runtime: string,
    loading: boolean,
  ) => void;
  setServiceDeployOptionsError: (
    serviceId: string,
    runtime: string,
    error: string | null,
  ) => void;
  getServiceDeployOptions: (
    serviceId: string,
    runtime: string,
  ) => ServiceDeployOptions | null;
  clearServiceDeployOptions: (serviceId: string, runtime: string) => void;

  // Actions for component models (generic)
  setComponentModels: (
    serviceId: string,
    componentType: string,
    runtime: string,
    data: LLMOption[],
  ) => void;
  setComponentModelsLoading: (
    serviceId: string,
    componentType: string,
    runtime: string,
    loading: boolean,
  ) => void;
  setComponentModelsError: (
    serviceId: string,
    componentType: string,
    runtime: string,
    error: string | null,
  ) => void;
  getComponentModels: (
    serviceId: string,
    componentType: string,
    runtime: string,
  ) => LLMOption[];
  clearComponentModels: (
    serviceId: string,
    componentType: string,
    runtime: string,
  ) => void;

  // Actions for provider schemas
  setProviderSchema: (
    serviceId: string,
    componentType: string,
    providerId: string,
    runtime: string,
    schema: ProviderSchema,
  ) => void;
  getProviderSchema: (
    serviceId: string,
    componentType: string,
    providerId: string,
    runtime: string,
  ) => ProviderSchema | null;
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

// Builds a composite cache key from the provided parts
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

      // Service deploy options actions — keyed by "serviceId:runtime"
      setServiceDeployOptions: (serviceId, runtime, data) => {
        const key = createKey(serviceId, runtime);
        set((state) => ({
          serviceDeployOptions: {
            ...state.serviceDeployOptions,
            [key]: data,
          },
          serviceDeployOptionsLoading: {
            ...state.serviceDeployOptionsLoading,
            [key]: false,
          },
        }));
      },

      setServiceDeployOptionsLoading: (serviceId, runtime, loading) => {
        const key = createKey(serviceId, runtime);
        set((state) => ({
          serviceDeployOptionsLoading: {
            ...state.serviceDeployOptionsLoading,
            [key]: loading,
          },
        }));
      },

      setServiceDeployOptionsError: (serviceId, runtime, error) => {
        const key = createKey(serviceId, runtime);
        set((state) => ({
          serviceDeployOptionsError: {
            ...state.serviceDeployOptionsError,
            [key]: error,
          },
          serviceDeployOptionsLoading: {
            ...state.serviceDeployOptionsLoading,
            [key]: false,
          },
        }));
      },

      getServiceDeployOptions: (serviceId, runtime) => {
        const state = get();
        return (
          state.serviceDeployOptions[createKey(serviceId, runtime)] || null
        );
      },

      clearServiceDeployOptions: (serviceId, runtime) => {
        const key = createKey(serviceId, runtime);
        set((state) => {
          const newOptions = { ...state.serviceDeployOptions };
          const newErrors = { ...state.serviceDeployOptionsError };
          const newLoading = { ...state.serviceDeployOptionsLoading };
          delete newOptions[key];
          delete newErrors[key];
          delete newLoading[key];
          return {
            serviceDeployOptions: newOptions,
            serviceDeployOptionsError: newErrors,
            serviceDeployOptionsLoading: newLoading,
          };
        });
      },

      // Component models actions — keyed by "serviceId:componentType:runtime"
      setComponentModels: (serviceId, componentType, runtime, data) => {
        const key = createKey(serviceId, componentType, runtime);
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

      setComponentModelsLoading: (
        serviceId,
        componentType,
        runtime,
        loading,
      ) => {
        const key = createKey(serviceId, componentType, runtime);
        set((state) => ({
          componentModelsLoading: {
            ...state.componentModelsLoading,
            [key]: loading,
          },
        }));
      },

      setComponentModelsError: (serviceId, componentType, runtime, error) => {
        const key = createKey(serviceId, componentType, runtime);
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

      getComponentModels: (serviceId, componentType, runtime) => {
        const state = get();
        const key = createKey(serviceId, componentType, runtime);
        return state.componentModels[key] || [];
      },

      clearComponentModels: (serviceId, componentType, runtime) => {
        const key = createKey(serviceId, componentType, runtime);
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

      // Provider schemas actions — keyed by "serviceId:componentType:providerId:runtime"
      setProviderSchema: (
        serviceId,
        componentType,
        providerId,
        runtime,
        schema,
      ) => {
        const key = createKey(serviceId, componentType, providerId, runtime);
        set((state) => ({
          providerSchemas: {
            ...state.providerSchemas,
            [key]: schema,
          },
        }));
      },

      getProviderSchema: (serviceId, componentType, providerId, runtime) => {
        const key = createKey(serviceId, componentType, providerId, runtime);
        const state = get();
        return state.providerSchemas[key] || null;
      },

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
