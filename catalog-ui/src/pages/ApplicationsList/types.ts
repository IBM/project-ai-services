export interface ApplicationRow {
  id: string;
  name: string;
  template: string;
  processors: number;
  memory: string;
  cards: number;
  storage: string;
  actions: string;
}

export type ExportStatus = "idle" | "exporting" | "success" | "error";

export interface AppState {
  search: string;
  page: number;
  pageSize: number;
  isDeleteDialogOpen: boolean;
  isConfirmed: boolean;
  rowsData: ApplicationRow[];
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
  | { type: typeof ACTION_TYPES.SET_IS_DELETING; payload: boolean };
