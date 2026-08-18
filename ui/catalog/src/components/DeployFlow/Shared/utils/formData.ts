import type { BaseDeployFlowState, DeployFormData } from "../types";

// Shared initial values — each flow spreads this and adds its own fields on top.
export const BASE_INITIAL_STATE = {
  currentStep: 0,
  isDeploying: false,
  isEditing: false,
  hasInsufficientResources: false,
  deployError: null,
  deployToastOpen: false,
  showStepOneNameError: false,
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
