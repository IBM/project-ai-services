import type { DataTableHeader } from "@carbon/react";
import type { ConnectorStatus } from "@/types/api.types";
import type {
  BaseTableState,
  SharedTableAction,
} from "@/components/Table/types";
import {
  handleSharedTableAction,
  isSharedTableAction,
  setLoading,
} from "@/components/Table/utils/reducerUtils";

export interface DataSourceConnectorRow {
  id: string;
  name: string;
  status: ConnectorStatus;
  type: string;
  services: number | null;
  messages: string;
  /** Required by BaseTableRow — kept as empty string for actions column */
  actions: string;
}

export interface AppState extends BaseTableState<DataSourceConnectorRow> {
  // Selected connector for the details side panel
  selectedConnectorId: string | null;
  isDetailsPanelOpen: boolean;
}

export const ACTION_TYPES = {
  FETCH_CONNECTORS_SUCCESS: "FETCH_CONNECTORS_SUCCESS",
  OPEN_DETAILS_PANEL: "OPEN_DETAILS_PANEL",
  CLOSE_DETAILS_PANEL: "CLOSE_DETAILS_PANEL",
} as const;

export type AppAction = {
  type: typeof ACTION_TYPES.FETCH_CONNECTORS_SUCCESS;
  payload: { rows: DataSourceConnectorRow[]; total: number };
};

export const HEADERS: DataTableHeader[] = [
  { header: "Name", key: "name" },
  { header: "Status", key: "status" },
  { header: "Type", key: "type" },
  { header: "Services", key: "services" },
  { header: "Messages", key: "messages" },
  { header: "", key: "actions" },
];

// Status column sort order — offline floats to the top
export const STATUS_SORT_ORDER: Record<ConnectorStatus, number> = {
  Offline: 1,
  Connected: 2,
};

export const DEFAULT_VISIBLE_COLUMNS: Record<string, boolean> = {
  name: true,
  status: true,
  type: true,
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
  selectedConnectorId: null,
  isDetailsPanelOpen: false,
};

function ownCases(state: AppState, action: AppAction): AppState {
  switch (action.type) {
    case ACTION_TYPES.FETCH_CONNECTORS_SUCCESS:
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
