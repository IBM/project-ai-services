import { useReducer, useCallback, useRef } from "react";
import {
  DataTable,
  DataTableSkeleton,
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
  Button,
} from "@carbon/react";
import { Deploy } from "@carbon/icons-react";
import type { Dispatch } from "react";
import type { ModelDeploymentRow } from "./types";
import {
  ACTION_TYPES,
  DEFAULT_VISIBLE_COLUMNS,
  HEADERS,
  INITIAL_STATE,
  appReducer,
} from "./types";
import type { AppAction } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import { DEPLOYMENT_STATUS } from "@/constants";
import { CELL_RENDERERS } from "./CellRenderers";
import TableToolbarActions from "@/components/Table/components/TableToolbarActions";
import ExportModal from "@/components/Table/components/ExportModal";
import TableToasts from "@/components/Table/components/TableToasts";
import TableEmptyStates from "@/components/Table/components/TableEmptyStates";
import { useAutoRefresh } from "@/components/Table/hooks/useAutoRefresh";
import { useCSVExport } from "@/components/Table/hooks/useCSVExport";
import { useExportToastAutoDismiss } from "@/components/Table/hooks/useExportToastAutoDismiss";
import { getVisibleHeaders } from "@/components/Table/utils/tableUtils";
import {
  fetchModelsPage,
  fetchAllModels,
  transformModelDeploymentToRow,
} from "@/api/models.api";
import sharedStyles from "@/components/Table/table.shared.module.scss";
import styles from "./ModelsTable.module.scss";

// ─── Generic cell renderer wrapper ───────────────────────────────────────────

interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  cellKey: string;
  cellProps: Record<string, unknown>;
  rowData: ModelDeploymentRow;
  onRowClick?: (row: ModelDeploymentRow) => void;
}

const renderCell = ({
  header,
  value,
  rowId,
  dispatch,
  cellKey,
  cellProps,
  rowData,
  onRowClick,
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
          onRowClick={header === "name" ? onRowClick : undefined}
        />
      ) : (
        String(value ?? "")
      )}
    </TableCell>
  );
};

// ─── Component props ──────────────────────────────────────────────────────────

export interface ModelsTableProps {
  onDeploy?: () => void;
  refreshTrigger?: number;
  onRowClick?: (row: ModelDeploymentRow) => void;
}

// ─── Component ────────────────────────────────────────────────────────────────

const ModelsTable = ({
  onDeploy,
  refreshTrigger,
  onRowClick,
}: ModelsTableProps) => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

  const pageRef = useRef(INITIAL_STATE.page);
  const pageSizeRef = useRef(INITIAL_STATE.pageSize);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;

  // hasTransitionalRow activates the additional 5-second poll whenever any
  // row is in a transitional state.
  const hasTransitionalRow = state.rowsData.some(
    (row) =>
      row.status === DEPLOYMENT_STATUS.DEPLOYING ||
      row.status === DEPLOYMENT_STATUS.DELETING ||
      row.status === DEPLOYMENT_STATUS.INITIALIZING ||
      row.status === DEPLOYMENT_STATUS.DOWNLOADING,
  );
  const hasTransitionalRowRef = useRef(hasTransitionalRow);
  hasTransitionalRowRef.current = hasTransitionalRow;

  // ─── Data fetching ──────────────────────────────────────────────────────────

  const fetchModels = useCallback(
    async (page = pageRef.current, pageSize = pageSizeRef.current) => {
      // Skip the skeleton while rows are transitional so polling updates statuses in place
      if (!hasTransitionalRowRef.current) {
        dispatch({ type: "SHARED_SET_LOADING", payload: true });
      }
      dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: null });

      try {
        const { data: rawData, pagination } = await fetchModelsPage();

        const totalItems = pagination?.total_items ?? rawData.length;
        const totalPages = pagination?.total_pages ?? 1;

        // If current page is beyond total_pages, jump back to last valid page
        if (page > totalPages && totalPages >= 1) {
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          fetchModels(totalPages, pageSize);
          return;
        }

        dispatch({
          type: ACTION_TYPES.FETCH_MODELS_SUCCESS,
          payload: {
            rows: rawData.map(transformModelDeploymentToRow),
            total: totalItems,
          },
        });
      } catch (error) {
        const errorMessage =
          error instanceof Error ? error.message : "Failed to fetch models";
        dispatch({ type: "SHARED_SET_LOADING", payload: false });
        dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: errorMessage });
      }
    },
    [],
  );

  // ─── Auto-refresh ───────────────────────────────────────────────────────────

  useAutoRefresh({
    fetchFn: fetchModels,
    hasData: state.rowsData.length > 0,
    isPaused: state.isDeleteDialogOpen || state.isDeleting,
    refreshTrigger,
    hasTransitionalRow,
  });

  // ─── Export toast auto-dismiss ──────────────────────────────────────────────

  useExportToastAutoDismiss({
    exportToastOpen: state.exportToastOpen,
    exportToastKind: state.exportToastKind,
    onDismiss: () => dispatch({ type: "SHARED_HIDE_EXPORT_TOAST" }),
  });

  // ─── CSV export ─────────────────────────────────────────────────────────────

  const { downloadCSV } = useCSVExport<Record<string, unknown>>({
    csvFileName: state.csvFileName,
    totalItems: state.totalItems,
    search: "",
    searchFields: [],
    visibleColumns: state.visibleColumns,
    headers: HEADERS,
    fetchAllRows: async () => {
      const allData = await fetchAllModels();
      return allData.map(transformModelDeploymentToRow) as unknown as Record<
        string,
        unknown
      >[];
    },
    dispatch,
  });

  // ─── Derived state ──────────────────────────────────────────────────────────

  const noModels =
    !state.isLoading && state.rowsData.length === 0 && !state.fetchError;

  const visibleHeaders = getVisibleHeaders(HEADERS, state.visibleColumns);

  const filterSlot = (
    <h6 className={sharedStyles.overflowMenuHeading}>Filter</h6>
  );

  // ─── Render ─────────────────────────────────────────────────────────────────

  return (
    <>
      <TableToasts
        toastOpen={false}
        deleteErrorRowName=""
        deleteErrorMessage=""
        entityLabel="model deployment"
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
                        onSearchChange={() => {
                          /* server-side search — not yet wired */
                        }}
                        onRefresh={() => fetchModels()}
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
                        filterSlot={filterSlot}
                        filterLabel="Filter"
                      >
                        <Button
                          kind="primary"
                          size="lg"
                          renderIcon={Deploy}
                          onClick={onDeploy}
                        >
                          Deploy
                        </Button>
                      </TableToolbarActions>

                      <Table
                        {...getTableProps()}
                        aria-label="Model deployments"
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

                        {!state.fetchError && !noModels && (
                          <TableBody>
                            {rows.map((row) => {
                              const { key: rowKey, ...rowProps } = getRowProps({
                                row,
                              });

                              const rowData = state.rowsData.find(
                                (r) => r.id === row.id,
                              ) as ModelDeploymentRow;

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
                                      rowData,
                                      onRowClick,
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
                        noData={noModels}
                        noSearchResults={false}
                        entityName="model deployment"
                        className={styles.noDataContent}
                      />
                    </TableContainer>

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
                            void fetchModels(page, pageSize);
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
          </Column>
        </Grid>
      </div>
    </>
  );
};

export default ModelsTable;
