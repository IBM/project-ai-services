import type { BaseTableState } from "@/components/table/shared/types";

// Search

export function setSearch(value: string): Pick<BaseTableState, "search"> {
  return { search: value };
}

// Pagination

export function setPage(page: number): Pick<BaseTableState, "page"> {
  return { page };
}

export function setPageSize(
  pageSize: number,
): Pick<BaseTableState, "pageSize"> {
  return { pageSize };
}

// Delete dialog

export function openDeleteDialog(
  rowId: string,
): Pick<BaseTableState, "selectedRowId" | "isDeleteDialogOpen" | "toastOpen"> {
  return {
    selectedRowId: rowId,
    isDeleteDialogOpen: true,
    toastOpen: false,
  };
}

export function closeDeleteDialog(
  hasError: boolean,
  selectedRowId: string | null,
): Pick<
  BaseTableState,
  "isDeleteDialogOpen" | "isConfirmed" | "selectedRowId"
> {
  return {
    isDeleteDialogOpen: false,
    isConfirmed: false,
    selectedRowId: hasError ? selectedRowId : null,
  };
}

export function setConfirmed(
  checked: boolean,
): Pick<BaseTableState, "isConfirmed"> {
  return { isConfirmed: checked };
}

export function setSelectedRowId(
  id: string | null,
): Pick<BaseTableState, "selectedRowId"> {
  return { selectedRowId: id };
}

// Loading

export function setLoading(value: boolean): Pick<BaseTableState, "isLoading"> {
  return { isLoading: value };
}

// Delete error

export function showError(payload: {
  message: string;
  rowName?: string;
}): Pick<
  BaseTableState,
  | "deleteErrorMessage"
  | "deleteErrorRowName"
  | "toastOpen"
  | "isDeleting"
  | "hasError"
> {
  return {
    deleteErrorMessage: payload.message,
    deleteErrorRowName: payload.rowName ?? "",
    toastOpen: true,
    isDeleting: false,
    hasError: true,
  };
}

export function hideError(): Pick<
  BaseTableState,
  "toastOpen" | "selectedRowId" | "hasError" | "deleteErrorRowName"
> {
  return {
    toastOpen: false,
    selectedRowId: null,
    hasError: false,
    deleteErrorRowName: "",
  };
}

// Export dialog

export function openExportDialog(): Pick<
  BaseTableState,
  "isExportDialogOpen" | "csvFileName" | "exportErrorMessage"
> {
  return {
    isExportDialogOpen: true,
    csvFileName: "",
    exportErrorMessage: "",
  };
}

export function closeExportDialog(): Pick<
  BaseTableState,
  "isExportDialogOpen"
> {
  return { isExportDialogOpen: false };
}

export function setCsvFilename(
  name: string,
): Pick<BaseTableState, "csvFileName"> {
  return { csvFileName: name };
}

export function setExportError(
  message: string,
): Pick<BaseTableState, "exportErrorMessage"> {
  return { exportErrorMessage: message };
}

export function clearExportError(): Pick<BaseTableState, "exportErrorMessage"> {
  return { exportErrorMessage: "" };
}

export function setExporting(
  value: boolean,
): Pick<BaseTableState, "isExporting"> {
  return { isExporting: value };
}

// Export result toast

export function showExportToast(payload: {
  message: string;
  kind: "success" | "error";
}): Pick<
  BaseTableState,
  "exportToastOpen" | "exportToastMessage" | "exportToastKind"
> {
  return {
    exportToastOpen: true,
    exportToastMessage: payload.message,
    exportToastKind: payload.kind,
  };
}

export function hideExportToast(): Pick<BaseTableState, "exportToastOpen"> {
  return { exportToastOpen: false };
}

// Column visibility

export function toggleColumnVisibility(
  visibleColumns: Record<string, boolean>,
  columnKey: string,
): Pick<BaseTableState, "visibleColumns"> {
  return {
    visibleColumns: {
      ...visibleColumns,
      [columnKey]: !visibleColumns[columnKey],
    },
  };
}

export function resetColumnVisibility(
  defaultColumns: Record<string, boolean>,
): Pick<BaseTableState, "visibleColumns"> {
  return { visibleColumns: { ...defaultColumns } };
}
