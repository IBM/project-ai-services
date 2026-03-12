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

export interface AppState {
  search: string;
  page: number;
  pageSize: number;
  isDeleteDialogOpen: boolean;
  isConfirmed: boolean;
  rowsData: ApplicationRow[];
  selectedRowId: string | null;
  toastOpen: boolean;
  errorMessage: string;
  errorRowName: string;
  isDeleting: boolean;
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
  | { type: typeof ACTION_TYPES.SET_IS_DELETING; payload: boolean };
