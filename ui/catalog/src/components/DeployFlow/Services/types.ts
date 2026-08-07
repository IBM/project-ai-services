import type { ServiceDeployOptions, LLMOption } from "@/types/api.types";

import { SHARED_ACTION_TYPES } from "../Shared/types";
import type {
  DeployFormData,
  BaseStepProps,
  SharedDeployFlowAction,
} from "../Shared/types";

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
  ...SHARED_ACTION_TYPES,
  SET_SELECTED_SERVICE: "SET_SELECTED_SERVICE",
} as const;

export type DeployFlowAction =
  | SharedDeployFlowAction
  | { type: typeof ACTION_TYPES.SET_SELECTED_SERVICE; payload: string | null };

export interface StepProps extends BaseStepProps {
  deployOptions: ServiceDeployOptions;
  selectedServiceId?: string | null;
  llmModelsWithProviders?: LLMOption[];
  serviceDescription?: string;
  isLoadingLlmModels?: boolean;
}
