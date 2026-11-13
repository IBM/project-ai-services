import type { DataTableHeader } from "@carbon/react";
import type {
  BaseTableState,
  SharedTableAction,
} from "@/components/Table/types";
import {
  handleSharedTableAction,
  isSharedTableAction,
  setLoading,
} from "@/components/Table/utils/reducerUtils";

export interface ModelDeploymentRow {
  id: string;
  name: string;
  status: string;
  createdAt: string;
  model: string;
  componentType: string;
  backend: string;
  workerResource: string;
  servicesCount: number;
  messages: string;
  actions: string;
}

// Switch to `interface ... extends BaseTableState<...>` once table-specific
// state (e.g. a details panel) is added, as in the other tables.
export type AppState = BaseTableState<ModelDeploymentRow>;

export const ACTION_TYPES = {
  FETCH_MODELS_SUCCESS: "FETCH_MODELS_SUCCESS",
} as const;

export type AppAction = {
  type: typeof ACTION_TYPES.FETCH_MODELS_SUCCESS;
  payload: { rows: ModelDeploymentRow[]; total: number };
};

// Table headers — 9 visible columns + actions overflow
export const HEADERS: DataTableHeader[] = [
  { header: "Instance name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Created", key: "createdAt" },
  { header: "Model", key: "model" },
  { header: "Type", key: "componentType" },
  { header: "Backend", key: "backend" },
  { header: "Worker resource", key: "workerResource" },
  { header: "Services", key: "servicesCount" },
  { header: "Messages", key: "messages" },
  { header: "", key: "actions" },
];

export const DEFAULT_VISIBLE_COLUMNS: Record<string, boolean> = {
  name: true,
  status: true,
  createdAt: true,
  model: true,
  componentType: true,
  backend: true,
  workerResource: true,
  servicesCount: true,
  messages: true,
};

export const INITIAL_STATE: AppState = {
  search: "",
  page: 1,
  pageSize: 20,
  totalItems: 0,
  isDeleteDialogOpen: false,
  confirmValue: "",
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
  isLoading: true,
  fetchError: null,
};

function ownCases(state: AppState, action: AppAction): AppState {
  switch (action.type) {
    case ACTION_TYPES.FETCH_MODELS_SUCCESS:
      return {
        ...state,
        ...setLoading(false),
        rowsData: action.payload.rows,
        totalItems: action.payload.total,
        fetchError: null,
      };
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
