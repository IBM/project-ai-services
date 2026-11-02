import { useReducer, useCallback, useRef, useState, useEffect } from "react";
import axios from "axios";
import {
  DataTable,
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableCell,
  TableContainer,
  Pagination,
  Grid,
  Column,
  DataTableSkeleton,
  Button,
} from "@carbon/react";
import { Add } from "@carbon/icons-react";
import type { Dispatch } from "react";
import type { AppAction, ApplicationDatasourceRow } from "./types";
import {
  ACTION_TYPES,
  DEFAULT_VISIBLE_COLUMNS,
  HEADERS,
  INITIAL_STATE,
  appReducer,
} from "./types";
import { CELL_RENDERERS } from "./CellRenderers";
import ConnectDatasourceModal from "./ConnectDatasourceModal";
import type { SharedTableAction } from "@/components/Table/types";
import TableToolbarActions from "@/components/Table/components/TableToolbarActions";
import ExportModal from "@/components/Table/components/ExportModal";
import DeleteConfirmNameModal from "@/components/DeleteConfirmNameModal";
import TableToasts from "@/components/Table/components/TableToasts";
import TableEmptyStates from "@/components/Table/components/TableEmptyStates";
import { useAutoRefresh } from "@/components/Table/hooks/useAutoRefresh";
import { useCSVExport } from "@/components/Table/hooks/useCSVExport";
import { useExportToastAutoDismiss } from "@/components/Table/hooks/useExportToastAutoDismiss";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { getVisibleHeaders } from "@/components/Table/utils/tableUtils";
import {
  fetchApplicationDatasources,
  fetchAllApplicationDatasources,
  removeApplicationDatasource,
} from "@/api/applications.api";
import styles from "./ApplicationDatasourcesTable.module.scss";

interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  cellKey: string;
  cellProps: Record<string, unknown>;
  rowData?: ApplicationDatasourceRow;
}

const renderCell = ({
  header,
  value,
  rowId,
  dispatch,
  cellKey,
  cellProps,
  rowData,
}: RenderCellProps) => {
  const CellRenderer = CELL_RENDERERS[header];

  return (
    <TableCell key={cellKey} {...cellProps}>
      {CellRenderer ? (
        <CellRenderer
          value={value}
          rowId={rowId}
          dispatch={dispatch}
          rowData={rowData}
        />
      ) : (
        String(value ?? "")
      )}
    </TableCell>
  );
};

export interface ApplicationDatasourcesTableProps {
  applicationId: string;
  /** Called after each successful load with the total number of connected datasources */
  onDatasourceCountChange?: (count: number) => void;
}

const ApplicationDatasourcesTable = ({
  applicationId,
  onDatasourceCountChange,
}: ApplicationDatasourcesTableProps) => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);
  const [isConnectModalOpen, setIsConnectModalOpen] = useState(false);

  const pageRef = useRef(INITIAL_STATE.page);
  const pageSizeRef = useRef(INITIAL_STATE.pageSize);
  const onDatasourceCountChangeRef = useRef(onDatasourceCountChange);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;
  onDatasourceCountChangeRef.current = onDatasourceCountChange;

  const debouncedSearch = useDebouncedValue(state.search.trim(), 400);
  const debouncedSearchRef = useRef(debouncedSearch);
  debouncedSearchRef.current = debouncedSearch;

  const abortControllerRef = useRef<AbortController | null>(null);
  // Flips to true after the very first successful fetch. Used by showSkeleton
  // so that all subsequent loads use the dimming overlay instead of the full skeleton.
  const hasEverLoadedRef = useRef(false);

  const loadDatasources = useCallback(
    async (
      page = pageRef.current,
      pageSize = pageSizeRef.current,
      search = debouncedSearchRef.current,
    ) => {
      // Cancel previous pending requests
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
      const controller = new AbortController();
      abortControllerRef.current = controller;

      dispatch({ type: "SHARED_SET_LOADING", payload: true });
      dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: null });

      try {
        const { rows, pagination } = await fetchApplicationDatasources(
          applicationId,
          page,
          pageSize,
          search,
          { signal: controller.signal },
        );

        const totalPages = pagination.total_pages ?? 1;
        if (page > totalPages && totalPages >= 1) {
          pageRef.current = totalPages;
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          void loadDatasources(totalPages, pageSize, search);
          return;
        }

        hasEverLoadedRef.current = true;
        dispatch({
          type: ACTION_TYPES.FETCH_DATASOURCES_SUCCESS,
          payload: { rows, total: pagination.total_items },
        });
        if (!search || search.trim() === "") {
          onDatasourceCountChangeRef.current?.(pagination.total_items);
        }
      } catch (error) {
        // The axios interceptor wraps cancellations in a generic Error, so
        // check the signal itself to ignore superseded requests.
        if (
          controller.signal.aborted ||
          (error instanceof Error && error.name === "AbortError") ||
          axios.isCancel(error)
        ) {
          return;
        }

        const errorMessage =
          error instanceof Error
            ? error.message
            : "Failed to load data sources";
        dispatch({ type: "SHARED_SET_LOADING", payload: false });
        dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: errorMessage });
      }
    },
    [applicationId],
  );

  const isFirstMount = useRef(true);

  // After a remove or connect mutation, if a search is active the normal
  // loadDatasources call returns a filtered total
  // This fires a background page=1 page_size=1 call (no search) to
  // read the unfiltered total_items and show "No data" badge
  const refreshUnfilteredCount = useCallback(async () => {
    if (!debouncedSearchRef.current) return;
    try {
      const { pagination } = await fetchApplicationDatasources(
        applicationId,
        1,
        1,
      );
      onDatasourceCountChangeRef.current?.(pagination.total_items);
    } catch {
      // Non-critical background call — ignore errors silently
    }
  }, [applicationId]);

  // Trigger search on debounced term changes (resetting page to 1)
  useEffect(() => {
    if (isFirstMount.current) {
      isFirstMount.current = false;
      return;
    }

    pageRef.current = 1;
    dispatch({ type: "SHARED_SET_PAGE", payload: 1 });
    void loadDatasources(1, pageSizeRef.current, debouncedSearch);
  }, [debouncedSearch, loadDatasources]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, []);

  const handleRemove = async () => {
    if (!state.selectedRowId) {
      dispatch({
        type: "SHARED_SHOW_ERROR",
        payload: { message: "No data source selected for removal" },
      });
      return;
    }

    dispatch({ type: "SHARED_SET_DELETING", payload: true });
    dispatch({ type: ACTION_TYPES.SET_MODAL_DELETE_ERROR, payload: "" });

    try {
      await removeApplicationDatasource(applicationId, state.selectedRowId);
      dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" });
      dispatch({ type: ACTION_TYPES.SET_CONFIRM_TEXT, payload: "" });
      await loadDatasources();
      void refreshUnfilteredCount();
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : "Failed to remove data source";
      dispatch({ type: ACTION_TYPES.SET_MODAL_DELETE_ERROR, payload: msg });
    } finally {
      dispatch({ type: "SHARED_SET_DELETING", payload: false });
    }
  };

  // Mount fetch + 2-minute auto-refresh.
  useAutoRefresh({
    fetchFn: loadDatasources,
    hasData: true,
    isPaused: state.isDeleteDialogOpen || state.isDeleting,
  });

  // Auto-dismiss success export toast after 5 seconds
  useExportToastAutoDismiss({
    exportToastOpen: state.exportToastOpen,
    exportToastKind: state.exportToastKind,
    onDismiss: () => dispatch({ type: "SHARED_HIDE_EXPORT_TOAST" }),
  });

  const { downloadCSV } = useCSVExport<Record<string, unknown>>({
    csvFileName: state.csvFileName,
    totalItems: state.totalItems,
    search: state.search,
    visibleColumns: state.visibleColumns,
    headers: HEADERS,
    serverSideSearch: true,
    fetchAllRows: async () => {
      // Use state.search.trim() (the immediate input value) so the export
      // always reflects what the user sees in the search box
      const rows = await fetchAllApplicationDatasources(
        applicationId,
        state.search.trim(),
      );
      return rows as unknown as Record<string, unknown>[];
    },
    dispatch,
  });

  const noData =
    state.rowsData.length === 0 &&
    !state.isLoading &&
    !state.fetchError &&
    !debouncedSearch;
  const noSearchResults =
    state.rowsData.length === 0 &&
    !state.isLoading &&
    !state.fetchError &&
    !!debouncedSearch;

  const visibleHeaders = getVisibleHeaders(HEADERS, state.visibleColumns);

  // Show the full skeleton only on the very first load
  const showSkeleton = state.isLoading && !hasEverLoadedRef.current;

  return (
    <>
      {/* Toasts — rendered outside the grid to stay fixed-position */}
      <TableToasts
        toastOpen={state.toastOpen}
        deleteErrorRowName={state.deleteErrorRowName}
        deleteErrorMessage={state.deleteErrorMessage}
        entityLabel="data source"
        onDeleteErrorClose={() => dispatch({ type: "SHARED_HIDE_ERROR" })}
        onDeleteErrorRetry={handleRemove}
        exportToastOpen={state.exportToastOpen}
        exportToastKind={state.exportToastKind}
        exportToastMessage={state.exportToastMessage}
        onExportToastClose={() =>
          dispatch({ type: "SHARED_HIDE_EXPORT_TOAST" })
        }
      />

      <div className={styles.tableContent}>
        <Grid fullWidth>
          <Column lg={16} md={8} sm={4} className={styles.tableColumn}>
            {showSkeleton ? (
              <DataTableSkeleton
                headers={HEADERS}
                rowCount={state.pageSize}
                columnCount={HEADERS.length}
              />
            ) : (
              <DataTable
                rows={state.rowsData}
                headers={visibleHeaders}
                size="lg"
              >
                {({
                  rows,
                  headers,
                  getHeaderProps,
                  getRowProps,
                  getCellProps,
                  getTableProps,
                }) => (
                  <>
                    <TableContainer title="Data sources">
                      <TableToolbarActions
                        search={state.search}
                        headers={HEADERS}
                        visibleColumns={state.visibleColumns}
                        onSearchChange={(value) =>
                          dispatch({
                            type: "SHARED_SET_SEARCH",
                            payload: value,
                          })
                        }
                        onRefresh={() => loadDatasources()}
                        onExport={() =>
                          dispatch({ type: "SHARED_OPEN_EXPORT_DIALOG" })
                        }
                        onToggleColumn={(key) =>
                          dispatch({
                            type: "SHARED_TOGGLE_COLUMN_VISIBILITY",
                            payload: key,
                          })
                        }
                        onResetColumns={() =>
                          dispatch({
                            type: "SHARED_RESET_COLUMN_VISIBILITY",
                            payload: DEFAULT_VISIBLE_COLUMNS,
                          })
                        }
                      >
                        <Button
                          kind="primary"
                          size="lg"
                          renderIcon={Add}
                          onClick={() => setIsConnectModalOpen(true)}
                        >
                          Connect
                        </Button>
                      </TableToolbarActions>

                      <Table
                        className={
                          state.isLoading ? styles.loadingTable : undefined
                        }
                        {...getTableProps()}
                      >
                        <TableHead>
                          <TableRow>
                            {headers.map((header) => {
                              const { key, ...rest } = getHeaderProps({
                                header,
                              });
                              return (
                                <TableHeader key={key} {...rest}>
                                  {header.header}
                                </TableHeader>
                              );
                            })}
                          </TableRow>
                        </TableHead>

                        {!state.fetchError && !noData && !noSearchResults && (
                          <TableBody>
                            {rows.map((row) => {
                              const { key: rowKey, ...rowProps } = getRowProps({
                                row,
                              });
                              const originalRow = state.rowsData.find(
                                (r) => r.id === row.id,
                              );

                              return (
                                <TableRow key={rowKey} {...rowProps}>
                                  {row.cells.map((cell) => {
                                    const { key: cellKey, ...cellProps } =
                                      getCellProps({ cell });

                                    return renderCell({
                                      header: cell.info.header,
                                      value: cell.value,
                                      rowId: row.id as string,
                                      dispatch,
                                      cellKey,
                                      cellProps,
                                      rowData: originalRow,
                                    });
                                  })}
                                </TableRow>
                              );
                            })}
                          </TableBody>
                        )}
                      </Table>

                      <TableEmptyStates
                        fetchError={state.fetchError}
                        noData={noData}
                        noSearchResults={noSearchResults}
                        entityName="data source"
                        noDataTitle="No data sources connected"
                        noDataSubtitle="Connect a data source to start ingesting files into the assistant."
                        className={styles.noDataContent}
                      />
                    </TableContainer>

                    {/* Show pagination only when there is more than one page */}
                    {!state.isLoading &&
                      state.totalItems > state.pageSize &&
                      state.rowsData.length > 0 && (
                        <Pagination
                          page={state.page}
                          pageSize={state.pageSize}
                          pageSizes={[20, 30, 50]}
                          totalItems={state.totalItems}
                          onChange={({ page, pageSize }) => {
                            pageRef.current = page;
                            pageSizeRef.current = pageSize;
                            dispatch({
                              type: "SHARED_SET_PAGE",
                              payload: page,
                            });
                            dispatch({
                              type: "SHARED_SET_PAGE_SIZE",
                              payload: pageSize,
                            });
                            void loadDatasources(page, pageSize);
                          }}
                        />
                      )}
                  </>
                )}
              </DataTable>
            )}

            {/* Connect datasource modal */}
            <ConnectDatasourceModal
              open={isConnectModalOpen}
              applicationId={applicationId}
              onClose={() => setIsConnectModalOpen(false)}
              onConnected={() => {
                setIsConnectModalOpen(false);
                void loadDatasources();
                void refreshUnfilteredCount();
              }}
              onPartialConnect={() => {
                void loadDatasources();
                void refreshUnfilteredCount();
              }}
            />

            {/* Remove data source modal */}
            <DeleteConfirmNameModal
              isOpen={state.isDeleteDialogOpen}
              isDeleting={state.isDeleting}
              itemName={
                state.rowsData.find((r) => r.id === state.selectedRowId)
                  ?.name ?? ""
              }
              warningText="Disconnecting this data source will remove it from this service and permanently delete its indexed data from this service's vector store. Syncing and ingestion will continue for any other services still connected to this data source."
              confirmValue={state.confirmTextValue}
              onConfirmValueChange={(value) =>
                dispatch({
                  type: ACTION_TYPES.SET_CONFIRM_TEXT,
                  payload: value,
                })
              }
              errorMessage={state.modalDeleteError}
              onConfirm={() => void handleRemove()}
              onClose={() => {
                dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" });
                dispatch({ type: ACTION_TYPES.SET_CONFIRM_TEXT, payload: "" });
                dispatch({
                  type: ACTION_TYPES.SET_MODAL_DELETE_ERROR,
                  payload: "",
                });
              }}
            />

            {/* Export modal */}
            <ExportModal
              isOpen={state.isExportDialogOpen}
              isExporting={state.isExporting}
              csvFileName={state.csvFileName}
              exportErrorMessage={state.exportErrorMessage}
              onConfirm={downloadCSV}
              onClose={() => dispatch({ type: "SHARED_CLOSE_EXPORT_DIALOG" })}
              onFileNameChange={(value) =>
                dispatch({
                  type: "SHARED_SET_CSV_FILENAME",
                  payload: value,
                })
              }
              onClearError={() =>
                dispatch({ type: "SHARED_CLEAR_EXPORT_ERROR" })
              }
            />
          </Column>
        </Grid>
      </div>
    </>
  );
};

export default ApplicationDatasourcesTable;
