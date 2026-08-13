import { useCallback } from "react";
import { SHARED_ACTION_TYPES } from "../types";
import type {
  BaseDeployFlowState,
  DeployFormData,
  SharedDeployFlowAction,
} from "../types";
import { handleUpdateFormData } from "../utils/formData";

// Handles all shared action cases — each flow's reducer delegates to this for shared types.
export function sharedDeployFlowReducer<S extends BaseDeployFlowState>(
  state: S,
  action: SharedDeployFlowAction,
): S {
  switch (action.type) {
    case SHARED_ACTION_TYPES.SET_CURRENT_STEP:
      return { ...state, currentStep: action.payload };
    case SHARED_ACTION_TYPES.SET_IS_DEPLOYING:
      return { ...state, isDeploying: action.payload };
    case SHARED_ACTION_TYPES.SET_IS_EDITING:
      return { ...state, isEditing: action.payload };
    case SHARED_ACTION_TYPES.SET_HAS_INSUFFICIENT_RESOURCES:
      return { ...state, hasInsufficientResources: action.payload };
    case SHARED_ACTION_TYPES.SET_DEPLOY_ERROR:
      return { ...state, deployError: action.payload };
    case SHARED_ACTION_TYPES.SHOW_DEPLOY_TOAST:
      return { ...state, deployToastOpen: true };
    case SHARED_ACTION_TYPES.HIDE_DEPLOY_TOAST:
      return { ...state, deployToastOpen: false };
    case SHARED_ACTION_TYPES.SET_FORM_DATA:
      return { ...state, formData: action.payload };
    case SHARED_ACTION_TYPES.UPDATE_FORM_DATA:
      return handleUpdateFormData(state, action.payload);
    case SHARED_ACTION_TYPES.SET_SHOW_STEP_ONE_NAME_ERROR:
      return { ...state, showStepOneNameError: action.payload };
    default:
      return state;
  }
}

// Returns the four callbacks that are identical across both flows.
// Each flow owns its own useReducer call — this hook only wraps the callbacks.
export function useDeployFlowReducer(
  dispatch: React.Dispatch<SharedDeployFlowAction>,
  currentStep: number,
) {
  const handleFormDataChange = useCallback(
    (updates: Partial<DeployFormData>) => {
      dispatch({
        type: SHARED_ACTION_TYPES.UPDATE_FORM_DATA,
        payload: updates,
      });
    },
    [dispatch],
  );

  const handleEditingChange = useCallback(
    (isEditing: boolean) => {
      dispatch({
        type: SHARED_ACTION_TYPES.SET_IS_EDITING,
        payload: isEditing,
      });
    },
    [dispatch],
  );

  const handleResourceStatusChange = useCallback(
    (hasInsufficientResources: boolean) => {
      dispatch({
        type: SHARED_ACTION_TYPES.SET_HAS_INSUFFICIENT_RESOURCES,
        payload: hasInsufficientResources,
      });
    },
    [dispatch],
  );

  const handleBack = useCallback(() => {
    if (currentStep > 0) {
      dispatch({
        type: SHARED_ACTION_TYPES.SET_CURRENT_STEP,
        payload: currentStep - 1,
      });
    }
  }, [currentStep, dispatch]);

  return {
    handleFormDataChange,
    handleEditingChange,
    handleResourceStatusChange,
    handleBack,
  };
}
