import type { DeployOptionsResponse } from "@/types/api.types";

import { SHARED_ACTION_TYPES } from "../Shared/types";
import type {
  ServiceConfig as BaseServiceConfig,
  BaseStepProps,
  BaseDeployFlowState,
  SharedDeployFlowAction,
} from "../Shared/types";

// DA-only extension of ServiceConfig — inferenceBackend removed in PR 9.
export interface ServiceConfig extends BaseServiceConfig {
  inferenceBackend?: string;
}

export interface DeployFlowState extends BaseDeployFlowState {
  isLoading: boolean;
  error: string | null;
}

export const ACTION_TYPES = {
  ...SHARED_ACTION_TYPES,
  RESET_STATE: "RESET_STATE",
  SET_IS_LOADING: "SET_IS_LOADING",
  SET_ERROR: "SET_ERROR",
} as const;

export type DeployFlowAction =
  | SharedDeployFlowAction
  | { type: typeof ACTION_TYPES.RESET_STATE }
  | { type: typeof ACTION_TYPES.SET_IS_LOADING; payload: boolean }
  | { type: typeof ACTION_TYPES.SET_ERROR; payload: string | null };

export interface StepProps extends BaseStepProps {
  deployOptions: DeployOptionsResponse;
}
