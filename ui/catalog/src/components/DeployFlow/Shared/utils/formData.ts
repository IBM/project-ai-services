import type {
  BaseDeployFlowState,
  DeployFormData,
  DeploymentRuntimeType,
} from "../types";
import { LOCAL_WORKER_NAME, DEFAULT_RUNTIME } from "@/constants";

// Default form data values shared across both deploy flows.
export const DEFAULT_FORM_DATA: Pick<
  DeployFormData,
  "deploymentType" | "workerName"
> = {
  deploymentType: DEFAULT_RUNTIME as DeploymentRuntimeType,
  workerName: LOCAL_WORKER_NAME,
};

// Shared initial values — each flow spreads this and adds its own fields on top.
export const BASE_INITIAL_STATE = {
  currentStep: 0,
  isDeploying: false,
  isEditing: false,
  hasInsufficientResources: false,
  deployError: null,
  deployToastOpen: false,
  showStepOneNameError: false,
  showStepOneWorkerError: false,
} as const;

// Shared reducer logic for UPDATE_FORM_DATA — identical across both flows.
export function handleUpdateFormData<S extends BaseDeployFlowState>(
  state: S,
  payload: Partial<DeployFormData>,
): S {
  return {
    ...state,
    formData: { ...state.formData, ...payload },
    showStepOneNameError:
      "name" in payload
        ? !String(payload.name ?? "").trim() && state.showStepOneNameError
        : state.showStepOneNameError,
  };
}
