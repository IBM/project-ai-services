import type { DataTableHeader } from "@carbon/react";
import type {
  BaseTableState,
  SharedTableAction,
} from "@/components/Table/types";
import {
  handleSharedTableAction,
  isSharedTableAction,
} from "@/components/Table/utils/reducerUtils";

export interface DeployedServicesRow {
  id: string;
  name: string;
  status: "Downloading" | "Deploying" | "Running" | "Deleting" | "Error";
  type?: string;
  uptime: string;
  workerResource: string;
  workerType: string;
  messages: string;
  actions: string;
  service: string;
  children?: DeployedServicesRow[];
}

export interface AppState extends BaseTableState<DeployedServicesRow> {
  // rowsData: DeployedServicesRow[] inherited from BaseTableState
  // DS-specific: service filter
  selectedServices: string[];
}

export const ACTION_TYPES = {
  // DS-specific only — shared actions (search, pagination, export, columns,
  // delete dialog, etc.) are dispatched as SharedTableAction
  DEPLOYED_SERVICES_SET_TOTAL_ITEMS: "DEPLOYED_SERVICES_SET_TOTAL_ITEMS",
  DEPLOYED_SERVICES_DELETE_ROW: "DEPLOYED_SERVICES_DELETE_ROW",
  DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER:
    "DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER",
  DEPLOYED_SERVICES_RESET_SERVICE_FILTER:
    "DEPLOYED_SERVICES_RESET_SERVICE_FILTER",
  DEPLOYED_SERVICES_SET_ROWS_DATA: "DEPLOYED_SERVICES_SET_ROWS_DATA",
} as const;

export type AppAction =
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_TOTAL_ITEMS;
      payload: number;
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_DELETE_ROW; payload: string }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER;
      payload: string;
    }
  | { type: typeof ACTION_TYPES.DEPLOYED_SERVICES_RESET_SERVICE_FILTER }
  | {
      type: typeof ACTION_TYPES.DEPLOYED_SERVICES_SET_ROWS_DATA;
      payload: DeployedServicesRow[];
    };

// Table headers
export const HEADERS: DataTableHeader[] = [
  { header: "Name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Uptime", key: "uptime" },
  { header: "Worker resource", key: "workerResource" },
  { header: "Worker type", key: "workerType" },
  { header: "Service", key: "service" },
  { header: "Messages", key: "messages" },
  { header: "", key: "actions" },
];

// Status Column sort order
export const STATUS_SORT_ORDER: Record<string, number> = {
  Downloading: 1,
  Deploying: 2,
  Deleting: 3,
  Error: 4,
  Running: 5,
};

export const DEFAULT_VISIBLE_COLUMNS: Record<string, boolean> = {
  name: true,
  status: true,
  uptime: true,
  workerResource: true,
  workerType: true,
  messages: true,
  service: true,
};

// Initial state
export const INITIAL_STATE: AppState = {
  search: "",
  page: 1,
  pageSize: 20,
  totalItems: 0,
  isDeleteDialogOpen: false,
  isConfirmed: false,
  isExporting: false,
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
  visibleColumns: { ...DEFAULT_VISIBLE_COLUMNS },
  exportToastOpen: false,
  exportToastMessage: "",
  exportToastKind: "success",
  selectedServices: [],
  isLoading: true,
  fetchError: null,
};

// DS-specific cases only. All shared cases are handled by handleSharedTableAction.
function ownCases(state: AppState, action: AppAction): AppState {
  switch (action.type) {
    case ACTION_TYPES.DEPLOYED_SERVICES_SET_TOTAL_ITEMS:
      return { ...state, totalItems: action.payload };
    case ACTION_TYPES.DEPLOYED_SERVICES_DELETE_ROW:
      return {
        ...state,
        rowsData: state.rowsData.filter((r) => r.id !== action.payload),
        isDeleteDialogOpen: false,
        isConfirmed: false,
      };
    case ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER:
      // Single-select: selecting same deselects, selecting new replaces previous
      return {
        ...state,
        selectedServices: state.selectedServices.includes(action.payload)
          ? []
          : [action.payload],
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
