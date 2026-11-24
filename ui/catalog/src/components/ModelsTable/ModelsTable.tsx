import { useReducer, useCallback, useRef, useState, useEffect } from "react";
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
import FilterPanel from "@/components/FilterPanel";
import type { FilterOption } from "@/components/FilterPanel";
import {
  fetchModelsPage,
  fetchAllModels,
  transformModelDeploymentToRow,
} from "@/api/models.api";
import styles from "./ModelsTable.module.scss";

const toggleValue = (list: string[], value: string) =>
  list.includes(value) ? list.filter((v) => v !== value) : [...list, value];

// Width class for a column, keyed by header id so hiding columns doesn't shift widths
const getColumnClassName = (header: string) => {
  if (header === "messages") return styles.messagesColumn;
  if (header === "actions") return styles.actionsColumn;
  return styles.compactColumn;
};

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
    <TableCell
      key={cellKey}
      {...cellProps}
      className={getColumnClassName(header)}
    >
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
  const selectedProvidersRef = useRef(INITIAL_STATE.selectedProviders);
  const selectedModelsRef = useRef(INITIAL_STATE.selectedModels);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;
  selectedProvidersRef.current = state.selectedProviders;
  selectedModelsRef.current = state.selectedModels;

  // Filter panel — distinct backends and models derived from all models
  const [backendOptions, setBackendOptions] = useState<FilterOption[]>([]);
  const [modelOptions, setModelOptions] = useState<FilterOption[]>([]);
  const [isFilterPanelOpen, setIsFilterPanelOpen] = useState(false);

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

  // Flips to true after the first successful fetch. Later loads dim the table
  // instead of showing the full skeleton.
  const hasEverLoadedRef = useRef(false);

  // ─── Data fetching ──────────────────────────────────────────────────────────

  const fetchModels = useCallback(
    async (
      page = pageRef.current,
      pageSize = pageSizeRef.current,
      providerIds = selectedProvidersRef.current,
      models = selectedModelsRef.current,
    ) => {
      // Skip the skeleton while rows are transitional so polling updates statuses in place
      if (!hasTransitionalRowRef.current) {
        dispatch({ type: "SHARED_SET_LOADING", payload: true });
      }
      dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: null });

      try {
        const { data: rawData, pagination } = await fetchModelsPage({
          page,
          pageSize,
          providerIds,
          models,
        });

        const totalItems = pagination?.total_items ?? rawData.length;
        const totalPages = pagination?.total_pages ?? 1;

        // If current page is beyond total_pages, jump back to last valid page
        if (page > totalPages && totalPages >= 1) {
          pageRef.current = totalPages;
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          void fetchModels(totalPages, pageSize, providerIds, models);
          return;
        }

        hasEverLoadedRef.current = true;
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

  // ─── Filters ────────────────────────────────────────────────────────────────

  const loadFilterOptions = useCallback(async () => {
    try {
      const allModels = await fetchAllModels();
      const backends = new Map<string, string>();
      const models = new Set<string>();
      allModels.forEach((m) => {
        backends.set(m.provider.id, m.provider.name);
        models.add(m.model);
      });
      setBackendOptions(
        [...backends].map(([id, name]) => ({ id, label: name })),
      );
      setModelOptions([...models].map((name) => ({ id: name })));
    } catch {
      // Filter options are non-critical; leave the lists empty on failure.
    }
  }, []);

  useEffect(() => {
    void loadFilterOptions();
  }, [loadFilterOptions]);

  const applyFilters = (providers: string[], models: string[]) => {
    dispatch({
      type: ACTION_TYPES.SET_FILTERS,
      payload: { providers, models },
    });
    pageRef.current = 1;
    void fetchModels(1, pageSizeRef.current, providers, models);
  };

  // ─── Auto-refresh ───────────────────────────────────────────────────────────

  // hasData is always true so auto-refresh keeps running when filters match nothing.
  useAutoRefresh({
    fetchFn: fetchModels,
    hasData: true,
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
      const allData = await fetchAllModels(
        state.selectedProviders,
        state.selectedModels,
      );
      return allData.map(transformModelDeploymentToRow) as unknown as Record<
        string,
        unknown
      >[];
    },
    dispatch,
  });

  // ─── Derived state ──────────────────────────────────────────────────────────

  const hasActiveFilters =
    state.selectedProviders.length > 0 || state.selectedModels.length > 0;

  const noRows =
    !state.isLoading && state.rowsData.length === 0 && !state.fetchError;
  const noModels = noRows && !hasActiveFilters;
  const noFilterResults = noRows && hasActiveFilters;

  const visibleHeaders = getVisibleHeaders(HEADERS, state.visibleColumns);

  // Full skeleton only on the very first load
  const showSkeleton = state.isLoading && !hasEverLoadedRef.current;

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
        {isFilterPanelOpen && (
          <FilterPanel
            title="Filters"
            ariaLabel="Model filters"
            groups={[
              {
                id: "model",
                title: "Model",
                options: modelOptions,
                selected: state.selectedModels,
                onToggle: (id) =>
                  applyFilters(
                    state.selectedProviders,
                    toggleValue(state.selectedModels, id),
                  ),
              },
              {
                id: "backend",
                title: "Backend",
                options: backendOptions,
                selected: state.selectedProviders,
                onToggle: (id) =>
                  applyFilters(
                    toggleValue(state.selectedProviders, id),
                    state.selectedModels,
                  ),
              },
            ]}
            onReset={() => applyFilters([], [])}
          />
        )}
        <Grid fullWidth className={styles.tableGrid}>
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
                        onSearchChange={() => {
                          // TODO: Wire server-side search
                        }}
                        onRefresh={() => {
                          void fetchModels();
                          void loadFilterOptions();
                        }}
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
                        filterLabel="Filter"
                        onFilterToggle={() =>
                          setIsFilterPanelOpen((open) => !open)
                        }
                        isFilterOpen={isFilterPanelOpen}
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
                        className={
                          state.isLoading ? styles.loadingTable : undefined
                        }
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
                                <TableHeader
                                  key={key}
                                  {...rest}
                                  className={getColumnClassName(header.key)}
                                >
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
                        noSearchResults={noFilterResults}
                        entityName="model deployment"
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
