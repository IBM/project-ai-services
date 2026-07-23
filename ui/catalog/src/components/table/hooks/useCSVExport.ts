import { useCallback } from "react";
import { downloadCSVWithChildren } from "@/utils/csv";
import { getToggleableHeaders } from "@/components/table/utils/tableUtils";
import type { TableHeaders } from "@/components/table/types";
export interface CSVExportActionTypes {
  SET_EXPORT_ERROR: string;
  SET_EXPORTING: string;
  CLOSE_EXPORT_DIALOG: string;
  SHOW_EXPORT_TOAST: string;
}

// Bivariant method signature — accepts any useReducer dispatcher without
// requiring callers to cast, while still avoiding `any`.
interface DispatchLike {
  dispatch(action: { type: string; payload?: unknown }): void;
}

interface UseCSVExportOptions<TRow extends Record<string, unknown>> {
  csvFileName: string;
  totalItems: number;
  search: string;
  visibleColumns: Record<string, boolean>;
  headers: TableHeaders;

  fetchAllRows: () => Promise<TRow[]>;
  dispatch: DispatchLike["dispatch"];
  actionTypes: CSVExportActionTypes;
}

export function useCSVExport<TRow extends Record<string, unknown>>({
  csvFileName,
  totalItems,
  search,
  visibleColumns,
  headers,
  fetchAllRows,
  dispatch,
  actionTypes,
}: UseCSVExportOptions<TRow>) {
  const downloadCSV = useCallback(async () => {
    const name = csvFileName.trim();

    if (!name) {
      dispatch({
        type: actionTypes.SET_EXPORT_ERROR,
        payload: "Provide a valid file name",
      });
      return;
    }

    if (totalItems === 0) {
      dispatch({
        type: actionTypes.SET_EXPORT_ERROR,
        payload: "No data available to export",
      });
      return;
    }

    dispatch({ type: actionTypes.SET_EXPORTING, payload: true });

    try {
      const allRows = await fetchAllRows();

      // Apply search filter to exported data (matches the in-table filter)
      const filteredRows = search
        ? allRows.filter((row) =>
            Object.values(row)
              .join(" ")
              .toLowerCase()
              .includes(search.toLowerCase()),
          )
        : allRows;

      // Only export visible, non-actions columns
      const visibleHeaders = getToggleableHeaders(headers).filter(
        (h) => visibleColumns[h.key] === true,
      );

      const result = downloadCSVWithChildren(
        filteredRows as unknown as (TRow & { children?: TRow[] })[],
        visibleHeaders,
        name,
      );

      dispatch({ type: actionTypes.CLOSE_EXPORT_DIALOG });
      dispatch({
        type: actionTypes.SHOW_EXPORT_TOAST,
        payload: {
          message: result.message,
          kind: result.success ? "success" : "error",
        },
      });
    } catch {
      dispatch({
        type: actionTypes.SHOW_EXPORT_TOAST,
        payload: {
          message: "Failed to fetch data for export",
          kind: "error",
        },
      });
    } finally {
      dispatch({ type: actionTypes.SET_EXPORTING, payload: false });
    }
  }, [
    csvFileName,
    totalItems,
    search,
    visibleColumns,
    headers,
    fetchAllRows,
    dispatch,
    actionTypes,
  ]);

  return { downloadCSV };
}
