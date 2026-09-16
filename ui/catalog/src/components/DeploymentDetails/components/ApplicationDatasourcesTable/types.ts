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

export type { DatasourceSyncStatus as ApplicationDatasourceStatus } from "@/types/api.types";

export interface ApplicationDatasourceRow {
  id: string;
  name: string;
  source_type: string;
  status: string;
  files: string;
  last_sync: string;
  messages: string;
  /** Required by Carbon DataTable — kept as empty string for the actions column */
  actions: string;
}

export interface AppState extends BaseTableState<ApplicationDatasourceRow> {
  /** Text typed into the Remove confirmation input */
  confirmTextValue: string;
  /** Error message shown inside the Remove modal */
  modalDeleteError: string;
}

export const ACTION_TYPES = {
  FETCH_DATASOURCES_SUCCESS: "FETCH_DATASOURCES_SUCCESS",
  SET_CONFIRM_TEXT: "SET_CONFIRM_TEXT",
  SET_MODAL_DELETE_ERROR: "SET_MODAL_DELETE_ERROR",
} as const;

export type AppAction =
  | {
      type: typeof ACTION_TYPES.FETCH_DATASOURCES_SUCCESS;
      payload: {
        rows: ApplicationDatasourceRow[];
        total: number;
      };
    }
  | { type: typeof ACTION_TYPES.SET_CONFIRM_TEXT; payload: string }
  | { type: typeof ACTION_TYPES.SET_MODAL_DELETE_ERROR; payload: string };

export const HEADERS: DataTableHeader[] = [
  { header: "Name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Source type", key: "source_type" },
  { header: "Files", key: "files" },
  { header: "Last sync", key: "last_sync" },
  { header: "Messages", key: "messages" },
  { header: "", key: "actions" },
];

export const DEFAULT_VISIBLE_COLUMNS: Record<string, boolean> = {
  name: true,
  status: true,
  source_type: true,
  files: true,
  last_sync: true,
  messages: true,
};

export const INITIAL_STATE: AppState = {
  confirmTextValue: "",
  modalDeleteError: "",
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
    case ACTION_TYPES.FETCH_DATASOURCES_SUCCESS:
      return {
        ...state,
        ...setLoading(false),
        rowsData: [...action.payload.rows].sort((a, b) =>
          a.name.localeCompare(b.name),
        ),
        totalItems: action.payload.total,
        fetchError: null,
      };
    case ACTION_TYPES.SET_CONFIRM_TEXT:
      return { ...state, confirmTextValue: action.payload };
    case ACTION_TYPES.SET_MODAL_DELETE_ERROR:
      return { ...state, modalDeleteError: action.payload };
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
