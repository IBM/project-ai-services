export interface ComponentConfig {
  providerId: string;
  params: Record<string, unknown>;
}

export interface DeployFormData {
  name: string;
  version: string;
  globalComponents: Record<string, ComponentConfig>; // e.g., { embedding: {...}, vector_store: {...} }
  services: Record<string, ServiceConfig>; // e.g., { digitize: {...}, chat: {...} }
}

// `inferenceBackend` is a DigitalAssistant-only extension of this — removed in PR 9.
export interface ServiceConfig {
  enabled: boolean;
  version: string;
  components: Record<string, ComponentConfig>; // e.g., { llm: {...}, embedding: {...} }
  params: Record<string, unknown>; // Service-level params from schema
}

export interface BaseStepProps {
  title: string;
  formData: DeployFormData;
  onChange: (updates: Partial<DeployFormData>) => void;
  onEditingChange?: (isEditing: boolean) => void;
  onResourceStatusChange?: (hasInsufficientResources: boolean) => void;
  showNameError?: boolean;
}

export interface ResourceItem {
  label: string;
  required: string;
  available: string;
  unit: string;
  type: "cpu" | "memory" | "accelerator" | "storage";
  acceleratorType?: string;
}

// Each flow's ACTION_TYPES object includes all of these plus its own unique keys.
export const SHARED_ACTION_TYPES = {
  SET_CURRENT_STEP: "SET_CURRENT_STEP",
  SET_IS_DEPLOYING: "SET_IS_DEPLOYING",
  SET_IS_EDITING: "SET_IS_EDITING",
  SET_HAS_INSUFFICIENT_RESOURCES: "SET_HAS_INSUFFICIENT_RESOURCES",
  SET_DEPLOY_ERROR: "SET_DEPLOY_ERROR",
  SET_FORM_DATA: "SET_FORM_DATA",
  UPDATE_FORM_DATA: "UPDATE_FORM_DATA",
  RESET_STATE: "RESET_STATE",
  SET_SHOW_STEP_ONE_NAME_ERROR: "SET_SHOW_STEP_ONE_NAME_ERROR",
} as const;

export type SharedDeployFlowAction =
  | { type: typeof SHARED_ACTION_TYPES.SET_CURRENT_STEP; payload: number }
  | { type: typeof SHARED_ACTION_TYPES.SET_IS_DEPLOYING; payload: boolean }
  | { type: typeof SHARED_ACTION_TYPES.SET_IS_EDITING; payload: boolean }
  | {
      type: typeof SHARED_ACTION_TYPES.SET_HAS_INSUFFICIENT_RESOURCES;
      payload: boolean;
    }
  | {
      type: typeof SHARED_ACTION_TYPES.SET_DEPLOY_ERROR;
      payload: string | null;
    }
  | { type: typeof SHARED_ACTION_TYPES.SET_FORM_DATA; payload: DeployFormData }
  | {
      type: typeof SHARED_ACTION_TYPES.UPDATE_FORM_DATA;
      payload: Partial<DeployFormData>;
    }
  | {
      type: typeof SHARED_ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR;
      payload: boolean;
    }
  | { type: typeof SHARED_ACTION_TYPES.RESET_STATE };
