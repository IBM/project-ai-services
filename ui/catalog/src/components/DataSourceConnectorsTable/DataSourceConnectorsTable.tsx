import { useReducer, useCallback, useRef, useEffect } from "react";

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
} from "@carbon/react";
import { Add } from "@carbon/icons-react";
import type { Dispatch } from "react";
import type { AppAction, DataSourceConnectorRow } from "./types";
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
import { useAutoRefresh } from "@/components/Table/hooks/useAutoRefresh";
import { useCSVExport } from "@/components/Table/hooks/useCSVExport";
import { useExportToastAutoDismiss } from "@/components/Table/hooks/useExportToastAutoDismiss";
import {
  filterRowsBySearch,
  getVisibleHeaders,
} from "@/components/Table/utils/tableUtils";
import {
  fetchDataSourceConnectors,
  fetchAllDataSourceConnectors,
  transformConnectorToRow,
  fetchConnectorTypes,
  fetchConnectorParams,
} from "@/api/connectors.api";
import { useConnectorsStore } from "@/store/connectors.store";
import styles from "./DataSourceConnectorsTable.module.scss";

interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  cellKey: string;
  cellProps: Record<string, unknown>;
  rowData?: DataSourceConnectorRow;
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

export interface DataSourceConnectorsTableProps {
  onAdd?: () => void;
  refreshTrigger?: number;
}

const DataSourceConnectorsTable = ({
  onAdd,
  refreshTrigger,
}: DataSourceConnectorsTableProps) => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

  // Connector catalog prefetch — read store actions once, stable references
  const {
    isConnectorTypesStale,
    setConnectorTypes,
    setConnectorTypesLoading,
    setConnectorTypesError,
    isParamsStale,
    setParams,
    setParamsLoading,
    setParamsError,
  } = useConnectorsStore();

  const pageRef = useRef(INITIAL_STATE.page);
  const pageSizeRef = useRef(INITIAL_STATE.pageSize);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;

  const loadConnectors = useCallback(
    async (page = pageRef.current, pageSize = pageSizeRef.current) => {
      dispatch({ type: "SHARED_SET_LOADING", payload: true });
      dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: null });

      try {
        const response = await fetchDataSourceConnectors(page, pageSize);
        const rows = response.data.map(transformConnectorToRow);

        // Guard: if the deleted item was the last one on a non-first page,
        // the API returns an empty page. Correct back to the last valid page
        // and re-fetch — same pattern used in DigitalAssistants.
        const totalPages = Math.max(1, Math.ceil(response.total / pageSize));
        if (page > totalPages) {
          pageRef.current = totalPages;
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          void loadConnectors(totalPages, pageSize);
          return;
        }

        dispatch({
          type: ACTION_TYPES.FETCH_CONNECTORS_SUCCESS,
          payload: { rows, total: response.total },
        });
      } catch (error) {
        const errorMessage =
          error instanceof Error
            ? error.message
            : "Failed to load data source connectors";
        dispatch({ type: "SHARED_SET_LOADING", payload: false });
        dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: errorMessage });
      }
    },
    [],
  );

  // Background prefetch — fires once after the connector list loads successfully.
  useEffect(() => {
    // Wait until the table has data (list loaded)
    if (state.rowsData.length === 0) return;
    // Skip if cache is still fresh
    if (!isConnectorTypesStale()) return;

    setConnectorTypesLoading(true);

    fetchConnectorTypes()
      .then((types) => {
        setConnectorTypes(types);

        // Prefetch params for each provider in parallel — skip any still fresh
        types.forEach((type) => {
          if (!isParamsStale(type.id)) return;
          setParamsLoading(type.id, true);
          fetchConnectorParams(type.id)
            .then((schema) => setParams(type.id, schema))
            .catch(() => setParamsError(type.id, "Failed to load params"));
        });
      })
      .catch(() => setConnectorTypesError("Failed to load connector types"));
    // Only re-run when rowsData goes from empty → populated (first successful load)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.rowsData.length]);

  // Mount fetch + optional 2-minute auto-refresh (paused during delete flow)
  useAutoRefresh({
    fetchFn: loadConnectors,
    hasData: state.rowsData.length > 0,
    isPaused: state.isDeleteDialogOpen || state.isDeleting,
    refreshTrigger,
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
    searchFields: ["name", "status", "type", "services", "messages"],
    visibleColumns: state.visibleColumns,
    headers: HEADERS,
    fetchAllRows: async () => {
      const allData = await fetchAllDataSourceConnectors();
      return allData.map(transformConnectorToRow) as unknown as Record<
        string,
        unknown
      >[];
    },
    dispatch,
  });

  // Client-side search filter (API endpoint will handle this server-side later)
  const filteredRows = filterRowsBySearch<Record<string, unknown>>(
    state.rowsData as unknown as Record<string, unknown>[],
    state.search,
    ["name", "status", "type", "services", "messages"],
  ) as unknown as DataSourceConnectorRow[];

  const noData =
    state.rowsData.length === 0 && !state.isLoading && !state.fetchError;
  const noSearchResults =
    state.rowsData.length > 0 && filteredRows.length === 0 && !state.fetchError;

  const visibleHeaders = getVisibleHeaders(HEADERS, state.visibleColumns);

  return (
    <>
      {/* Toasts — rendered outside the grid to stay fixed-position */}
      <TableToasts
        toastOpen={state.toastOpen}
        deleteErrorRowName={state.deleteErrorRowName}
        deleteErrorMessage={state.deleteErrorMessage}
        entityLabel="data source connector"
        onDeleteErrorClose={() => {}}
        onDeleteErrorRetry={async () => {}}
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
            {state.isLoading ? (
              <DataTableSkeleton
                headers={HEADERS}
                rowCount={state.pageSize}
                columnCount={HEADERS.length}
              />
            ) : (
              <DataTable rows={filteredRows} headers={visibleHeaders} size="lg">
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
                        onRefresh={() => loadConnectors()}
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
                          onClick={onAdd}
                        >
                          Add
                        </Button>
                      </TableToolbarActions>

                      <Table {...getTableProps()}>
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
                              const originalRow = filteredRows.find(
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
                        entityName="data source connector"
                        className={styles.noDataContent}
                      />
                    </TableContainer>

                    {/* Show pagination only when there is more than one page */}
                    {!state.isLoading &&
                      state.totalItems > state.pageSize &&
                      filteredRows.length > 0 && (
                        <Pagination
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
                            void loadConnectors(page, pageSize);
                          }}
                        />
                      )}
                  </>
                )}
              </DataTable>
            )}

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

export default DataSourceConnectorsTable;
