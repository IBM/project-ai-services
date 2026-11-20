import {
  Fragment,
  useReducer,
  useCallback,
  useEffect,
  useMemo,
  useRef,
} from "react";
import axios from "axios";
import {
  deleteApplication,
  fetchDeployedServicesPage,
  fetchAllDeployedServices,
  transformDeployedServiceToRow,
} from "@/api/applications.api";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import type { DeploymentDetails } from "@/types/api.types";
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
  Button,
  Grid,
  Column,
  RadioButton,
  RadioButtonGroup,
} from "@carbon/react";
import { Deploy } from "@carbon/icons-react";
import styles from "./DeployedServices.module.scss";
import { DEPLOYMENT_STATUS } from "@/constants";
import type { DeployedServicesRow } from "./types";
import {
  ACTION_TYPES,
  DEFAULT_VISIBLE_COLUMNS,
  HEADERS,
  INITIAL_STATE,
  appReducer,
} from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import { CELL_RENDERERS } from "./CellRenderers";
import type { Dispatch } from "react";
import type { AppAction } from "./types";
import TableToolbarActions from "@/components/Table/components/TableToolbarActions";
import DeleteConfirmNameModal from "@/components/DeleteConfirmNameModal/DeleteConfirmNameModal";
import ExportModal from "@/components/Table/components/ExportModal";
import TableToasts from "@/components/Table/components/TableToasts";
import TableEmptyStates from "@/components/Table/components/TableEmptyStates";
import { useAutoRefresh } from "@/components/Table/hooks/useAutoRefresh";
import { useCSVExport } from "@/components/Table/hooks/useCSVExport";
import { useExportToastAutoDismiss } from "@/components/Table/hooks/useExportToastAutoDismiss";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { getVisibleHeaders } from "@/components/Table/utils/tableUtils";
import sharedStyles from "@/components/Table/table.shared.module.scss";

// Generic cell renderer wrapper
interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
  cellKey: string;
  cellProps: Record<string, unknown>;
  rowData: DeployedServicesRow;
  onRowClick?: (deployment: DeploymentDetails) => void;
  onViewIntegration?: (deployment: DeploymentDetails) => void;
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
  onViewIntegration,
}: RenderCellProps) => {
  const CellRenderer = CELL_RENDERERS[header as keyof typeof CELL_RENDERERS];
  const cellDispatch = dispatch as Parameters<
    typeof CellRenderer
  >[0]["dispatch"];

  return (
    <TableCell key={cellKey} {...cellProps}>
      {CellRenderer ? (
        <CellRenderer
          value={value}
          rowId={rowId}
          dispatch={cellDispatch}
          rowData={rowData}
          onRowClick={header === "name" ? onRowClick : undefined}
          onViewIntegration={
            header === "actions" && onViewIntegration
              ? () =>
                  onViewIntegration({
                    id: rowData.id,
                    name: rowData.name,
                    status: rowData.status,
                    type: rowData.type ?? "Service",
                  })
              : undefined
          }
        />
      ) : (
        String(value || "")
      )}
    </TableCell>
  );
};

interface DeployedServicesTableProps {
  onDeploy?: () => void;
  refreshTrigger?: number;
  onRowClick?: (deployment: DeploymentDetails) => void;
  onViewIntegration?: (deployment: DeploymentDetails) => void;
}

const DeployedServicesTable = ({
  onDeploy,
  refreshTrigger,
  onRowClick,
  onViewIntegration,
}: DeployedServicesTableProps) => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

  const pageRef = useRef(INITIAL_STATE.page);
  const pageSizeRef = useRef(INITIAL_STATE.pageSize);
  const selectedServicesRef = useRef(INITIAL_STATE.selectedServices);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;
  selectedServicesRef.current = state.selectedServices;

  const debouncedSearch = useDebouncedValue(state.search.trim(), 400);
  const debouncedSearchRef = useRef(debouncedSearch);
  debouncedSearchRef.current = debouncedSearch;

  const abortControllerRef = useRef<AbortController | null>(null);
  // Flips to true after the very first successful fetch. Used by showSkeleton
  // so that all subsequent loads use the dimming overlay instead of the full skeleton.
  const hasEverLoadedRef = useRef(false);

  const { services } = useServiceDeployStore();

  // Generate dynamic service filter options from backend services
  // Only show services where standalone === true
  const availableServiceFilters = useMemo(() => {
    if (!services || services.length === 0) return [];

    return services
      .filter((service) => service.standalone === true)
      .map((service) => ({
        id: service.id,
        name: service.name,
      }));
  }, [services]);

  // hasTransitionalRow activates the additional 5-second poll whenever any
  // row is in a transitional state (Deploying or Deleting).
  const hasTransitionalRow = state.rowsData.some(
    (row) =>
      row.status === DEPLOYMENT_STATUS.DEPLOYING ||
      row.status === DEPLOYMENT_STATUS.DELETING ||
      row.status === DEPLOYMENT_STATUS.DOWNLOADING,
  );
  const hasTransitionalRowRef = useRef(hasTransitionalRow);
  hasTransitionalRowRef.current = hasTransitionalRow;

  // `silent` skips the loading state; only background refreshes set it
  const fetchDeployedServices = useCallback(
    async (
      page = pageRef.current,
      pageSize = pageSizeRef.current,
      selectedServices = selectedServicesRef.current,
      search = debouncedSearchRef.current,
      options?: { silent?: boolean },
    ) => {
      const silent = options?.silent ?? false;

      // Cancel previous pending requests
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
      const controller = new AbortController();
      abortControllerRef.current = controller;

      if (!silent) {
        dispatch({ type: "SHARED_SET_LOADING", payload: true });
      }
      dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: null });

      try {
        const { data: rawData, pagination } = await fetchDeployedServicesPage(
          {
            page,
            pageSize,
            catalogId: selectedServices[0],
          },
          search,
          { signal: controller.signal },
        );

        const totalItems = pagination?.total_items ?? rawData.length;
        const totalPages = pagination?.total_pages ?? 1;

        // If the current page is beyond total_pages (e.g. last item on page N was deleted),
        // jump back to the last valid page and re-fetch.
        if (page > totalPages && totalPages >= 1) {
          pageRef.current = totalPages;
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          void fetchDeployedServices(
            totalPages,
            pageSize,
            selectedServices,
            search,
            { silent },
          );
          return;
        }

        hasEverLoadedRef.current = true;
        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_TOTAL_ITEMS,
          payload: totalItems,
        } as AppAction);

        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_ROWS_DATA,
          payload: rawData.map(transformDeployedServiceToRow),
        } as AppAction);

        dispatch({ type: "SHARED_SET_LOADING", payload: false });
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
            : "Failed to fetch deployed services";
        dispatch({ type: "SHARED_SET_LOADING", payload: false });
        dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: errorMessage });
      }
    },
    [],
  );

  // Used by useAutoRefresh; silent while rows are transitional
  const refreshDeployedServices = useCallback(
    () =>
      fetchDeployedServices(
        pageRef.current,
        pageSizeRef.current,
        selectedServicesRef.current,
        debouncedSearchRef.current,
        { silent: hasTransitionalRowRef.current },
      ),
    [fetchDeployedServices],
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
    void fetchDeployedServices(
      1,
      pageSizeRef.current,
      selectedServicesRef.current,
      debouncedSearch,
    );
  }, [debouncedSearch, fetchDeployedServices]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, []);

  // Mount fetch + 2-minute auto-refresh interval (shared hook).
  // hasData is always true so auto-refresh keeps running on empty/no-match results.
  useAutoRefresh({
    fetchFn: refreshDeployedServices,
    hasData: true,
    isPaused: state.isDeleteDialogOpen || state.isDeleting,
    refreshTrigger,
    hasTransitionalRow,
  });

  // Auto-dismiss success export toast after 5 seconds (shared hook)
  useExportToastAutoDismiss({
    exportToastOpen: state.exportToastOpen,
    exportToastKind: state.exportToastKind,
    onDismiss: () => dispatch({ type: "SHARED_HIDE_EXPORT_TOAST" }),
  });

  const handleDelete = async () => {
    if (!state.selectedRowId) {
      dispatch({
        type: "SHARED_SHOW_ERROR",
        payload: { message: "No service selected for deletion" },
      });
      return;
    }

    dispatch({ type: "SHARED_SET_DELETING", payload: true });

    try {
      await deleteApplication(state.selectedRowId);
      dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" });
      await fetchDeployedServices();
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : "Failed deleting service deployment";
      const name =
        state.rowsData.find((r) => r.id === state.selectedRowId)?.name ?? "";
      dispatch({
        type: "SHARED_SHOW_ERROR",
        payload: { message: msg, rowName: name },
      });
    } finally {
      dispatch({ type: "SHARED_SET_DELETING", payload: false });
    }
  };

  // CSV export — shared hook handles multi-page fetch, filter, download
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
      const allData = await fetchAllDeployedServices(
        state.selectedServices[0],
        state.search.trim(),
      );
      return allData.map(transformDeployedServiceToRow) as unknown as Record<
        string,
        unknown
      >[];
    },
    dispatch,
  });

  const noApplications =
    state.rowsData.length === 0 &&
    !state.isLoading &&
    !state.fetchError &&
    !debouncedSearch;
  const noSearchResults =
    state.rowsData.length === 0 &&
    !state.isLoading &&
    !state.fetchError &&
    !!debouncedSearch;

  // Visible headers for the DataTable (shared utility)
  const visibleHeaders = getVisibleHeaders(HEADERS, state.visibleColumns);

  const selectedItemName =
    state.rowsData.find((r) => r.id === state.selectedRowId)?.name ?? "";
  // Show the full skeleton only on the very first load
  const showSkeleton = state.isLoading && !hasEverLoadedRef.current;

  // Service filter slot — injected into the shared toolbar's filterSlot prop
  const serviceFilterSlot = (
    <>
      <h6 className={sharedStyles.overflowMenuHeading}>Filter by service</h6>
      <RadioButtonGroup
        legendText=""
        name="service-filter"
        orientation="vertical"
        valueSelected={state.selectedServices[0] ?? ""}
        onChange={(selection) => {
          const value = String(selection ?? "");
          if (!value) return;
          dispatch({
            type: ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER,
            payload: value,
          } as AppAction);
          pageRef.current = 1;
          void fetchDeployedServices(1, pageSizeRef.current, [value]);
        }}
      >
        {availableServiceFilters.map((service) => (
          <RadioButton
            key={service.id}
            labelText={service.name}
            value={service.id}
            id={`filter-${service.id}`}
          />
        ))}
      </RadioButtonGroup>
      <div className={sharedStyles.overflowMenuActions}>
        <Button
          kind="secondary"
          size="sm"
          onClick={() => {
            dispatch({
              type: ACTION_TYPES.DEPLOYED_SERVICES_RESET_SERVICE_FILTER,
            } as AppAction);
            pageRef.current = 1;
            void fetchDeployedServices(1, pageSizeRef.current, []);
          }}
        >
          Reset filter
        </Button>
      </div>
    </>
  );

  return (
    <>
      <TableToasts
        // Delete error toast
        toastOpen={state.toastOpen}
        deleteErrorRowName={state.deleteErrorRowName}
        deleteErrorMessage={state.deleteErrorMessage}
        entityLabel="service deployment"
        onDeleteErrorClose={() => dispatch({ type: "SHARED_HIDE_ERROR" })}
        onDeleteErrorRetry={async () => {
          const currentRowId = state.selectedRowId;
          dispatch({ type: "SHARED_HIDE_ERROR" });
          dispatch({
            type: "SHARED_SET_SELECTED_ROW_ID",
            payload: currentRowId,
          });
          await handleDelete();
        }}
        // Export toast
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
                        onRefresh={() => fetchDeployedServices()}
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
                        filterSlot={serviceFilterSlot}
                        filterLabel="Filter by service"
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
                        {!state.fetchError &&
                          !noApplications &&
                          !noSearchResults && (
                            <TableBody>
                              {rows.map((row) => {
                                const { key: rowKey, ...rowProps } =
                                  getRowProps({ row });

                                return (
                                  <Fragment key={rowKey}>
                                    <TableRow
                                      {...rowProps}
                                      isExpanded={row.isExpanded}
                                    >
                                      {row.cells.map((cell) => {
                                        const { key: cellKey, ...cellProps } =
                                          getCellProps({ cell });

                                        // Find the full row data for this row
                                        const rowData = state.rowsData.find(
                                          (r) => r.id === row.id,
                                        ) as DeployedServicesRow;

                                        return renderCell({
                                          header: cell.info.header,
                                          value: cell.value,
                                          rowId: row.id as string,
                                          dispatch,
                                          cellKey,
                                          cellProps,
                                          rowData,
                                          onRowClick,
                                          onViewIntegration,
                                        });
                                      })}
                                    </TableRow>
                                  </Fragment>
                                );
                              })}
                            </TableBody>
                          )}
                      </Table>

                      <TableEmptyStates
                        fetchError={state.fetchError}
                        noData={noApplications}
                        noSearchResults={noSearchResults}
                        entityName="service"
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
                            void fetchDeployedServices(page, pageSize);
                          }}
                        />
                      )}
                  </>
                )}
              </DataTable>
            )}

            <DeleteConfirmNameModal
              isOpen={state.isDeleteDialogOpen}
              isDeleting={state.isDeleting}
              itemName={selectedItemName}
              confirmValue={state.confirmValue}
              modalLabel={`Delete ${selectedItemName}`}
              primaryButtonLabel="Delete"
              primaryButtonLoadingLabel="Deleting..."
              warningText="Deleting a service deployment permanently removes all associated components, including connected services, runtime metadata, and configurations, and cannot be undone."
              onConfirmValueChange={(value) =>
                dispatch({ type: "SHARED_SET_CONFIRM_VALUE", payload: value })
              }
              onConfirm={handleDelete}
              onClose={() => dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" })}
            />

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

export default DeployedServicesTable;
