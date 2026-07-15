import type { ServiceDeployOptions, LLMOption } from "@/types/api.types";
import type { DeployFormData } from "../shared/types";

// Re-export shared types so existing consumers of this file keep working.
export type {
  ComponentConfig,
  ServiceConfig,
  DeployFormData,
  BaseStepProps,
  ResourceItem,
  SharedDeployFlowAction,
} from "../shared/types";

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
