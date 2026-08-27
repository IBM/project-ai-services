import type { RegisterPhase } from "@/components/WorkerResourcesTable/types";

export interface WorkerResourcesState {
  isModalOpen: boolean;
  refreshTrigger: number;
  phase: RegisterPhase;
  workerName: string;
  token: string;
  errorMessage: string;
}

export type WorkerResourcesAction =
  | { type: "OPEN_MODAL" }
  | { type: "CLOSE_MODAL" }
  | { type: "SET_WORKER_NAME"; payload: string }
  | { type: "SET_PHASE"; payload: RegisterPhase }
  | { type: "REGISTER_SUCCESS"; payload: { token: string } }
  | { type: "REGISTER_ERROR"; payload: string };

export const initialState: WorkerResourcesState = {
  isModalOpen: false,
  refreshTrigger: 0,
  phase: "idle",
  workerName: "",
  token: "",
  errorMessage: "",
};

export const workerResourcesReducer = (
  state: WorkerResourcesState,
  action: WorkerResourcesAction,
): WorkerResourcesState => {
  switch (action.type) {
    case "OPEN_MODAL":
      return {
        ...state,
        isModalOpen: true,
        phase: "idle",
        workerName: "",
        token: "",
        errorMessage: "",
      };
    case "CLOSE_MODAL":
      return {
        ...initialState,
        isModalOpen: false,
        refreshTrigger: state.refreshTrigger,
      };
    case "SET_WORKER_NAME":
      return {
        ...state,
        workerName: action.payload,
        phase: state.phase === "invalid" ? "idle" : state.phase,
      };
    case "SET_PHASE":
      return { ...state, phase: action.payload };
    case "REGISTER_SUCCESS":
      return {
        ...state,
        phase: "success",
        token: action.payload.token,
        refreshTrigger: state.refreshTrigger + 1,
      };
    case "REGISTER_ERROR":
      return { ...state, phase: "error", errorMessage: action.payload };
    default:
      return state;
  }
};
