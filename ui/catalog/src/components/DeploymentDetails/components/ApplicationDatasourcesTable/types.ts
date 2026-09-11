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

export type AppState = BaseTableState<ApplicationDatasourceRow>;

export const ACTION_TYPES = {
  FETCH_DATASOURCES_SUCCESS: "FETCH_DATASOURCES_SUCCESS",
} as const;

export type AppAction = {
  type: typeof ACTION_TYPES.FETCH_DATASOURCES_SUCCESS;
  payload: {
    rows: ApplicationDatasourceRow[];
    total: number;
  };
};

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
