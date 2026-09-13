import type {
  ServiceDeployOptions,
  LLMOption,
  WorkerApiResponse,
} from "@/types/api.types";

import { SHARED_ACTION_TYPES } from "../Shared/types";
import type {
  BaseStepProps,
  BaseDeployFlowProps,
  BaseDeployFlowState,
  SharedDeployFlowAction,
} from "../Shared/types";

export interface ServicesDeployFlowProps extends BaseDeployFlowProps {
  preSelectedServiceId?: string;
}

export interface DeployFlowState extends BaseDeployFlowState {
  selectedServiceId: string | null;
  hasDatasourceStepError: boolean;
  showDatasourceSelectionError: boolean;
}

export const ACTION_TYPES = {
  ...SHARED_ACTION_TYPES,
  RESET_STATE: "RESET_STATE",
  SET_SELECTED_SERVICE: "SET_SELECTED_SERVICE",
  SET_DATASOURCE_STEP_ERROR: "SET_DATASOURCE_STEP_ERROR",
  SET_SHOW_DATASOURCE_SELECTION_ERROR: "SET_SHOW_DATASOURCE_SELECTION_ERROR",
} as const;

export type DeployFlowAction =
  | SharedDeployFlowAction
  | { type: typeof ACTION_TYPES.RESET_STATE }
  | { type: typeof ACTION_TYPES.SET_SELECTED_SERVICE; payload: string | null }
  | {
      type: typeof ACTION_TYPES.SET_DATASOURCE_STEP_ERROR;
      payload: boolean;
    }
  | {
      type: typeof ACTION_TYPES.SET_SHOW_DATASOURCE_SELECTION_ERROR;
      payload: boolean;
    };

export interface StepProps extends BaseStepProps {
  deployOptions: ServiceDeployOptions;
  selectedServiceId?: string | null;
  llmModelsWithProviders?: LLMOption[];
  serviceDescription?: string;
  isLoadingLlmModels?: boolean;
  runtime?: string;
  workers?: WorkerApiResponse[];
  isLoadingWorkers?: boolean;
  refetchWorkers?: () => void;
}
