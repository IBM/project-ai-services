import { useCallback } from "react";
import { downloadCSVWithChildren } from "@/utils/csv";
import {
  filterRowsBySearch,
  getToggleableHeaders,
} from "@/components/Table/utils/tableUtils";
import type { SharedTableAction, TableHeaders } from "@/components/Table/types";

interface UseCSVExportOptions<TRow extends Record<string, unknown>> {
  csvFileName: string;
  totalItems: number;
  search: string;
  searchFields: (keyof TRow)[];
  visibleColumns: Record<string, boolean>;
  headers: TableHeaders;

  fetchAllRows: () => Promise<TRow[]>;
  dispatch: (action: SharedTableAction) => void;
}

export function useCSVExport<TRow extends Record<string, unknown>>({
  csvFileName,
  totalItems,
  search,
  searchFields,
  visibleColumns,
  headers,
  fetchAllRows,
  dispatch,
}: UseCSVExportOptions<TRow>) {
  const downloadCSV = useCallback(async () => {
    const name = csvFileName.trim();

    if (!name) {
      dispatch({
        type: "SHARED_SET_EXPORT_ERROR",
        payload: "Provide a valid file name",
      });
      return;
    }

    if (totalItems === 0) {
      dispatch({
        type: "SHARED_SET_EXPORT_ERROR",
        payload: "No data available to export",
      });
      return;
    }

    dispatch({ type: "SHARED_SET_EXPORTING", payload: true });

    try {
      const allRows = await fetchAllRows();

      // Apply search filter to exported data (matches the in-table filter)
      const filteredRows = filterRowsBySearch(allRows, search, searchFields);
      // Only export visible, non-actions columns
      const visibleHeaders = getToggleableHeaders(headers).filter(
        (h) => visibleColumns[h.key] === true,
      );

      const result = downloadCSVWithChildren(
        filteredRows as unknown as (TRow & { children?: TRow[] })[],
        visibleHeaders,
        name,
      );

      dispatch({ type: "SHARED_CLOSE_EXPORT_DIALOG" });
      dispatch({
        type: "SHARED_SHOW_EXPORT_TOAST",
        payload: {
          message: result.message,
          kind: result.success ? "success" : "error",
        },
      });
    } catch {
      dispatch({
        type: "SHARED_SHOW_EXPORT_TOAST",
        payload: {
          message: "Failed to fetch data for export",
          kind: "error",
        },
      });
    } finally {
      dispatch({ type: "SHARED_SET_EXPORTING", payload: false });
    }
  }, [
    csvFileName,
    totalItems,
    search,
    searchFields,
    visibleColumns,
    headers,
    fetchAllRows,
    dispatch,
  ]);

  return { downloadCSV };
}
