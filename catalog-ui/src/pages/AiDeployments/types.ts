import type { DataTableHeader } from "@carbon/react";

export interface AiDeploymentRow {
  id: string;
  name: string;
  status: "Deploying..." | "Deleting..." | "Error" | "Stopped" | "Running";
  uptime: string;
  type: string;
  messages: string;
  actions: string;
}

export type ExportStatus = "idle" | "exporting" | "success" | "error";

export interface AppState {
  search: string;
  page: number;
  pageSize: number;
  isDeleteDialogOpen: boolean;
  isConfirmed: boolean;
  rowsData: AiDeploymentRow[];
  selectedRowId: string | null;
  toastOpen: boolean;
  deleteErrorMessage: string;
  deleteErrorRowName: string;
  isDeleting: boolean;
  isExportDialogOpen: boolean;
  csvFileName: string;
  exportStatus: ExportStatus;
  exportErrorMessage: string;
  hasError: boolean;
  visibleColumns: Record<string, boolean>;
  filters: {
    architectures: string[];
    services: string[];
  };
  pendingFilters: {
    architectures: string[];
    services: string[];
  };
}

export const ACTION_TYPES = {
  SET_SEARCH: "SET_SEARCH",
  SET_PAGE: "SET_PAGE",
  SET_PAGE_SIZE: "SET_PAGE_SIZE",
  OPEN_DELETE_DIALOG: "OPEN_DELETE_DIALOG",
  CLOSE_DELETE_DIALOG: "CLOSE_DELETE_DIALOG",
  SET_CONFIRMED: "SET_CONFIRMED",
  DELETE_ROW: "DELETE_ROW",
  SHOW_ERROR: "SHOW_ERROR",
  HIDE_ERROR: "HIDE_ERROR",
  SET_IS_DELETING: "SET_IS_DELETING",
  OPEN_EXPORT_DIALOG: "OPEN_EXPORT_DIALOG",
  CLOSE_EXPORT_DIALOG: "CLOSE_EXPORT_DIALOG",
  SET_CSV_FILENAME: "SET_CSV_FILENAME",
  SET_EXPORT_STATUS: "SET_EXPORT_STATUS",
  SET_EXPORT_ERROR: "SET_EXPORT_ERROR",
  CLEAR_EXPORT_ERROR: "CLEAR_EXPORT_ERROR",
  SET_SELECTED_ROW_ID: "SET_SELECTED_ROW_ID",
  TOGGLE_COLUMN_VISIBILITY: "TOGGLE_COLUMN_VISIBILITY",
  RESET_COLUMN_VISIBILITY: "RESET_COLUMN_VISIBILITY",
  SET_PENDING_FILTER: "SET_PENDING_FILTER",
  APPLY_FILTERS: "APPLY_FILTERS",
  RESET_FILTERS: "RESET_FILTERS",
} as const;

export type AppAction =
  | { type: typeof ACTION_TYPES.SET_SEARCH; payload: string }
  | { type: typeof ACTION_TYPES.SET_PAGE; payload: number }
  | { type: typeof ACTION_TYPES.SET_PAGE_SIZE; payload: number }
  | { type: typeof ACTION_TYPES.OPEN_DELETE_DIALOG; payload: string }
  | { type: typeof ACTION_TYPES.CLOSE_DELETE_DIALOG }
  | { type: typeof ACTION_TYPES.SET_CONFIRMED; payload: boolean }
  | { type: typeof ACTION_TYPES.DELETE_ROW; payload: string }
  | {
      type: typeof ACTION_TYPES.SHOW_ERROR;
      payload: { message: string; rowName?: string };
    }
  | { type: typeof ACTION_TYPES.HIDE_ERROR }
  | { type: typeof ACTION_TYPES.SET_IS_DELETING; payload: boolean }
  | { type: typeof ACTION_TYPES.OPEN_EXPORT_DIALOG }
  | { type: typeof ACTION_TYPES.CLOSE_EXPORT_DIALOG }
  | { type: typeof ACTION_TYPES.SET_CSV_FILENAME; payload: string }
  | { type: typeof ACTION_TYPES.SET_EXPORT_STATUS; payload: ExportStatus }
  | { type: typeof ACTION_TYPES.SET_EXPORT_ERROR; payload: string }
  | { type: typeof ACTION_TYPES.CLEAR_EXPORT_ERROR }
  | { type: typeof ACTION_TYPES.SET_SELECTED_ROW_ID; payload: string | null }
  | { type: typeof ACTION_TYPES.SET_IS_DELETING; payload: boolean }
  | { type: typeof ACTION_TYPES.TOGGLE_COLUMN_VISIBILITY; payload: string }
  | { type: typeof ACTION_TYPES.RESET_COLUMN_VISIBILITY }
  | {
      type: typeof ACTION_TYPES.SET_PENDING_FILTER;
      payload: { category: "architectures" | "services"; value: string };
    }
  | { type: typeof ACTION_TYPES.APPLY_FILTERS }
  | { type: typeof ACTION_TYPES.RESET_FILTERS };

// Table headers
export const HEADERS: DataTableHeader[] = [
  { header: "Deployment name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Uptime", key: "uptime" },
  { header: "Type", key: "type" },
  { header: "Messages", key: "messages" },
  { header: "", key: "actions" },
];

// Status Column sort order
export const STATUS_SORT_ORDER: Record<string, number> = {
  "Deploying...": 1,
  "Deleting...": 2,
  Error: 3,
  Stopped: 4,
  Running: 5,
};

// Mock data
export const MOCK_ROWS: AiDeploymentRow[] = [
  {
    id: "1",
    name: "Incident troubleshooting",
    status: "Deploying...",
    uptime: "Mar 4, 2026",
    type: "Digital assistant",
    messages: "Error message goes here...",
    actions: "actions",
  },
  {
    id: "2",
    name: "Process FAQs",
    status: "Deleting...",
    uptime: "2 days",
    type: "Deep process",
    messages: "Deploying [service]...",
    actions: "actions",
  },
  {
    id: "3",
    name: "Permission requests",
    status: "Error",
    uptime: "Mar 4, 2026",
    type: "Digital assistant",
    messages: "",
    actions: "actions",
  },
  {
    id: "4",
    name: "Contract analysis agent",
    status: "Running",
    uptime: "Mar 4, 2026",
    type: "Summary",
    messages: "",
    actions: "actions",
  },
  {
    id: "5",
    name: "Case routing",
    status: "Running",
    uptime: "12 hours",
    type: "Translation",
    messages: "Ingest data",
    actions: "actions",
  },
  {
    id: "6",
    name: "Deals tracker",
    status: "Stopped",
    uptime: "12 hours",
    type: "Digital assistant",
    messages: "",
    actions: "actions",
  },
  {
    id: "7",
    name: "Privacy, redaction, audit",
    status: "Running",
    uptime: "Jan 2, 2026",
    type: "Question and an...",
    messages: "",
    actions: "actions",
  },
  {
    id: "8",
    name: "IT support triage",
    status: "Running",
    uptime: "25 minutes",
    type: "Digital assistant",
    messages: "",
    actions: "actions",
  },
  {
    id: "9",
    name: "Sales deck generator",
    status: "Running",
    uptime: "Nov 9, 2025",
    type: "Digital assistant",
    messages: "",
    actions: "actions",
  },
];

// Initial state
export const INITIAL_STATE: AppState = {
  search: "",
  page: 1,
  pageSize: 10,
  isDeleteDialogOpen: false,
  isConfirmed: false,
  rowsData: [...MOCK_ROWS].sort(
    (a, b) => STATUS_SORT_ORDER[a.status] - STATUS_SORT_ORDER[b.status],
  ),
  selectedRowId: null,
  toastOpen: false,
  deleteErrorMessage: "",
  deleteErrorRowName: "",
  isDeleting: false,
  hasError: false,
  isExportDialogOpen: false,
  csvFileName: "",
  exportStatus: "idle",
  exportErrorMessage: "",
  visibleColumns: {
    name: true,
    status: true,
    uptime: true,
    type: true,
    messages: true,
  },
  filters: {
    architectures: [],
    services: [],
  },
  pendingFilters: {
    architectures: [],
    services: [],
  },
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
        selectedRowId: state.hasError ? state.selectedRowId : null,
      };
    case ACTION_TYPES.SET_CONFIRMED:
      return { ...state, isConfirmed: action.payload };
    case ACTION_TYPES.DELETE_ROW:
      return {
        ...state,
        rowsData: state.rowsData.filter((r) => r.id !== action.payload),
        isDeleteDialogOpen: false,
        isConfirmed: false,
      };
    case ACTION_TYPES.SHOW_ERROR:
      return {
        ...state,
        deleteErrorMessage: action.payload.message,
        deleteErrorRowName: action.payload.rowName ?? "",
        toastOpen: true,
        isDeleting: false,
        hasError: true,
      };
    case ACTION_TYPES.HIDE_ERROR:
      return {
        ...state,
        toastOpen: false,
        selectedRowId: null,
        hasError: false,
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
        exportStatus: "idle",
      };
    case ACTION_TYPES.CLOSE_EXPORT_DIALOG:
      return {
        ...state,
        isExportDialogOpen: false,
      };
    case ACTION_TYPES.SET_CSV_FILENAME:
      return { ...state, csvFileName: action.payload };
    case ACTION_TYPES.SET_EXPORT_STATUS:
      return { ...state, exportStatus: action.payload };
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
          type: true,
          messages: true,
        },
      };
    case ACTION_TYPES.SET_PENDING_FILTER: {
      const { category, value } = action.payload;
      const currentFilters = state.pendingFilters[category];
      const newFilters = currentFilters.includes(value)
        ? currentFilters.filter((v) => v !== value)
        : [...currentFilters, value];
      return {
        ...state,
        pendingFilters: {
          ...state.pendingFilters,
          [category]: newFilters,
        },
      };
    }
    case ACTION_TYPES.APPLY_FILTERS:
      return {
        ...state,
        filters: {
          architectures: [...state.pendingFilters.architectures],
          services: [...state.pendingFilters.services],
        },
      };
    case ACTION_TYPES.RESET_FILTERS:
      return {
        ...state,
        filters: {
          architectures: [],
          services: [],
        },
        pendingFilters: {
          architectures: [],
          services: [],
        },
      };
    default:
      return state;
  }
};
