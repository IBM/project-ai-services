import { Fragment, useReducer, useCallback, useMemo, useRef } from "react";
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
import DeleteModal from "@/components/Table/components/DeleteModal";
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
}

const DeployedServicesTable = ({
  onDeploy,
  refreshTrigger,
  onRowClick,
}: DeployedServicesTableProps) => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

  const pageRef = useRef(INITIAL_STATE.page);
  const pageSizeRef = useRef(INITIAL_STATE.pageSize);
  const selectedServicesRef = useRef(INITIAL_STATE.selectedServices);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;
  selectedServicesRef.current = state.selectedServices;

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

  // Fetch deployed services data
  const fetchDeployedServices = useCallback(
    async (
      page = pageRef.current,
      pageSize = pageSizeRef.current,
      selectedServices = selectedServicesRef.current,
    ) => {
      dispatch({ type: "SHARED_SET_LOADING", payload: true });
      dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: null });

      try {
        const { data: rawData, pagination } = await fetchDeployedServicesPage({
          page,
          pageSize,
          catalogId: selectedServices[0],
        });

        const totalItems = pagination?.total_items ?? rawData.length;
        const totalPages = pagination?.total_pages ?? 1;

        // If the current page is beyond total_pages (e.g. last item on page N was deleted),
        // jump back to the last valid page and re-fetch.
        if (page > totalPages && totalPages >= 1) {
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          fetchDeployedServices(totalPages, pageSize);
          return;
        }

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

  // Mount fetch + 2-minute auto-refresh interval (shared hook).
  // hasTransitionalRow activates the additional 5-second poll whenever any
  // row is in a transitional state (Deploying or Deleting).
  const hasTransitionalRow = state.rowsData.some(
    (row) =>
      row.status === "Deploying" ||
      row.status === "Deleting" ||
      row.status === "Downloading",
  );

  useAutoRefresh({
    fetchFn: fetchDeployedServices,
    hasData: state.rowsData.length > 0,
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
    searchFields: ["name", "status", "uptime", "messages", "service"],
    visibleColumns: state.visibleColumns,
    headers: HEADERS,
    fetchAllRows: async () => {
      const allData = await fetchAllDeployedServices(state.selectedServices[0]);
      return allData.map(transformDeployedServiceToRow) as unknown as Record<
        string,
        unknown
      >[];
    },
    dispatch,
  });

  // Apply search filter with shared utility
  const filteredRows = filterRowsBySearch<Record<string, unknown>>(
    state.rowsData as unknown as Record<string, unknown>[],
    state.search,
    ["name", "status", "uptime", "messages", "service"],
  ) as unknown as DeployedServicesRow[];

  const noApplications =
    !state.isLoading && state.rowsData.length === 0 && !state.fetchError;
  const noSearchResults =
    !state.isLoading && state.rowsData.length > 0 && filteredRows.length === 0;

  // Visible headers for the DataTable (shared utility)
  const visibleHeaders = getVisibleHeaders(HEADERS, state.visibleColumns);

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
                                        const rowData = filteredRows.find(
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

                    {!state.isLoading &&
                      state.totalItems > state.pageSize &&
                      filteredRows.length > 0 && (
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
                            void fetchDeployedServices(page, pageSize);
                          }}
                        />
                      )}
                  </>
                )}
              </DataTable>
            )}

            <DeleteModal
              isOpen={state.isDeleteDialogOpen}
              isDeleting={state.isDeleting}
              isConfirmed={state.isConfirmed}
              itemName={
                state.rowsData.find((r) => r.id === state.selectedRowId)
                  ?.name ?? ""
              }
              modalLabel="Delete service deployment"
              confirmLegend="Confirm service deployment to be deleted"
              warningText="Deleting a service deployment permanently deletes all associated components, including connected services, runtime metadata, and configurations. This action cannot be undone."
              onConfirm={() => handleDelete()}
              onClose={() => dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" })}
              onCheckboxChange={(checked) =>
                dispatch({
                  type: "SHARED_SET_CONFIRMED",
                  payload: checked,
                })
              }
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
