import type { DataTableHeader } from "@carbon/react";
import type {
  PaginationMetadata,
  DeploymentDetails,
} from "@/types/digitalAssistants";

export interface DigitalAssistantRow {
  id: string;
  name: string;
  status: "Deploying..." | "Deleting..." | "Error" | "Stopped" | "Running";
  type?: string;
  uptime: string;
  messages: string;
  actions: string;
  children?: DigitalAssistantRow[];
}

export interface AppState {
  search: string;
  page: number;
  pageSize: number;
  isDeleteDialogOpen: boolean;
  isConfirmed: boolean;
  rowsData: DigitalAssistantRow[];
  selectedRowId: string | null;
  toastOpen: boolean;
  deleteErrorMessage: string;
  deleteErrorRowName: string;
  isDeleting: boolean;
  isExportDialogOpen: boolean;
  csvFileName: string;
  exportErrorMessage: string;
  visibleColumns: Record<string, boolean>;
  exportToastOpen: boolean;
  exportToastMessage: string;
  exportToastKind: "success" | "error";
  isDeployFlowOpen: boolean;
  // API state
  isLoadingApplications: boolean;
  fetchError: string | null;
  pagination: PaginationMetadata | null;
  totalItems: number;
  // DeploymentDetails state
  selectedDeployment: DeploymentDetails | null;
  showDeploymentDetails: boolean;
}

export const ACTION_TYPES = {
  SET_SEARCH: "SET_SEARCH",
  SET_PAGE: "SET_PAGE",
  SET_PAGE_SIZE: "SET_PAGE_SIZE",
  OPEN_DELETE_DIALOG: "OPEN_DELETE_DIALOG",
  CLOSE_DELETE_DIALOG: "CLOSE_DELETE_DIALOG",
  SET_CONFIRMED: "SET_CONFIRMED",
  SHOW_ERROR: "SHOW_ERROR",
  HIDE_ERROR: "HIDE_ERROR",
  SET_IS_DELETING: "SET_IS_DELETING",
  OPEN_EXPORT_DIALOG: "OPEN_EXPORT_DIALOG",
  CLOSE_EXPORT_DIALOG: "CLOSE_EXPORT_DIALOG",
  SET_CSV_FILENAME: "SET_CSV_FILENAME",
  SET_EXPORT_ERROR: "SET_EXPORT_ERROR",
  CLEAR_EXPORT_ERROR: "CLEAR_EXPORT_ERROR",
  SET_SELECTED_ROW_ID: "SET_SELECTED_ROW_ID",
  TOGGLE_COLUMN_VISIBILITY: "TOGGLE_COLUMN_VISIBILITY",
  RESET_COLUMN_VISIBILITY: "RESET_COLUMN_VISIBILITY",
  SHOW_EXPORT_TOAST: "SHOW_EXPORT_TOAST",
  HIDE_EXPORT_TOAST: "HIDE_EXPORT_TOAST",
  OPEN_DEPLOY_FLOW: "OPEN_DEPLOY_FLOW",
  CLOSE_DEPLOY_FLOW: "CLOSE_DEPLOY_FLOW",
  // API actions
  FETCH_APPLICATIONS_START: "FETCH_APPLICATIONS_START",
  FETCH_APPLICATIONS_SUCCESS: "FETCH_APPLICATIONS_SUCCESS",
  FETCH_APPLICATIONS_ERROR: "FETCH_APPLICATIONS_ERROR",
  // DeploymentDetails actions
  SHOW_DEPLOYMENT_DETAILS: "SHOW_DEPLOYMENT_DETAILS",
  HIDE_DEPLOYMENT_DETAILS: "HIDE_DEPLOYMENT_DETAILS",
  UPDATE_DEPLOYMENT_NAME: "UPDATE_DEPLOYMENT_NAME",
} as const;

export type AppAction =
  | { type: typeof ACTION_TYPES.SET_SEARCH; payload: string }
  | { type: typeof ACTION_TYPES.SET_PAGE; payload: number }
  | { type: typeof ACTION_TYPES.SET_PAGE_SIZE; payload: number }
  | { type: typeof ACTION_TYPES.OPEN_DELETE_DIALOG; payload: string }
  | { type: typeof ACTION_TYPES.CLOSE_DELETE_DIALOG }
  | { type: typeof ACTION_TYPES.SET_CONFIRMED; payload: boolean }
  | {
      type: typeof ACTION_TYPES.SHOW_ERROR;
      payload: { message: string; rowName?: string };
    }
  | { type: typeof ACTION_TYPES.HIDE_ERROR }
  | { type: typeof ACTION_TYPES.SET_IS_DELETING; payload: boolean }
  | { type: typeof ACTION_TYPES.OPEN_EXPORT_DIALOG }
  | { type: typeof ACTION_TYPES.CLOSE_EXPORT_DIALOG }
  | { type: typeof ACTION_TYPES.SET_CSV_FILENAME; payload: string }
  | { type: typeof ACTION_TYPES.SET_EXPORT_ERROR; payload: string }
  | { type: typeof ACTION_TYPES.CLEAR_EXPORT_ERROR }
  | { type: typeof ACTION_TYPES.SET_SELECTED_ROW_ID; payload: string | null }
  | { type: typeof ACTION_TYPES.TOGGLE_COLUMN_VISIBILITY; payload: string }
  | { type: typeof ACTION_TYPES.RESET_COLUMN_VISIBILITY }
  | {
      type: typeof ACTION_TYPES.SHOW_EXPORT_TOAST;
      payload: { message: string; kind: "success" | "error" };
    }
  | { type: typeof ACTION_TYPES.HIDE_EXPORT_TOAST }
  | { type: typeof ACTION_TYPES.OPEN_DEPLOY_FLOW }
  | { type: typeof ACTION_TYPES.CLOSE_DEPLOY_FLOW }
  | { type: typeof ACTION_TYPES.FETCH_APPLICATIONS_START }
  | {
      type: typeof ACTION_TYPES.FETCH_APPLICATIONS_SUCCESS;
      payload: {
        rows: DigitalAssistantRow[];
        pagination: PaginationMetadata;
      };
    }
  | { type: typeof ACTION_TYPES.FETCH_APPLICATIONS_ERROR; payload: string }
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

// Initial state
export const INITIAL_STATE: AppState = {
  search: "",
  page: 1,
  pageSize: 10,
  isDeleteDialogOpen: false,
  isConfirmed: false,
  rowsData: [],
  selectedRowId: null,
  toastOpen: false,
  deleteErrorMessage: "",
  deleteErrorRowName: "",
  isDeleting: false,
  isExportDialogOpen: false,
  csvFileName: "",
  exportErrorMessage: "",
  visibleColumns: {
    name: true,
    status: true,
    uptime: true,
    messages: true,
  },
  exportToastOpen: false,
  exportToastMessage: "",
  exportToastKind: "success",
  isDeployFlowOpen: false,
  isLoadingApplications: false,
  fetchError: null,
  pagination: null,
  totalItems: 0,
  selectedDeployment: null,
  showDeploymentDetails: false,
};

// Reducer
export const appReducer = (state: AppState, action: AppAction): AppState => {
  switch (action.type) {
    case ACTION_TYPES.SET_SEARCH:
      return { ...state, search: action.payload };
    case ACTION_TYPES.SET_PAGE:
      return { ...state, page: action.payload };
    case ACTION_TYPES.SET_PAGE_SIZE:
      return { ...state, pageSize: action.payload };
    case ACTION_TYPES.OPEN_DELETE_DIALOG:
      return {
        ...state,
        selectedRowId: action.payload,
        isDeleteDialogOpen: true,
        toastOpen: false,
      };
    case ACTION_TYPES.CLOSE_DELETE_DIALOG:
      return {
        ...state,
        isDeleteDialogOpen: false,
        isConfirmed: false,
        selectedRowId: null,
      };
    case ACTION_TYPES.SET_CONFIRMED:
      return { ...state, isConfirmed: action.payload };
    case ACTION_TYPES.SHOW_ERROR:
      return {
        ...state,
        deleteErrorMessage: action.payload.message,
        deleteErrorRowName: action.payload.rowName ?? "",
        toastOpen: true,
        isDeleting: false,
      };
    case ACTION_TYPES.HIDE_ERROR:
      return {
        ...state,
        toastOpen: false,
        selectedRowId: null,
        deleteErrorRowName: "",
      };
    case ACTION_TYPES.SET_IS_DELETING:
      return { ...state, isDeleting: action.payload };
    case ACTION_TYPES.SET_SELECTED_ROW_ID:
      return { ...state, selectedRowId: action.payload };
    case ACTION_TYPES.OPEN_EXPORT_DIALOG:
      return {
        ...state,
        isExportDialogOpen: true,
        csvFileName: "",
        exportErrorMessage: "",
      };
    case ACTION_TYPES.CLOSE_EXPORT_DIALOG:
      return {
        ...state,
        isExportDialogOpen: false,
      };
    case ACTION_TYPES.SET_CSV_FILENAME:
      return { ...state, csvFileName: action.payload };
    case ACTION_TYPES.SET_EXPORT_ERROR:
      return {
        ...state,
        exportErrorMessage: action.payload,
      };
    case ACTION_TYPES.CLEAR_EXPORT_ERROR:
      return {
        ...state,
        exportErrorMessage: "",
      };
    case ACTION_TYPES.TOGGLE_COLUMN_VISIBILITY:
      return {
        ...state,
        visibleColumns: {
          ...state.visibleColumns,
          [action.payload]: !state.visibleColumns[action.payload],
        },
      };
    case ACTION_TYPES.RESET_COLUMN_VISIBILITY:
      return {
        ...state,
        visibleColumns: {
          name: true,
          status: true,
          uptime: true,
          messages: true,
        },
      };
    case ACTION_TYPES.SHOW_EXPORT_TOAST:
      return {
        ...state,
        exportToastOpen: true,
        exportToastMessage: action.payload.message,
        exportToastKind: action.payload.kind,
      };
    case ACTION_TYPES.HIDE_EXPORT_TOAST:
      return {
        ...state,
        exportToastOpen: false,
      };
    case ACTION_TYPES.OPEN_DEPLOY_FLOW:
      return {
        ...state,
        isDeployFlowOpen: true,
      };
    case ACTION_TYPES.CLOSE_DEPLOY_FLOW:
      return {
        ...state,
        isDeployFlowOpen: false,
      };
    case ACTION_TYPES.FETCH_APPLICATIONS_START:
      return {
        ...state,
        isLoadingApplications: true,
        fetchError: null,
      };
    case ACTION_TYPES.FETCH_APPLICATIONS_SUCCESS:
      return {
        ...state,
        isLoadingApplications: false,
        rowsData: action.payload.rows,
        pagination: action.payload.pagination,
        totalItems: action.payload.pagination.total_items,
        fetchError: null,
      };
    case ACTION_TYPES.FETCH_APPLICATIONS_ERROR:
      return {
        ...state,
        isLoadingApplications: false,
        fetchError: action.payload,
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
};
