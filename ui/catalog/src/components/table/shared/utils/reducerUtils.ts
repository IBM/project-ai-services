import type {
  BaseTableState,
  SharedTableAction,
} from "@/components/table/shared/types";

export function handleSharedTableAction<S extends BaseTableState>(
  state: S,
  action: SharedTableAction,
): S | undefined {
  switch (action.type) {
    case "SHARED_SET_SEARCH":
      return { ...state, ...setSearch(action.payload) };
    case "SHARED_SET_PAGE":
      return { ...state, ...setPage(action.payload) };
    case "SHARED_SET_PAGE_SIZE":
      return { ...state, ...setPageSize(action.payload) };
    case "SHARED_OPEN_DELETE_DIALOG":
      return { ...state, ...openDeleteDialog(action.payload) };
    case "SHARED_CLOSE_DELETE_DIALOG":
      return {
        ...state,
        ...closeDeleteDialog(action.hasError, action.selectedRowId),
      };
    case "SHARED_SET_CONFIRMED":
      return { ...state, ...setConfirmed(action.payload) };
    case "SHARED_SET_SELECTED_ROW_ID":
      return { ...state, ...setSelectedRowId(action.payload) };
    case "SHARED_SET_LOADING":
      return { ...state, ...setLoading(action.payload) };
    case "SHARED_SHOW_ERROR":
      return { ...state, ...showError(action.payload) };
    case "SHARED_HIDE_ERROR":
      return { ...state, ...hideError() };
    case "SHARED_OPEN_EXPORT_DIALOG":
      return { ...state, ...openExportDialog() };
    case "SHARED_CLOSE_EXPORT_DIALOG":
      return { ...state, ...closeExportDialog() };
    case "SHARED_SET_CSV_FILENAME":
      return { ...state, ...setCsvFilename(action.payload) };
    case "SHARED_SET_EXPORT_ERROR":
      return { ...state, ...setExportError(action.payload) };
    case "SHARED_CLEAR_EXPORT_ERROR":
      return { ...state, ...clearExportError() };
    case "SHARED_SET_EXPORTING":
      return { ...state, ...setExporting(action.payload) };
    case "SHARED_SHOW_EXPORT_TOAST":
      return { ...state, ...showExportToast(action.payload) };
    case "SHARED_HIDE_EXPORT_TOAST":
      return { ...state, ...hideExportToast() };
    case "SHARED_TOGGLE_COLUMN_VISIBILITY":
      return {
        ...state,
        ...toggleColumnVisibility(state.visibleColumns, action.payload),
      };
    case "SHARED_RESET_COLUMN_VISIBILITY":
      return { ...state, ...resetColumnVisibility(action.defaultColumns) };
    case "SHARED_SET_FETCH_ERROR":
      return { ...state, fetchError: action.payload, isLoading: false };
    default:
      return undefined;
  }
}

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
