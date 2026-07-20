import type { DataTableHeader } from "@carbon/react";

export type RowStatus =
  | "Initializing"
  | "Downloading"
  | "Deploying"
  | "Running"
  | "Deleting"
  | "Stopped"
  | "Error";

export type TableHeaders = DataTableHeader[];

export interface BaseTableState {
  // Search
  search: string;

  // Pagination
  page: number;
  pageSize: number;
  totalItems: number;

  // Delete dialog
  isDeleteDialogOpen: boolean;
  isConfirmed: boolean;
  selectedRowId: string | null;

  // Delete error toast
  toastOpen: boolean;
  deleteErrorMessage: string;
  deleteErrorRowName: string;
  isDeleting: boolean;
  hasError: boolean;

  // Export dialog
  isExportDialogOpen: boolean;
  isExporting: boolean;
  csvFileName: string;
  exportErrorMessage: string;

  // Column visibility
  visibleColumns: Record<string, boolean>;

  // Export result toast
  exportToastOpen: boolean;
  exportToastMessage: string;
  exportToastKind: "success" | "error";

  isLoading: boolean;
  fetchError: string | null;
}

// ─── Shared action union ──────────────────────────────────────────────────────

export type SharedTableAction =
  | { type: "SHARED_SET_SEARCH"; payload: string }
  | { type: "SHARED_SET_PAGE"; payload: number }
  | { type: "SHARED_SET_PAGE_SIZE"; payload: number }
  | { type: "SHARED_OPEN_DELETE_DIALOG"; payload: string }
  | {
      type: "SHARED_CLOSE_DELETE_DIALOG";
      hasError: boolean;
      selectedRowId: string | null;
    }
  | { type: "SHARED_SET_CONFIRMED"; payload: boolean }
  | { type: "SHARED_SET_SELECTED_ROW_ID"; payload: string | null }
  | { type: "SHARED_SET_LOADING"; payload: boolean }
  | {
      type: "SHARED_SHOW_ERROR";
      payload: { message: string; rowName?: string };
    }
  | { type: "SHARED_HIDE_ERROR" }
  | { type: "SHARED_OPEN_EXPORT_DIALOG" }
  | { type: "SHARED_CLOSE_EXPORT_DIALOG" }
  | { type: "SHARED_SET_CSV_FILENAME"; payload: string }
  | { type: "SHARED_SET_EXPORT_ERROR"; payload: string }
  | { type: "SHARED_CLEAR_EXPORT_ERROR" }
  | { type: "SHARED_SET_EXPORTING"; payload: boolean }
  | {
      type: "SHARED_SHOW_EXPORT_TOAST";
      payload: { message: string; kind: "success" | "error" };
    }
  | { type: "SHARED_HIDE_EXPORT_TOAST" }
  | { type: "SHARED_TOGGLE_COLUMN_VISIBILITY"; payload: string }
  | {
      type: "SHARED_RESET_COLUMN_VISIBILITY";
      /** Pass the table's DEFAULT_VISIBLE_COLUMNS constant. */
      defaultColumns: Record<string, boolean>;
    }
  | { type: "SHARED_SET_FETCH_ERROR"; payload: string | null };
