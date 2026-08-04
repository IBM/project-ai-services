import React, {
  useReducer,
  useEffect,
  useCallback,
  useRef,
  useMemo,
} from "react";
import { api } from "@/api/axios";
import { APPLICATION_ENDPOINTS } from "@/constants";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { NoDataEmptyState } from "@carbon/ibm-products";
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
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Pagination,
  Button,
  Grid,
  Column,
  Checkbox,
  CheckboxGroup,
  RadioButton,
  RadioButtonGroup,
  ActionableNotification,
  Modal,
  TextInput,
  OverflowMenu,
} from "@carbon/react";
import {
  Export,
  Column as ColumnIcon,
  Deploy,
  Filter,
  Renew,
} from "@carbon/icons-react";
import styles from "./DeployedServices.module.scss";
import type { DeployedServicesRow, ApplicationApiResponse } from "./types";
import { ACTION_TYPES, HEADERS, INITIAL_STATE, appReducer } from "./types";
import { CELL_RENDERERS } from "./CellRenderers";
import { downloadCSVWithChildren } from "@/utils/csv";
import type { Dispatch } from "react";
import type { AppAction } from "./types";
import { calculateUptime } from "@/api/applications.api";

// Generic cell renderer wrapper
interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction>;
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

  // Zustand store for services data
  const { setDeployedServicesLoading, setDeployedServicesError, services } =
    useServiceDeployStore();

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

  // Transform API response to table row format
  const transformDeployedServices = (data: ApplicationApiResponse[]) => {
    return data.map((app: ApplicationApiResponse) => {
      // Calculate uptime from created_at
      const uptime = calculateUptime(app.created_at);
      return {
        id: app.id,
        name: app.name,
        status: app.status,
        type: app.type,
        uptime: uptime,
        service: app.type || "",
        messages: app.message || "",
        actions: "actions",
      };
    });
  };

  // Fetch deployed services data
  const fetchDeployedServices = useCallback(
    async (
      page = state.page,
      pageSize = state.pageSize,
      selectedServices = state.selectedServices,
    ) => {
      setDeployedServicesLoading(true);
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_LOADING,
        payload: true,
      });

      try {
        const catalogIdParam =
          selectedServices.length > 0
            ? `&catalog_id=${selectedServices[0]}`
            : "";
        const response = await api.get(
          `${APPLICATION_ENDPOINTS.GET_DEPLOYED_SERVICES}&page=${page}&page_size=${pageSize}${catalogIdParam}`,
        );

        const rawData = response.data?.data || [];

        // Capture server-side total so Pagination knows the real count
        const totalItems =
          response.data?.pagination?.total_items ?? rawData.length;
        const totalPages = response.data?.pagination?.total_pages ?? 1;

        // If the current page is beyond total_pages (e.g. last item on page N was deleted),
        // jump back to the last valid page and re-fetch.
        if (page > totalPages && totalPages >= 1) {
          dispatch({
            type: ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE,
            payload: totalPages,
          });
          dispatch({
            type: ACTION_TYPES.DEPLOYED_SERVICES_SET_LOADING,
            payload: false,
          });
          fetchDeployedServices(totalPages, pageSize);
          return;
        }

        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_TOTAL_ITEMS,
          payload: totalItems,
        });

        // Transform and set in local state for table
        const transformedRows = transformDeployedServices(rawData);
        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_ROWS_DATA,
          payload: transformedRows,
        });

        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_LOADING,
          payload: false,
        });
      } catch (error) {
        console.error("Error fetching deployed services:", error);
        const errorMessage =
          error instanceof Error
            ? error.message
            : "Failed to fetch deployed services";
        setDeployedServicesError(errorMessage);
        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_FETCH_ERROR,
          payload: errorMessage,
        });
      }
    },
    [
      state.page,
      state.pageSize,
      state.selectedServices,
      setDeployedServicesError,
      setDeployedServicesLoading,
    ],
  );

  // Track if initial fetch has been done
  const hasFetchedRef = useRef(false);
  const prevRefreshTriggerRef = useRef(refreshTrigger);

  // Fetch deployed services data on component mount and when refreshTrigger changes
  useEffect(() => {
    // On mount: use cache if fresh (force = false)
    if (!hasFetchedRef.current) {
      hasFetchedRef.current = true;
      fetchDeployedServices();
      return;
    }

    // On refreshTrigger change: force refresh to get latest data
    if (refreshTrigger !== prevRefreshTriggerRef.current) {
      prevRefreshTriggerRef.current = refreshTrigger;
      fetchDeployedServices();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshTrigger]);

  // Auto-refresh every 2 minutes
  useEffect(() => {
    if (state.rowsData.length > 0) {
      const intervalId = setInterval(() => {
        fetchDeployedServices();
      }, 120000);
      return () => clearInterval(intervalId);
    }
  }, [state.rowsData.length, fetchDeployedServices]);

  // Auto-dismiss success toast after 5 seconds
  useEffect(() => {
    if (state.exportToastOpen && state.exportToastKind === "success") {
      const timer = setTimeout(() => {
        dispatch({ type: ACTION_TYPES.DEPLOYED_SERVICES_HIDE_EXPORT_TOAST });
      }, 5000);

      return () => clearTimeout(timer);
    }
  }, [state.exportToastOpen, state.exportToastKind]);

  const handleDelete = async () => {
    if (!state.selectedRowId) {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SHOW_ERROR,
        payload: { message: "No service selected for deletion" },
      });
      return;
    }

    const rowId = state.selectedRowId;

    // Set deleting flag to show loading state in modal
    dispatch({
      type: ACTION_TYPES.DEPLOYED_SERVICES_START_DELETING,
    });

    try {
      const response = await api.delete(
        APPLICATION_ENDPOINTS.DELETE_APPLICATION(rowId),
      );

      // API returns 202 Accepted for successful async deletion
      if (response.status === 202) {
        // Get the response data from the API
        const responseData = response.data;

        // Map API status to UI status format
        const mapApiStatusToUiStatus = (
          apiStatus: string | undefined,
        ): DeployedServicesRow["status"] => {
          if (!apiStatus) return "Deleting...";

          // Map API status values to UI status values
          switch (apiStatus.toLowerCase()) {
            case "deleting":
            case "pending":
              return "Deleting...";
            case "deleted":
              // If deleted, we'll remove the row after refresh
              return "Deleting...";
            case "error":
            case "failed":
              return "Error";
            default:
              return "Deleting...";
          }
        };

        // Update the table row with the API response data
        const uiStatus = mapApiStatusToUiStatus(responseData?.status);
        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_UPDATE_ROW_STATUS,
          payload: {
            id: rowId,
            status: uiStatus,
            message: responseData?.message || "Deletion in progress",
          },
        });
      } else {
        throw new Error(`Delete failed with status ${response.status}`);
      }
    } catch (err) {
      // On error, show error message
      const msg =
        err instanceof Error
          ? err.message
          : "Failed deleting service deployment";
      const name = state.rowsData.find((r) => r.id === rowId)?.name ?? "";

      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SHOW_ERROR,
        payload: { message: msg, rowName: name },
      });

      // Update row status to "Error"
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_UPDATE_ROW_STATUS,
        payload: {
          id: rowId,
          status: "Error",
          message: "Deletion failed",
        },
      });
    } finally {
      // Reset deleting flag
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_STOP_DELETING,
      });

      // Close modal after successful deletion or error
      dispatch({ type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG });
    }
  };

  const downloadCSV = async () => {
    const name = state.csvFileName.trim();

    if (!name) {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORT_ERROR,
        payload: "Provide a valid file name",
      });
      return;
    }

    if (state.totalItems === 0) {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORT_ERROR,
        payload: "No data available to export",
      });
      return;
    }

    // Show exporting state on modal button
    dispatch({
      type: ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORTING,
      payload: true,
    });

    try {
      const catalogIdParam =
        state.selectedServices.length > 0
          ? `&catalog_id=${state.selectedServices[0]}`
          : "";

      // Fetch all pages sequentially until has_next is false
      let currentPage = 1;
      let hasNext = true;
      const allData: ApplicationApiResponse[] = [];

      while (hasNext) {
        const response = await api.get(
          `${APPLICATION_ENDPOINTS.GET_DEPLOYED_SERVICES}&page=${currentPage}&page_size=100${catalogIdParam}`,
        );
        const pageData = response.data?.data || [];
        allData.push(...pageData);
        hasNext = response.data?.pagination?.has_next ?? false;
        currentPage++;
      }

      const allRows = transformDeployedServices(allData).filter((row) => {
        if (!state.search) return true;
        return [row.name, row.status, row.uptime, row.messages, row.service]
          .join(" ")
          .toLowerCase()
          .includes(state.search.toLowerCase());
      });

      const visibleHeaders = HEADERS.filter(
        (h) =>
          h.key !== "actions" &&
          state.visibleColumns[h.key as keyof typeof state.visibleColumns],
      );

      const result = downloadCSVWithChildren(
        allRows as DeployedServicesRow[],
        visibleHeaders,
        name,
      );

      dispatch({ type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG });
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SHOW_EXPORT_TOAST,
        payload: {
          message: result.message,
          kind: result.success ? "success" : "error",
        },
      });
    } catch {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SHOW_EXPORT_TOAST,
        payload: {
          message: "Failed to fetch data for export",
          kind: "error",
        },
      });
    } finally {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORTING,
        payload: false,
      });
    }
  };

  // Service filter is server-side (catalog_id param) — only search filters client-side
  const filteredRows = state.rowsData.filter((row) => {
    return [row.name, row.status, row.uptime, row.messages, row.service]
      .join(" ")
      .toLowerCase()
      .includes(state.search.toLowerCase());
  });

  // Server already returns the correct page — no client-side slice needed
  const paginatedRows = filteredRows;

  const noApplications =
    !state.isLoading && state.rowsData.length === 0 && !state.fetchError;
  const noSearchResults =
    !state.isLoading && state.rowsData.length > 0 && filteredRows.length === 0;

  return (
    <>
      {state.toastOpen && (
        <ActionableNotification
          actionButtonLabel="Try again"
          aria-label="close notification"
          kind="error"
          closeOnEscape
          title={`Delete service deployment  ${state.deleteErrorRowName} failed`}
          subtitle={state.deleteErrorMessage}
          onCloseButtonClick={() => {
            dispatch({ type: ACTION_TYPES.DEPLOYED_SERVICES_HIDE_ERROR });
          }}
          onActionButtonClick={async () => {
            const currentRowId = state.selectedRowId;
            dispatch({ type: ACTION_TYPES.DEPLOYED_SERVICES_HIDE_ERROR });
            dispatch({
              type: ACTION_TYPES.DEPLOYED_SERVICES_SET_SELECTED_ROW_ID,
              payload: currentRowId,
            });
            await handleDelete();
          }}
          className={styles.customToast}
        />
      )}
      {state.exportToastOpen && (
        <ActionableNotification
          aria-label="close notification"
          kind={state.exportToastKind}
          closeOnEscape
          title={
            state.exportToastKind === "success"
              ? "Export successful"
              : "Export failed"
          }
          subtitle={state.exportToastMessage}
          onCloseButtonClick={() => {
            dispatch({
              type: ACTION_TYPES.DEPLOYED_SERVICES_HIDE_EXPORT_TOAST,
            });
          }}
          className={styles.customToast}
          hideCloseButton={false}
        />
      )}

      <div className={styles.tableContent}>
        <Grid fullWidth>
          <Column lg={16} md={8} sm={4} className={styles.tableColumn}>
            <DataTable
              rows={paginatedRows}
              headers={HEADERS.filter(
                (h) =>
                  h.key === "actions" ||
                  state.visibleColumns[
                    h.key as keyof typeof state.visibleColumns
                  ],
              )}
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
                    {!state.isLoading && (
                      <TableToolbar>
                        <TableToolbarSearch
                          placeholder="Search"
                          persistent
                          value={state.search}
                          onChange={(e) => {
                            if (typeof e !== "string") {
                              dispatch({
                                type: ACTION_TYPES.DEPLOYED_SERVICES_SET_SEARCH,
                                payload: e.target.value,
                              });
                            }
                          }}
                        />

                        <TableToolbarContent>
                          <Button
                            hasIconOnly
                            kind="ghost"
                            renderIcon={Renew}
                            iconDescription="Refresh"
                            size="lg"
                            onClick={() => fetchDeployedServices()}
                          />
                          <OverflowMenu
                            renderIcon={Filter}
                            iconDescription="Filter by service"
                            aria-label="Filter by service"
                            size="lg"
                            flipped
                          >
                            <li
                              className={styles.overflowMenuContent}
                              role="none"
                            >
                              <h6 className={styles.overflowMenuHeading}>
                                Filter by service
                              </h6>
                              <RadioButtonGroup
                                legendText=""
                                name="service-filter"
                                orientation="vertical"
                                valueSelected={state.selectedServices[0] ?? ""}
                                onChange={(selection) => {
                                  const value = String(selection ?? "");
                                  if (!value) return;
                                  // Clicking the already-selected option deselects it
                                  const newSelected =
                                    state.selectedServices.includes(value)
                                      ? []
                                      : [value];
                                  dispatch({
                                    type: ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER,
                                    payload: value,
                                  });
                                  fetchDeployedServices(
                                    1,
                                    state.pageSize,
                                    newSelected,
                                  );
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
                              <div className={styles.overflowMenuActions}>
                                <Button
                                  kind="secondary"
                                  size="sm"
                                  onClick={() => {
                                    dispatch({
                                      type: ACTION_TYPES.DEPLOYED_SERVICES_RESET_SERVICE_FILTER,
                                    });
                                    fetchDeployedServices(
                                      1,
                                      state.pageSize,
                                      [],
                                    );
                                  }}
                                >
                                  Reset filter
                                </Button>
                              </div>
                            </li>
                          </OverflowMenu>
                          <Button
                            hasIconOnly
                            kind="ghost"
                            renderIcon={Export}
                            iconDescription="Export"
                            size="lg"
                            onClick={() =>
                              dispatch({
                                type: ACTION_TYPES.DEPLOYED_SERVICES_OPEN_EXPORT_DIALOG,
                              })
                            }
                          />
                          <OverflowMenu
                            renderIcon={ColumnIcon}
                            iconDescription="Edit columns"
                            aria-label="Edit columns"
                            size="lg"
                            flipped
                          >
                            <li
                              className={styles.overflowMenuContent}
                              role="none"
                            >
                              <h6 className={styles.overflowMenuHeading}>
                                Edit columns
                              </h6>
                              <CheckboxGroup legendText="">
                                {HEADERS.filter((h) => h.key !== "actions").map(
                                  (header) => (
                                    <Checkbox
                                      key={`column-${header.key}`}
                                      labelText={String(header.header)}
                                      id={`column-${header.key}`}
                                      checked={
                                        state.visibleColumns[
                                          header.key as keyof typeof state.visibleColumns
                                        ]
                                      }
                                      disabled={header.key === "name"}
                                      onChange={() =>
                                        dispatch({
                                          type: ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_COLUMN_VISIBILITY,
                                          payload: header.key,
                                        })
                                      }
                                    />
                                  ),
                                )}
                              </CheckboxGroup>
                              <div className={styles.overflowMenuActions}>
                                <Button
                                  kind="secondary"
                                  size="sm"
                                  onClick={() =>
                                    dispatch({
                                      type: ACTION_TYPES.DEPLOYED_SERVICES_RESET_COLUMN_VISIBILITY,
                                    })
                                  }
                                >
                                  Reset
                                </Button>
                              </div>
                            </li>
                          </OverflowMenu>
                          <Button
                            kind="primary"
                            size="lg"
                            renderIcon={Deploy}
                            onClick={onDeploy}
                          >
                            Deploy
                          </Button>
                        </TableToolbarContent>
                      </TableToolbar>
                    )}

                    {state.isLoading ? (
                      <DataTableSkeleton
                        headers={HEADERS.filter(
                          (h) =>
                            h.key === "actions" ||
                            state.visibleColumns[
                              h.key as keyof typeof state.visibleColumns
                            ],
                        )}
                        rowCount={5}
                        showHeader={false}
                        showToolbar={false}
                      />
                    ) : state.fetchError ? (
                      <NoDataEmptyState
                        title="Error loading services"
                        subtitle={state.fetchError}
                        className={styles.noDataContent}
                      />
                    ) : noApplications ? (
                      <NoDataEmptyState
                        title="Start by adding a service"
                        subtitle="To deploy a new service, click Deploy."
                        className={styles.noDataContent}
                      />
                    ) : noSearchResults ? (
                      <NoDataEmptyState
                        title="No data"
                        subtitle="Try adjusting your search or filter."
                        className={styles.noDataContent}
                      />
                    ) : (
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
                        <TableBody>
                          {rows.map((row) => {
                            const { key: rowKey, ...rowProps } = getRowProps({
                              row,
                            });

                            return (
                              <React.Fragment key={rowKey}>
                                <TableRow
                                  {...rowProps}
                                  isExpanded={row.isExpanded}
                                >
                                  {row.cells.map((cell) => {
                                    const { key: cellKey, ...cellProps } =
                                      getCellProps({ cell });

                                    // Find the full row data for this row
                                    const rowData = paginatedRows.find(
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
                              </React.Fragment>
                            );
                          })}
                        </TableBody>
                      </Table>
                    )}
                  </TableContainer>

                  {!state.isLoading &&
                    state.totalItems > 20 &&
                    filteredRows.length > 0 && (
                      <Pagination
                        page={state.page}
                        pageSize={state.pageSize}
                        pageSizes={[20, 30, 50]}
                        totalItems={state.totalItems}
                        onChange={({ page, pageSize }) => {
                          dispatch({
                            type: ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE,
                            payload: page,
                          });
                          dispatch({
                            type: ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE_SIZE,
                            payload: pageSize,
                          });
                          fetchDeployedServices(page, pageSize);
                        }}
                      />
                    )}
                </>
              )}
            </DataTable>

            <Modal
              open={state.isDeleteDialogOpen}
              size="sm"
              modalLabel="Delete service deployment"
              modalHeading="Confirm delete"
              primaryButtonText={state.isDeleting ? "Deleting..." : "Delete"}
              secondaryButtonText="Cancel"
              danger
              primaryButtonDisabled={!state.isConfirmed || state.isDeleting}
              onRequestClose={() => {
                // Prevent closing modal while deletion is in progress
                if (!state.isDeleting) {
                  dispatch({
                    type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG,
                  });
                }
              }}
              onRequestSubmit={handleDelete}
            >
              <p>
                Deleting a service deployment permanently deletes all associated
                components, including connected services, runtime metadata, and
                configurations. This action cannot be undone.
              </p>
              <div>
                <CheckboxGroup
                  className={styles.deleteConfirmation}
                  legendText="Confirm service deployment to be deleted"
                >
                  <Checkbox
                    id="checkbox-label-1"
                    labelText={
                      <strong>
                        {state.selectedRowId
                          ? state.rowsData.find(
                              (r: DeployedServicesRow) =>
                                r.id === state.selectedRowId,
                            )?.name
                          : ""}
                      </strong>
                    }
                    checked={state.isConfirmed}
                    onChange={(_, { checked }) =>
                      dispatch({
                        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_CONFIRMED,
                        payload: checked,
                      })
                    }
                  />
                </CheckboxGroup>
              </div>
            </Modal>
            <Modal
              open={state.isExportDialogOpen}
              size="sm"
              modalHeading="Export as CSV"
              primaryButtonText={state.isExporting ? "Exporting..." : "Export"}
              primaryButtonDisabled={state.isExporting}
              secondaryButtonText="Cancel"
              onRequestSubmit={downloadCSV}
              onRequestClose={() => {
                if (!state.isExporting)
                  dispatch({
                    type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG,
                  });
              }}
            >
              <TextInput
                id="csv-file-name"
                labelText="File name"
                value={state.csvFileName}
                invalid={!!state.exportErrorMessage}
                invalidText={state.exportErrorMessage}
                onChange={(e) => {
                  dispatch({
                    type: ACTION_TYPES.DEPLOYED_SERVICES_SET_CSV_FILENAME,
                    payload: e.target.value,
                  });
                  dispatch({
                    type: ACTION_TYPES.DEPLOYED_SERVICES_CLEAR_EXPORT_ERROR,
                  });
                }}
              />
            </Modal>
          </Column>
        </Grid>
      </div>
    </>
  );
};

export default DeployedServicesTable;
