import type { DeployOptionsResponse, ProviderSchema } from "@/types/api.types";

import { SHARED_ACTION_TYPES } from "../Shared/types";
import type {
  ServiceConfig as BaseServiceConfig,
  BaseStepProps,
  SharedDeployFlowAction,
} from "../Shared/types";

// DA-only extension of ServiceConfig — inferenceBackend removed in PR 9.
export interface ServiceConfig extends BaseServiceConfig {
  inferenceBackend?: string;
}

export const ACTION_TYPES = {
  ...SHARED_ACTION_TYPES,
  RESET_STATE: "RESET_STATE",
} as const;

export type DeployFlowAction =
  | SharedDeployFlowAction
  | { type: typeof ACTION_TYPES.RESET_STATE };

export interface StepProps extends BaseStepProps {
  deployOptions: DeployOptionsResponse;
  providerParamsByType: Record<string, Record<string, ProviderSchema>>;
}
