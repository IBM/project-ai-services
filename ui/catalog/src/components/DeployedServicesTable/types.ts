import type { DataTableHeader } from "@carbon/react";

// API response types
export interface ApplicationService {
  type: string;
  [key: string]: unknown;
}

export interface ApplicationApiResponse {
  id: string;
  name: string;
  deployment_type: string;
  type: string;
  status: "Deploying..." | "Deleting..." | "Error" | "Stopped" | "Running";
  created_at: string;
  updated_at: string;
  message?: string;
  services?: ApplicationService[];
}

export interface DeployedServicesRow {
  id: string;
  name: string;
  status: "Deploying..." | "Deleting..." | "Error" | "Stopped" | "Running";
  type?: string;
  uptime: string;
  messages: string;
  actions: string;
  service: string;
  children?: DeployedServicesRow[];
}

export interface AppState {
  search: string;
  page: number;
  pageSize: number;
  isDeleteDialogOpen: boolean;
  isConfirmed: boolean;
  rowsData: DeployedServicesRow[];
  selectedRowId: string | null;
  toastOpen: boolean;
  deleteErrorMessage: string;
  deleteErrorRowName: string;
  isDeleting: boolean;
  isExportDialogOpen: boolean;
  csvFileName: string;
  exportErrorMessage: string;
  hasError: boolean;
  visibleColumns: Record<string, boolean>;
  exportToastOpen: boolean;
  exportToastMessage: string;
  exportToastKind: "success" | "error";
  selectedServices: string[];
  isLoading: boolean;
  fetchError: string | null;
}

export const ACTION_TYPES = {
  DEPLOYED_SERVICES_SET_SEARCH: "DEPLOYED_SERVICES_SET_SEARCH",
  DEPLOYED_SERVICES_SET_PAGE: "DEPLOYED_SERVICES_SET_PAGE",
  DEPLOYED_SERVICES_SET_PAGE_SIZE: "DEPLOYED_SERVICES_SET_PAGE_SIZE",
  DEPLOYED_SERVICES_OPEN_DELETE_DIALOG: "DEPLOYED_SERVICES_OPEN_DELETE_DIALOG",
  DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG:
    "DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG",
  DEPLOYED_SERVICES_SET_CONFIRMED: "DEPLOYED_SERVICES_SET_CONFIRMED",
  DEPLOYED_SERVICES_DELETE_ROW: "DEPLOYED_SERVICES_DELETE_ROW",
  DEPLOYED_SERVICES_SHOW_ERROR: "DEPLOYED_SERVICES_SHOW_ERROR",
  DEPLOYED_SERVICES_HIDE_ERROR: "DEPLOYED_SERVICES_HIDE_ERROR",
  DEPLOYED_SERVICES_START_DELETING: "DEPLOYED_SERVICES_START_DELETING",
  DEPLOYED_SERVICES_STOP_DELETING: "DEPLOYED_SERVICES_STOP_DELETING",
  DEPLOYED_SERVICES_OPEN_EXPORT_DIALOG: "DEPLOYED_SERVICES_OPEN_EXPORT_DIALOG",
  DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG:
    "DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG",
  DEPLOYED_SERVICES_SET_CSV_FILENAME: "DEPLOYED_SERVICES_SET_CSV_FILENAME",
  DEPLOYED_SERVICES_SET_EXPORT_ERROR: "DEPLOYED_SERVICES_SET_EXPORT_ERROR",
  DEPLOYED_SERVICES_CLEAR_EXPORT_ERROR: "DEPLOYED_SERVICES_CLEAR_EXPORT_ERROR",
  DEPLOYED_SERVICES_SET_SELECTED_ROW_ID:
    "DEPLOYED_SERVICES_SET_SELECTED_ROW_ID",
  DEPLOYED_SERVICES_TOGGLE_COLUMN_VISIBILITY:
    "DEPLOYED_SERVICES_TOGGLE_COLUMN_VISIBILITY",
  DEPLOYED_SERVICES_RESET_COLUMN_VISIBILITY:
    "DEPLOYED_SERVICES_RESET_COLUMN_VISIBILITY",
  DEPLOYED_SERVICES_SHOW_EXPORT_TOAST: "DEPLOYED_SERVICES_SHOW_EXPORT_TOAST",
  DEPLOYED_SERVICES_HIDE_EXPORT_TOAST: "DEPLOYED_SERVICES_HIDE_EXPORT_TOAST",
  DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER:
    "DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER",
  DEPLOYED_SERVICES_RESET_SERVICE_FILTER:
    "DEPLOYED_SERVICES_RESET_SERVICE_FILTER",
  DEPLOYED_SERVICES_SET_ROWS_DATA: "DEPLOYED_SERVICES_SET_ROWS_DATA",
  DEPLOYED_SERVICES_UPDATE_ROW_STATUS: "DEPLOYED_SERVICES_UPDATE_ROW_STATUS",
  DEPLOYED_SERVICES_SET_LOADING: "DEPLOYED_SERVICES_SET_LOADING",
  DEPLOYED_SERVICES_SET_FETCH_ERROR: "DEPLOYED_SERVICES_SET_FETCH_ERROR",
} as const;

export type AppAction =
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_SEARCH; payload: string }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE; payload: number }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE_SIZE;
      payload: number;
    }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_OPEN_DELETE_DIALOG;
      payload: string;
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_CONFIRMED;
      payload: boolean;
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_DELETE_ROW; payload: string }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SHOW_ERROR;
      payload: { message: string; rowName?: string };
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_HIDE_ERROR }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_START_DELETING }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_STOP_DELETING }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_OPEN_EXPORT_DIALOG }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_CSV_FILENAME;
      payload: string;
    }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORT_ERROR;
      payload: string;
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_CLEAR_EXPORT_ERROR }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_SELECTED_ROW_ID;
      payload: string | null;
    }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_COLUMN_VISIBILITY;
      payload: string;
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_RESET_COLUMN_VISIBILITY }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SHOW_EXPORT_TOAST;
      payload: { message: string; kind: "success" | "error" };
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_HIDE_EXPORT_TOAST }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER;
      payload: string;
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_RESET_SERVICE_FILTER }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_ROWS_DATA;
      payload: DeployedServicesRow[];
    }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_UPDATE_ROW_STATUS;
      payload: {
        id: string;
        status: DeployedServicesRow["status"];
        message?: string;
      };
    }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_LOADING;
      payload: boolean;
    }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_FETCH_ERROR;
      payload: string | null;
    };

// Table headers - Messages column moved after Service
export const HEADERS: DataTableHeader[] = [
  { header: "Name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Uptime", key: "uptime" },
  { header: "Service", key: "service" },
  { header: "Messages", key: "messages" },
  { header: "", key: "actions" },
];

// Service filter constants
export const SERVICE_TYPES = {
  DIGITIZE_DOCUMENTS: "Digitize Documents",
  FIND_SIMILAR_ITEMS: "Find Similar Items",
  QUESTION_AND_ANSWER: "Question and Answer",
  SUMMARIZE: "Summarize",
} as const;

// Status Column sort order
export const STATUS_SORT_ORDER: Record<string, number> = {
  "Deploying...": 1,
  "Deleting...": 2,
  Error: 3,
  Stopped: 4,
  Running: 5,
};

// Initial state
export const INITIAL_STATE: AppState = {
  search: "",
  page: 1,
  pageSize: 10,
  isDeleteDialogOpen: false,
  isConfirmed: false,
  // rowsData: [...MOCK_ROWS].sort(
  //   (a, b) => STATUS_SORT_ORDER[a.status] - STATUS_SORT_ORDER[b.status],
  // ),
  rowsData: [],
  selectedRowId: null,
  toastOpen: false,
  deleteErrorMessage: "",
  deleteErrorRowName: "",
  isDeleting: false,
  hasError: false,
  isExportDialogOpen: false,
  csvFileName: "",
  exportErrorMessage: "",
  visibleColumns: {
    name: true,
    status: true,
    uptime: true,
    messages: true,
    service: true,
  },
  exportToastOpen: false,
  exportToastMessage: "",
  exportToastKind: "success",
  selectedServices: [],
  isLoading: true,
  fetchError: null,
};

// Reducer
export const appReducer = (state: AppState, action: AppAction): AppState => {
  switch (action.type) {
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_SEARCH:
      return { ...state, search: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE:
      return { ...state, page: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE_SIZE:
      return { ...state, pageSize: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_OPEN_DELETE_DIALOG:
      return {
        ...state,
        selectedRowId: action.payload,
        isDeleteDialogOpen: true,
        toastOpen: false,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG:
      return {
        ...state,
        isDeleteDialogOpen: false,
        isConfirmed: false,
        selectedRowId: state.hasError ? state.selectedRowId : null,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_CONFIRMED:
      return { ...state, isConfirmed: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_DELETE_ROW:
      return {
        ...state,
        rowsData: state.rowsData.filter((r) => r.id !== action.payload),
        isDeleteDialogOpen: false,
        isConfirmed: false,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_SHOW_ERROR:
      return {
        ...state,
        deleteErrorMessage: action.payload.message,
        deleteErrorRowName: action.payload.rowName ?? "",
        toastOpen: true,
        isDeleting: false,
        hasError: true,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_HIDE_ERROR:
      return {
        ...state,
        toastOpen: false,
        selectedRowId: null,
        hasError: false,
        deleteErrorRowName: "",
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_START_DELETING:
      return { ...state, isDeleting: true };
    case ACTION_TYPES.DEPLOYED_SERVICES_STOP_DELETING:
      return { ...state, isDeleting: false };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_SELECTED_ROW_ID:
      return { ...state, selectedRowId: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_OPEN_EXPORT_DIALOG:
      return {
        ...state,
        isExportDialogOpen: true,
        csvFileName: "",
        exportErrorMessage: "",
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG:
      return {
        ...state,
        isExportDialogOpen: false,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_CSV_FILENAME:
      return { ...state, csvFileName: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORT_ERROR:
      return {
        ...state,
        exportErrorMessage: action.payload,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_CLEAR_EXPORT_ERROR:
      return {
        ...state,
        exportErrorMessage: "",
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_COLUMN_VISIBILITY:
      return {
        ...state,
        visibleColumns: {
          ...state.visibleColumns,
          [action.payload]: !state.visibleColumns[action.payload],
        },
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_RESET_COLUMN_VISIBILITY:
      return {
        ...state,
        visibleColumns: {
          name: true,
          status: true,
          uptime: true,
          messages: true,
          service: true,
        },
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_SHOW_EXPORT_TOAST:
      return {
        ...state,
        exportToastOpen: true,
        exportToastMessage: action.payload.message,
        exportToastKind: action.payload.kind,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_HIDE_EXPORT_TOAST:
      return {
        ...state,
        exportToastOpen: false,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER:
      return {
        ...state,
        selectedServices: state.selectedServices.includes(action.payload)
          ? state.selectedServices.filter((s) => s !== action.payload)
          : [...state.selectedServices, action.payload],
        page: 1,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_RESET_SERVICE_FILTER:
      return {
        ...state,
        selectedServices: [],
        page: 1,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_ROWS_DATA:
      return {
        ...state,
        rowsData: action.payload.sort(
          (a, b) => STATUS_SORT_ORDER[a.status] - STATUS_SORT_ORDER[b.status],
        ),
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_UPDATE_ROW_STATUS:
      return {
        ...state,
        rowsData: state.rowsData
          .map((r) =>
            r.id === action.payload.id
              ? {
                  ...r,
                  status: action.payload.status,
                  messages:
                    action.payload.message !== undefined
                      ? action.payload.message
                      : r.messages,
                }
              : r,
          )
          .sort(
            (a, b) => STATUS_SORT_ORDER[a.status] - STATUS_SORT_ORDER[b.status],
          ),
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_LOADING:
      return { ...state, isLoading: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_FETCH_ERROR:
      return { ...state, fetchError: action.payload, isLoading: false };
    default:
      return state;
  }
};
