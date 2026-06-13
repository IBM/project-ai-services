import type { DeployOptionsResponse } from "@/types/digitalAssistants";

export interface DeployFlowProps {
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
}

export interface DeployFlowState {
  currentStep: number;
  isLoading: boolean;
  isDeploying: boolean;
  isEditing: boolean;
  hasInsufficientResources: boolean;
  error: string | null;
  deployError: string | null;
  deployToastOpen: boolean;
  formData: DeployFormData;
  showStepOneNameError: boolean;
}

export const ACTION_TYPES = {
  SET_CURRENT_STEP: "SET_CURRENT_STEP",
  SET_IS_LOADING: "SET_IS_LOADING",
  SET_IS_DEPLOYING: "SET_IS_DEPLOYING",
  SET_IS_EDITING: "SET_IS_EDITING",
  SET_HAS_INSUFFICIENT_RESOURCES: "SET_HAS_INSUFFICIENT_RESOURCES",
  SET_ERROR: "SET_ERROR",
  SET_DEPLOY_ERROR: "SET_DEPLOY_ERROR",
  SHOW_DEPLOY_TOAST: "SHOW_DEPLOY_TOAST",
  HIDE_DEPLOY_TOAST: "HIDE_DEPLOY_TOAST",
  SET_FORM_DATA: "SET_FORM_DATA",
  UPDATE_FORM_DATA: "UPDATE_FORM_DATA",
  RESET_STATE: "RESET_STATE",
  SET_SHOW_STEP_ONE_NAME_ERROR: "SET_SHOW_STEP_ONE_NAME_ERROR",
} as const;

export type DeployFlowAction =
  | { type: typeof ACTION_TYPES.SET_CURRENT_STEP; payload: number }
  | { type: typeof ACTION_TYPES.SET_IS_LOADING; payload: boolean }
  | { type: typeof ACTION_TYPES.SET_IS_DEPLOYING; payload: boolean }
  | { type: typeof ACTION_TYPES.SET_IS_EDITING; payload: boolean }
  | {
      type: typeof ACTION_TYPES.SET_HAS_INSUFFICIENT_RESOURCES;
      payload: boolean;
    }
  | { type: typeof ACTION_TYPES.SET_ERROR; payload: string | null }
  | { type: typeof ACTION_TYPES.SET_DEPLOY_ERROR; payload: string | null }
  | { type: typeof ACTION_TYPES.SHOW_DEPLOY_TOAST }
  | { type: typeof ACTION_TYPES.HIDE_DEPLOY_TOAST }
  | { type: typeof ACTION_TYPES.SET_FORM_DATA; payload: DeployFormData }
  | {
      type: typeof ACTION_TYPES.UPDATE_FORM_DATA;
      payload: Partial<DeployFormData>;
    }
  | {
      type: typeof ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR;
      payload: boolean;
    }
  | { type: typeof ACTION_TYPES.RESET_STATE };

/**
 * Component configuration for a provider
 * Contains provider ID and dynamic parameters from schema
 */
export interface ComponentConfig {
  providerId: string;
  params: Record<string, unknown>;
}

/**
 * Service configuration
 * Dynamic structure based on API response
 */
export interface ServiceConfig {
  enabled: boolean;
  version: string;
  components: Record<string, ComponentConfig>; // e.g., { llm: {...}, embedding: {...} }
  params: Record<string, unknown>; // Service-level params from schema
  inferenceBackend?: string; // Selected LLM provider ID for services with LLM component
}

/**
 * Deploy form data
 * Completely dynamic structure driven by API
 */
export interface DeployFormData {
  name: string;
  version: string;
  globalComponents: Record<string, ComponentConfig>; // e.g., { embedding: {...}, vector_store: {...} }
  services: Record<string, ServiceConfig>; // e.g., { digitize: {...}, chat: {...} }
}

export interface StepProps {
  title: string;
  formData: DeployFormData;
  onChange: (updates: Partial<DeployFormData>) => void;
  deployOptions: DeployOptionsResponse;
  onEditingChange?: (isEditing: boolean) => void;
  onResourceStatusChange?: (hasInsufficientResources: boolean) => void;
  showNameError?: boolean;
}
