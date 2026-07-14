import type { DataTableHeader } from "@carbon/react";

export type RowStatus =
  | "Deploying..."
  | "Deleting..."
  | "Error"
  | "Stopped"
  | "Running";

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

  // Loading
  isLoading: boolean;

  // Fetch error
  fetchError: string | null;

  hasError: boolean;
}
