import { useReducer, useCallback, useRef, useEffect } from "react";
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
  Button,
  Grid,
  Column,
  DataTableSkeleton,
  ActionableNotification,
} from "@carbon/react";
import { ApiKey } from "@carbon/icons-react";
import type { Dispatch } from "react";
import type { AppAction, WorkerResourceRow } from "./types";
import sharedStyles from "@/components/Table/table.shared.module.scss";
import {
  ACTION_TYPES,
  DEFAULT_VISIBLE_COLUMNS,
  HEADERS,
  INITIAL_STATE,
  appReducer,
} from "./types";
import { CELL_RENDERERS } from "./CellRenderers";
import type { SharedTableAction } from "@/components/Table/types";
import TableToolbarActions from "@/components/Table/components/TableToolbarActions";
import ExportModal from "@/components/Table/components/ExportModal";
import TableToasts from "@/components/Table/components/TableToasts";
import TableEmptyStates from "@/components/Table/components/TableEmptyStates";
import DeregisterWorkerModal from "./DeregisterWorkerModal";
import { useAutoRefresh } from "@/components/Table/hooks/useAutoRefresh";
import { useCSVExport } from "@/components/Table/hooks/useCSVExport";
import { useExportToastAutoDismiss } from "@/components/Table/hooks/useExportToastAutoDismiss";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { getVisibleHeaders } from "@/components/Table/utils/tableUtils";
import {
  fetchWorkerResources,
  fetchAllWorkerResources,
  transformWorkerToRow,
  deregisterWorker,
} from "@/api/workerResources.api";
import styles from "./WorkerResourcesTable.module.scss";

interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  cellKey: string;
  cellProps: Record<string, unknown>;
  rowData?: WorkerResourceRow;
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

export interface WorkerResourcesTableProps {
  onRegister?: () => void;
  registerError?: { message: string; onRetry: () => void } | null;
  onRegisterErrorDismiss?: () => void;
  refreshTrigger?: number;
}

const WorkerResourcesTable = ({
  onRegister,
  registerError,
  onRegisterErrorDismiss,
  refreshTrigger,
}: WorkerResourcesTableProps) => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

  const pageRef = useRef(INITIAL_STATE.page);
  const pageSizeRef = useRef(INITIAL_STATE.pageSize);
  const registerRetryRef = useRef<(() => void) | null>(null);
  const isRegisterError = useRef(false);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;

  const debouncedSearch = useDebouncedValue(state.search.trim(), 400);
  const debouncedSearchRef = useRef(debouncedSearch);
  debouncedSearchRef.current = debouncedSearch;

  const abortControllerRef = useRef<AbortController | null>(null);
  // Flips to true after the very first successful fetch. Used by showSkeleton
  // so that all subsequent loads use the dimming overlay instead of the full skeleton.
  const hasEverLoadedRef = useRef(false);

  const loadWorkers = useCallback(
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
        const response = await fetchWorkerResources(page, pageSize, search, {
          signal: controller.signal,
        });
        const rows = response.data.map(transformWorkerToRow);
        const totalItems = response.pagination.total_items;

        const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
        if (page > totalPages) {
          pageRef.current = totalPages;
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          void loadWorkers(totalPages, pageSize, search);
          return;
        }

        hasEverLoadedRef.current = true;
        dispatch({
          type: ACTION_TYPES.FETCH_WORKERS_SUCCESS,
          payload: { rows, total: totalItems },
        });
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
            : "Failed to load worker resources";
        dispatch({ type: "SHARED_SET_LOADING", payload: false });
        dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: errorMessage });
      }
    },
    [],
  );

  const isFirstMount = useRef(true);

  // Trigger search on debounced term changes (resetting page to 1)
  useEffect(() => {
    if (isFirstMount.current) {
      isFirstMount.current = false;
      return;
    }

    pageRef.current = 1;
    dispatch({ type: "SHARED_SET_PAGE", payload: 1 });
    void loadWorkers(1, pageSizeRef.current, debouncedSearch);
  }, [debouncedSearch, loadWorkers]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, []);

  const selectedRowSnapshot = useRef<WorkerResourceRow | undefined>(undefined);
  const selectedRow = state.rowsData.find((r) => r.id === state.selectedRowId);
  if (selectedRow) selectedRowSnapshot.current = selectedRow;
  const modalRow = state.isDeleteDialogOpen
    ? (selectedRow ?? selectedRowSnapshot.current)
    : selectedRowSnapshot.current;

  const handleDeregister = useCallback(async () => {
    const id = state.selectedRowId;
    if (!id) return;

    const rowName = state.rowsData.find((r) => r.id === id)?.name ?? "";
    dispatch({ type: "SHARED_SET_DELETING", payload: true });

    try {
      await deregisterWorker(id);
      dispatch({ type: "SHARED_HIDE_ERROR" });
      dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" });
      void loadWorkers();
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to deregister worker";
      dispatch({
        type: "SHARED_SHOW_ERROR",
        payload: { message, rowName },
      });
      dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" });
    }
  }, [state.selectedRowId, state.rowsData, loadWorkers]);

  // Mount fetch + 2-minute auto-refresh.
  useAutoRefresh({
    fetchFn: loadWorkers,
    hasData: true,
    isPaused: state.isDeleteDialogOpen || state.isDeleting,
    refreshTrigger,
  });

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
      const allData = await fetchAllWorkerResources(state.search.trim());
      return allData.map(transformWorkerToRow) as unknown as Record<
        string,
        unknown
      >[];
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

  useEffect(() => {
    if (!registerError) return;
    registerRetryRef.current = registerError.onRetry;
    isRegisterError.current = true;
    dispatch({
      type: ACTION_TYPES.SHOW_REGISTER_ERROR,
      payload: registerError.message,
    });
  }, [registerError]);

  const handleRegisterErrorClose = useCallback(() => {
    isRegisterError.current = false;
    dispatch({ type: ACTION_TYPES.HIDE_REGISTER_ERROR });
    onRegisterErrorDismiss?.();
  }, [onRegisterErrorDismiss]);

  return (
    <>
      {state.registerErrorToastOpen && (
        <ActionableNotification
          kind="error"
          title="Register worker failed"
          subtitle={state.registerErrorToastMessage}
          actionButtonLabel="Try again"
          closeOnEscape
          aria-label="close notification"
          className={sharedStyles.customToast}
          onCloseButtonClick={handleRegisterErrorClose}
          onActionButtonClick={() => {
            isRegisterError.current = false;
            dispatch({ type: ACTION_TYPES.HIDE_REGISTER_ERROR });
            registerRetryRef.current?.();
          }}
        />
      )}
      <TableToasts
        toastOpen={state.toastOpen}
        deleteErrorRowName={state.deleteErrorRowName}
        deleteErrorMessage={state.deleteErrorMessage}
        entityLabel="worker resource"
        onDeleteErrorClose={() => dispatch({ type: "SHARED_HIDE_ERROR" })}
        onDeleteErrorRetry={handleDeregister}
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
                    <TableContainer>
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
                        onRefresh={() => loadWorkers()}
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
                          renderIcon={ApiKey}
                          onClick={onRegister}
                        >
                          Register
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
                        entityName="worker resource"
                        noDataTitle="No worker resources registered yet"
                        noDataSubtitle="To register a new worker resource, click Register."
                        className={styles.noDataContent}
                      />
                    </TableContainer>

                    {state.totalItems > state.pageSize &&
                      state.rowsData.length > 0 && (
                        <Pagination
                          className={
                            state.isLoading ? styles.loadingTable : undefined
                          }
                          disabled={state.isLoading}
                          page={state.page}
                          pageSize={state.pageSize}
                          pageSizes={[10, 20, 30, 50]}
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
                            void loadWorkers(page, pageSize);
                          }}
                        />
                      )}
                  </>
                )}
              </DataTable>
            )}

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

            <DeregisterWorkerModal
              isOpen={state.isDeleteDialogOpen}
              isDeregistering={state.isDeleting}
              workerName={modalRow?.name ?? ""}
              workerStatus={modalRow?.status ?? ""}
              runtimeType={modalRow?.runtime_type ?? ""}
              onConfirm={() => void handleDeregister()}
              onClose={() => dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" })}
            />
          </Column>
        </Grid>
      </div>
    </>
  );
};

export default WorkerResourcesTable;
