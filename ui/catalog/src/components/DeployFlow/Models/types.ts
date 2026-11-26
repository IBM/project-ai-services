import { SHARED_ACTION_TYPES } from "../Shared/types";
import type {
  BaseDeployFlowProps,
  BaseDeployFlowState,
  SharedDeployFlowAction,
} from "../Shared/types";

export interface ModelsDeployFlowProps extends BaseDeployFlowProps {
  preSelectedModelId?: string;
}

export interface ModelConfig {
  inferenceBackendId: string;
  resourceProfileId: string;
  params: Record<string, unknown>;
}

export interface ModelsDeployFlowState
  extends BaseDeployFlowState, ModelConfig {
  selectedModelId: string | null;
}

export const ACTION_TYPES = {
  ...SHARED_ACTION_TYPES,
  RESET_STATE: "RESET_STATE",
  SET_SELECTED_MODEL: "SET_SELECTED_MODEL",
  SET_MODEL_CONFIG: "SET_MODEL_CONFIG",
} as const;

export type ModelsDeployFlowAction =
  | SharedDeployFlowAction
  | { type: typeof ACTION_TYPES.RESET_STATE }
  | { type: typeof ACTION_TYPES.SET_SELECTED_MODEL; payload: string | null }
  | { type: typeof ACTION_TYPES.SET_MODEL_CONFIG; payload: ModelConfig };
