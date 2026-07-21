import type { DeployOptionsResponse } from "@/types/api.types";
import type {
  DeployFormData,
  ServiceConfig as BaseServiceConfig,
} from "../Shared/types";

// Re-export shared types so existing consumers of this file keep working.
export type {
  ComponentConfig,
  DeployFormData,
  BaseStepProps,
  ResourceItem,
  SharedDeployFlowAction,
} from "../Shared/types";

// UI-only field for tracking the selected LLM provider ID. Removed in PR 9.
export interface ServiceConfig extends BaseServiceConfig {
  inferenceBackend?: string;
}

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

export interface StepProps {
  title: string;
  formData: DeployFormData;
  onChange: (updates: Partial<DeployFormData>) => void;
  deployOptions: DeployOptionsResponse;
  onEditingChange?: (isEditing: boolean) => void;
  onResourceStatusChange?: (hasInsufficientResources: boolean) => void;
  showNameError?: boolean;
}
