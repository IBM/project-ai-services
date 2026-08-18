import type { DataTableHeader } from "@carbon/react";
import type { PaginationMetadata, DeploymentDetails } from "@/types/api.types";
import type {
  BaseTableState,
  SharedTableAction,
} from "@/components/Table/types";
import {
  handleSharedTableAction,
  isSharedTableAction,
  setLoading,
} from "@/components/Table/utils/reducerUtils";

export interface DigitalAssistantRow {
  id: string;
  name: string;
  status:
    | "Downloading"
    | "Deploying"
    | "Running"
    | "Deleting"
    | "Error"
    | "Initializing";
  type?: string;
  uptime: string;
  messages: string;
  actions: string;
  children?: DigitalAssistantRow[];
}

export interface AppState extends BaseTableState<DigitalAssistantRow> {
  // rowsData: DigitalAssistantRow[] inherited from BaseTableState
  // DA-specific: deploy flow
  isDeployFlowOpen: boolean;
  // DA-specific: full pagination metadata from API
  pagination: PaginationMetadata | null;
  // DA-specific: deployment details panel
  selectedDeployment: DeploymentDetails | null;
  showDeploymentDetails: boolean;
}

export const ACTION_TYPES = {
  OPEN_DEPLOY_FLOW: "OPEN_DEPLOY_FLOW",
  CLOSE_DEPLOY_FLOW: "CLOSE_DEPLOY_FLOW",
  // API actions
  FETCH_APPLICATIONS_SUCCESS: "FETCH_APPLICATIONS_SUCCESS",
  // DeploymentDetails actions
  SHOW_DEPLOYMENT_DETAILS: "SHOW_DEPLOYMENT_DETAILS",
  HIDE_DEPLOYMENT_DETAILS: "HIDE_DEPLOYMENT_DETAILS",
  UPDATE_DEPLOYMENT_NAME: "UPDATE_DEPLOYMENT_NAME",
} as const;

// DA-specific actions only. Shared actions (search, pagination, export, columns,
// delete dialog, etc.) are dispatched as SharedTableAction and handled by
// handleSharedTableAction in the reducer below.
export type AppAction =
  | { type: typeof ACTION_TYPES.OPEN_DEPLOY_FLOW }
  | { type: typeof ACTION_TYPES.CLOSE_DEPLOY_FLOW }
  | {
      type: typeof ACTION_TYPES.FETCH_APPLICATIONS_SUCCESS;
      payload: {
        rows: DigitalAssistantRow[];
        pagination: PaginationMetadata;
      };
    }
  | {
      type: typeof ACTION_TYPES.SHOW_DEPLOYMENT_DETAILS;
      payload: DeploymentDetails;
    }
  | { type: typeof ACTION_TYPES.HIDE_DEPLOYMENT_DETAILS }
  | { type: typeof ACTION_TYPES.UPDATE_DEPLOYMENT_NAME; payload: string };

// Table headers
export const HEADERS: DataTableHeader[] = [
  { header: "Name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Uptime", key: "uptime" },
  { header: "Messages", key: "messages" },
  { header: "", key: "actions" },
];

export const DEFAULT_VISIBLE_COLUMNS: Record<string, boolean> = {
  name: true,
  status: true,
  uptime: true,
  messages: true,
};

// Initial state
export const INITIAL_STATE: AppState = {
  search: "",
  page: 1,
  pageSize: 20,
  totalItems: 0,
  isDeleteDialogOpen: false,
  isConfirmed: false,
  rowsData: [],
  selectedRowId: null,
  toastOpen: false,
  deleteErrorMessage: "",
  deleteErrorRowName: "",
  isDeleting: false,
  isExportDialogOpen: false,
  isExporting: false,
  csvFileName: "",
  exportErrorMessage: "",
  visibleColumns: { ...DEFAULT_VISIBLE_COLUMNS },
  exportToastOpen: false,
  exportToastMessage: "",
  exportToastKind: "success",
  isDeployFlowOpen: false,
  // BaseTableState fields
  isLoading: false,
  fetchError: null,
  hasError: false,
  // DA-specific
  pagination: null,
  selectedDeployment: null,
  showDeploymentDetails: false,
};

// DA-specific cases only. All shared cases are handled by handleSharedTableAction.
function ownCases(state: AppState, action: AppAction): AppState {
  switch (action.type) {
    case ACTION_TYPES.OPEN_DEPLOY_FLOW:
      return { ...state, isDeployFlowOpen: true };
    case ACTION_TYPES.CLOSE_DEPLOY_FLOW:
      return { ...state, isDeployFlowOpen: false };
    case ACTION_TYPES.FETCH_APPLICATIONS_SUCCESS:
      return {
        ...state,
        ...setLoading(false),
        rowsData: action.payload.rows,
        pagination: action.payload.pagination,
        totalItems: action.payload.pagination.total_items,
        fetchError: null,
      };
    case ACTION_TYPES.SHOW_DEPLOYMENT_DETAILS:
      return {
        ...state,
        selectedDeployment: action.payload,
        showDeploymentDetails: true,
      };
    case ACTION_TYPES.HIDE_DEPLOYMENT_DETAILS:
      return {
        ...state,
        selectedDeployment: null,
        showDeploymentDetails: false,
      };
    case ACTION_TYPES.UPDATE_DEPLOYMENT_NAME:
      return {
        ...state,
        selectedDeployment: state.selectedDeployment
          ? { ...state.selectedDeployment, name: action.payload }
          : null,
      };
    default:
      return state;
  }
}

// Reducer — shared actions are narrowed via isSharedTableAction; all other
// actions are handled by ownCases.
export const appReducer = (
  state: AppState,
  action: AppAction | SharedTableAction,
): AppState => {
  if (isSharedTableAction(action)) {
    return handleSharedTableAction(state, action) ?? state;
  }
  return ownCases(state, action);
};
