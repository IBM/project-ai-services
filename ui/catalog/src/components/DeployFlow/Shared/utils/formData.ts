import type { BaseDeployFlowState, DeployFormData } from "../types";

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
