import type { ServiceDeployOptions, LLMOption } from "@/types/api.types";

export interface ServicesDeployFlowProps {
  open: boolean;
  onClose: () => void;
  onSubmit: () => void;
  preSelectedServiceId?: string;
}

export interface DeployFlowState {
  currentStep: number;
  isDeploying: boolean;
  isEditing: boolean;
  hasInsufficientResources: boolean;
  deployError: string | null;
  formData: DeployFormData;
  selectedServiceId: string | null;
  showStepOneNameError: boolean;
}

export const ACTION_TYPES = {
  SET_CURRENT_STEP: "SET_CURRENT_STEP",
  SET_IS_DEPLOYING: "SET_IS_DEPLOYING",
  SET_IS_EDITING: "SET_IS_EDITING",
  SET_HAS_INSUFFICIENT_RESOURCES: "SET_HAS_INSUFFICIENT_RESOURCES",
  SET_DEPLOY_ERROR: "SET_DEPLOY_ERROR",
  SET_FORM_DATA: "SET_FORM_DATA",
  UPDATE_FORM_DATA: "UPDATE_FORM_DATA",
  SET_SELECTED_SERVICE: "SET_SELECTED_SERVICE",
  RESET_STATE: "RESET_STATE",
  SET_SHOW_STEP_ONE_NAME_ERROR: "SET_SHOW_STEP_ONE_NAME_ERROR",
} as const;

export type DeployFlowAction =
  | { type: typeof ACTION_TYPES.SET_CURRENT_STEP; payload: number }
  | { type: typeof ACTION_TYPES.SET_IS_DEPLOYING; payload: boolean }
  | { type: typeof ACTION_TYPES.SET_IS_EDITING; payload: boolean }
  | {
      type: typeof ACTION_TYPES.SET_HAS_INSUFFICIENT_RESOURCES;
      payload: boolean;
    }
  | { type: typeof ACTION_TYPES.SET_DEPLOY_ERROR; payload: string | null }
  | { type: typeof ACTION_TYPES.SET_FORM_DATA; payload: DeployFormData }
  | {
      type: typeof ACTION_TYPES.UPDATE_FORM_DATA;
      payload: Partial<DeployFormData>;
    }
  | { type: typeof ACTION_TYPES.SET_SELECTED_SERVICE; payload: string | null }
  | { type: typeof ACTION_TYPES.RESET_STATE }
  | {
      type: typeof ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR;
      payload: boolean;
    };

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
}

/**
 * Deploy form data
 * Completely dynamic structure driven by API
 */
export interface DeployFormData {
  name: string;
  version: string;
  globalComponents: Record<string, ComponentConfig>; // e.g., { embedding: {...}, vector_store: {...} }
  services: Record<string, ServiceConfig>; // e.g., { digitize: {...}, summarize: {...} }
}

export interface StepProps {
  title: string;
  formData: DeployFormData;
  onChange: (updates: Partial<DeployFormData>) => void;
  deployOptions: ServiceDeployOptions;
  onEditingChange?: (isEditing: boolean) => void;
  onResourceStatusChange?: (hasInsufficientResources: boolean) => void;
  selectedServiceId?: string | null;
  llmModelsWithProviders?: LLMOption[];
  serviceDescription?: string;
  isLoadingLlmModels?: boolean;
  showNameError?: boolean;
}

// Made with Bob
