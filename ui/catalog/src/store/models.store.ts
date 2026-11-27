import { create } from "zustand";
import type { CatalogModel, CatalogModelDetails } from "@/types/api.types";

interface ModelsState {
  models: CatalogModel[] | null;
  modelsLoading: boolean;
  modelsError: string | null;
  modelDetailsCache: Record<string, CatalogModelDetails>;

  setModels: (data: CatalogModel[]) => void;
  setModelsLoading: (loading: boolean) => void;
  setModelsError: (error: string | null) => void;
  setModelDetails: (modelId: string, details: CatalogModelDetails) => void;
  clearModelDetailsCache: () => void;
}

export const useModelsStore = create<ModelsState>()((set) => ({
  models: null,
  modelsLoading: false,
  modelsError: null,
  modelDetailsCache: {},

  setModels: (data) => set({ models: data, modelsLoading: false }),

  setModelsLoading: (loading) => set({ modelsLoading: loading }),

  setModelsError: (error) => set({ modelsError: error, modelsLoading: false }),

  setModelDetails: (modelId, details) => {
    set((state) => ({
      modelDetailsCache: {
        ...state.modelDetailsCache,
        [modelId]: details,
      },
    }));
  },

  clearModelDetailsCache: () => set({ modelDetailsCache: {} }),
}));
