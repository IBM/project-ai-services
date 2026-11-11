import { create } from "zustand";
import type { CatalogModel, CatalogModelDetails } from "@/api/models.api";

interface ModelsState {
  models: CatalogModel[] | null;
  modelsLoading: boolean;
  modelsError: string | null;
  modelDetailsCache: Record<string, CatalogModelDetails>;

  setModels: (data: CatalogModel[]) => void;
  setModelsLoading: (loading: boolean) => void;
  setModelsError: (error: string | null) => void;
  getModelDetails: (modelId: string) => CatalogModelDetails | null;
  setModelDetails: (modelId: string, details: CatalogModelDetails) => void;
  clearModelDetailsCache: () => void;
}

export const useModelsStore = create<ModelsState>()((set, get) => ({
  models: null,
  modelsLoading: false,
  modelsError: null,
  modelDetailsCache: {},

  setModels: (data) => set({ models: data, modelsLoading: false }),

  setModelsLoading: (loading) => set({ modelsLoading: loading }),

  setModelsError: (error) => set({ modelsError: error, modelsLoading: false }),

  getModelDetails: (modelId) => {
    return get().modelDetailsCache[modelId] || null;
  },

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
