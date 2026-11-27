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

export interface AppState extends BaseTableState<ModelDeploymentRow> {
  // Model-specific: selected filter values (provider ids / model identifiers)
  selectedProviders: string[];
  selectedModels: string[];
}

export const ACTION_TYPES = {
  FETCH_MODELS_SUCCESS: "FETCH_MODELS_SUCCESS",
  SET_FILTERS: "SET_FILTERS",
} as const;

export type AppAction =
  | {
      type: typeof ACTION_TYPES.FETCH_MODELS_SUCCESS;
      payload: { rows: ModelDeploymentRow[]; total: number };
    }
  | {
      type: typeof ACTION_TYPES.SET_FILTERS;
      payload: { providers: string[]; models: string[] };
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
  selectedProviders: [],
  selectedModels: [],
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
    case ACTION_TYPES.SET_FILTERS:
      return {
        ...state,
        selectedProviders: action.payload.providers,
        selectedModels: action.payload.models,
        page: 1,
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
