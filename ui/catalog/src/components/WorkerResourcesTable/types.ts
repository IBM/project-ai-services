import type { DataTableHeader } from "@carbon/react";
import type { WorkerStatus } from "@/types/api.types";
import type {
  BaseTableState,
  SharedTableAction,
} from "@/components/Table/types";
import {
  handleSharedTableAction,
  isSharedTableAction,
  setLoading,
} from "@/components/Table/utils/reducerUtils";

export interface WorkerResourceRow {
  id: string;
  name: string;
  status: WorkerStatus;
  runtime_type: string;
  services: number;
  messages: string;
  actions: string;
}

export type AppState = BaseTableState<WorkerResourceRow>;

export const ACTION_TYPES = {
  FETCH_WORKERS_SUCCESS: "FETCH_WORKERS_SUCCESS",
} as const;

export type AppAction = {
  type: typeof ACTION_TYPES.FETCH_WORKERS_SUCCESS;
  payload: {
    rows: WorkerResourceRow[];
    total: number;
  };
};

export const HEADERS: DataTableHeader[] = [
  { header: "Name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Type", key: "runtime_type" },
  { header: "Services", key: "services" },
  { header: "Message", key: "messages" },
  { header: "", key: "actions" },
];

// Status column sort order — disconnected/pending float to the top
export const STATUS_SORT_ORDER: Record<WorkerStatus, number> = {
  disconnected: 1,
  pending: 2,
  ready: 3,
};

export const DEFAULT_VISIBLE_COLUMNS: Record<string, boolean> = {
  name: true,
  status: true,
  runtime_type: true,
  services: true,
  messages: true,
};

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
  hasError: false,
  isExportDialogOpen: false,
  isExporting: false,
  csvFileName: "",
  exportErrorMessage: "",
  visibleColumns: { ...DEFAULT_VISIBLE_COLUMNS },
  exportToastOpen: false,
  exportToastMessage: "",
  exportToastKind: "success",
  isLoading: true,
  fetchError: null,
};

function ownCases(state: AppState, action: AppAction): AppState {
  switch (action.type) {
    case ACTION_TYPES.FETCH_WORKERS_SUCCESS:
      return {
        ...state,
        ...setLoading(false),
        rowsData: [...action.payload.rows].sort(
          (a, b) =>
            STATUS_SORT_ORDER[a.status] - STATUS_SORT_ORDER[b.status] ||
            a.name.localeCompare(b.name),
        ),
        totalItems: action.payload.total,
        fetchError: null,
      };
    default:
      return state;
  }
}

export const appReducer = (
  state: AppState,
  action: AppAction | SharedTableAction,
): AppState => {
  if (isSharedTableAction(action)) {
    return handleSharedTableAction(state, action) ?? state;
  }
  return ownCases(state, action);
};
