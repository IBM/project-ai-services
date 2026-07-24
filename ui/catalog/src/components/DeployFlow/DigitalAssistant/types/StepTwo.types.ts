import type { ServiceConfig } from "../types";

// Resource item for display
export interface ResourceItem {
  label: string;
  required: string;
  available: string;
  unit: string;
  type: "cpu" | "memory" | "accelerator" | "storage";
  acceleratorType?: string;
}

// State interface for StepTwo useReducer
export interface StepTwoState {
  editingService: string | null;
  tempConfig: ServiceConfig | null;
  // Dynamic model names: componentType -> providerId -> modelName
  modelNamesByComponent: Record<string, Record<string, string>>;
}

// Action types for StepTwo reducer
export type StepTwoAction =
  | { type: "SET_EDITING_SERVICE"; payload: string | null }
  | { type: "SET_TEMP_CONFIG"; payload: ServiceConfig | null }
  | { type: "UPDATE_TEMP_CONFIG"; payload: Partial<ServiceConfig> }
  | {
      type: "SET_MODEL_NAMES";
      payload: { componentType: string; modelNames: Record<string, string> };
    }
  | { type: "RESET_EDITING" };

// Field configuration for service config cards
export interface ServiceConfigField {
  key: keyof ServiceConfig;
  label: string;
  options: Array<{ id: string; text: string }>;
  readonly?: boolean;
  globalValue?: string;
}
