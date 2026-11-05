import { create } from "zustand";
import type { CatalogModel } from "@/api/models.api";

interface ModelsState {
  models: CatalogModel[] | null;
  modelsLoading: boolean;
  modelsError: string | null;

  setModels: (data: CatalogModel[]) => void;
  setModelsLoading: (loading: boolean) => void;
  setModelsError: (error: string | null) => void;
}

export const useModelsStore = create<ModelsState>()((set) => ({
  models: null,
  modelsLoading: false,
  modelsError: null,

  setModels: (data) => set({ models: data, modelsLoading: false }),

  setModelsLoading: (loading) => set({ modelsLoading: loading }),

  setModelsError: (error) => set({ modelsError: error, modelsLoading: false }),
}));
