import { create } from "zustand";
import type { ServiceDetailData } from "@/components/ServiceDetailPanel/ServiceDetailPanel";

type ServiceDetailsState = {
  // Cache of service details by service ID
  serviceDetailsCache: Record<string, ServiceDetailData>;

  // Get service details from cache
  getServiceDetails: (serviceId: string) => ServiceDetailData | null;

  // Set service details in cache
  setServiceDetails: (serviceId: string, details: ServiceDetailData) => void;

  // Clear entire cache
  clearCache: () => void;

  // Remove specific service from cache
  removeServiceDetails: (serviceId: string) => void;
};

export const useServiceDetailsStore = create<ServiceDetailsState>(
  (set, get) => ({
    serviceDetailsCache: {},

    getServiceDetails: (serviceId) => {
      const cache = get().serviceDetailsCache;
      return cache[serviceId] || null;
    },

    setServiceDetails: (serviceId, details) => {
      set((state) => ({
        serviceDetailsCache: {
          ...state.serviceDetailsCache,
          [serviceId]: details,
        },
      }));
    },

    clearCache: () => {
      set({ serviceDetailsCache: {} });
    },

    removeServiceDetails: (serviceId) => {
      set((state) => {
        const newCache = { ...state.serviceDetailsCache };
        delete newCache[serviceId];
        return { serviceDetailsCache: newCache };
      });
    },
  }),
);

// Made with Bob
