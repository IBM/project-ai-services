import type { DeploymentDetails } from "@/types/digitalAssistants";

// State type
export interface ServicesState {
  selectedTabIndex: number;
  selectedServiceId: string | null;
  isPanelOpen: boolean;
  isDeployFlowOpen: boolean;
  deployServiceId: string | null;
  tableRefreshTrigger: number;
  // DeploymentDetails state
  selectedDeployment: DeploymentDetails | null;
  showDeploymentDetails: boolean;
}

// Action types
export type ServicesAction =
  | { type: "SET_SELECTED_TAB"; payload: number }
  | { type: "OPEN_PANEL"; payload: string }
  | { type: "CLOSE_PANEL" }
  | { type: "OPEN_DEPLOY_FLOW"; payload: string | null }
  | { type: "CLOSE_DEPLOY_FLOW" }
  | { type: "CLEAR_DEPLOY_SERVICE_ID" }
  | { type: "DEPLOY_SUBMIT" }
  | { type: "CLEAR_SELECTED_SERVICE_ID" }
  | { type: "SHOW_DEPLOYMENT_DETAILS"; payload: DeploymentDetails }
  | { type: "HIDE_DEPLOYMENT_DETAILS" };

// Initial state
export const initialState: ServicesState = {
  selectedTabIndex: 0,
  selectedServiceId: null,
  isPanelOpen: false,
  isDeployFlowOpen: false,
  deployServiceId: null,
  tableRefreshTrigger: 0,
  // DeploymentDetails state
  selectedDeployment: null,
  showDeploymentDetails: false,
};

// Reducer function
export const servicesReducer = (
  state: ServicesState,
  action: ServicesAction,
): ServicesState => {
  switch (action.type) {
    case "SET_SELECTED_TAB":
      return { ...state, selectedTabIndex: action.payload };
    case "OPEN_PANEL":
      return { ...state, selectedServiceId: action.payload, isPanelOpen: true };
    case "CLOSE_PANEL":
      return { ...state, isPanelOpen: false };
    case "OPEN_DEPLOY_FLOW":
      return {
        ...state,
        deployServiceId: action.payload,
        isDeployFlowOpen: true,
      };
    case "CLOSE_DEPLOY_FLOW":
      return { ...state, isDeployFlowOpen: false };
    case "CLEAR_DEPLOY_SERVICE_ID":
      return { ...state, deployServiceId: null };
    case "DEPLOY_SUBMIT":
      return {
        ...state,
        tableRefreshTrigger: state.tableRefreshTrigger + 1,
        selectedTabIndex: 0,
      };
    case "CLEAR_SELECTED_SERVICE_ID":
      return { ...state, selectedServiceId: null };
    case "SHOW_DEPLOYMENT_DETAILS":
      return {
        ...state,
        selectedDeployment: action.payload,
        showDeploymentDetails: true,
      };
    case "HIDE_DEPLOYMENT_DETAILS":
      return {
        ...state,
        selectedDeployment: null,
        showDeploymentDetails: false,
      };
    default:
      return state;
  }
};
