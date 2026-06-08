import React, { useReducer, useEffect } from "react";
import { api } from "@/api/axios";
import { APPLICATION_ENDPOINTS } from "@/constants";
import { NoDataEmptyState } from "@carbon/ibm-products";
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

// Generic cell renderer wrapper
interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction>;
  cellKey: string;
  cellProps: Record<string, unknown>;
}

const renderCell = ({
  header,
  value,
  rowId,
  dispatch,
  cellKey,
  cellProps,
}: RenderCellProps) => {
  const CellRenderer = CELL_RENDERERS[header as keyof typeof CELL_RENDERERS];

  return (
    <TableCell key={cellKey} {...cellProps}>
      {CellRenderer ? (
        <CellRenderer value={value} rowId={rowId} dispatch={dispatch} />
      ) : (
        String(value || "")
      )}
    </TableCell>
  );
};

const DeployedServicesTable = () => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

  // Fetch deployed services data
  const fetchDeployedServices = async () => {
    dispatch({
      type: ACTION_TYPES.DEPLOYED_SERVICES_SET_LOADING,
      payload: true,
    });

    try {
      const response = await api.get("/applications?deployment_type=services");
      console.log("Deployed Services Data:", response.data);

      // Transform API response to table row format
      if (response.data?.data) {
        const transformedRows = response.data.data.map(
          (app: ApplicationApiResponse) => {
            // Calculate uptime from created_at
            const createdDate = new Date(app.created_at);
            const now = new Date();
            const diffTime = Math.abs(now.getTime() - createdDate.getTime());
            const diffDays = Math.floor(diffTime / (1000 * 60 * 60 * 24));

            let uptime: string;
            if (diffDays === 0) {
              uptime = "Today";
            } else if (diffDays === 1) {
              uptime = "1 day";
            } else if (diffDays < 30) {
              uptime = `${diffDays} days`;
            } else {
              uptime = createdDate.toLocaleDateString("en-US", {
                year: "numeric",
                month: "short",
                day: "numeric",
              });
            }

            // Get first service data
            const firstService = app.services?.[0];
            const serviceName = firstService?.type || "";

            return {
              id: app.id,
              name: app.name,
              status: app.status,
              uptime: uptime,
              service: serviceName,
              messages: app.message || "",
              actions: "actions",
            };
          },
        );

        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_ROWS_DATA,
          payload: transformedRows,
        });
      } else {
        // No data returned
        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_ROWS_DATA,
          payload: [],
        });
      }

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
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_FETCH_ERROR,
        payload: errorMessage,
      });
    }
  };

  // Fetch deployed services data on component mount
  useEffect(() => {
    fetchDeployedServices();
  }, []);

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

    // Close modal immediately for better UX
    dispatch({ type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG });

    // Update row status to "Deleting..." so user sees progress in table
    dispatch({
      type: ACTION_TYPES.DEPLOYED_SERVICES_UPDATE_ROW_STATUS,
      payload: {
        id: rowId,
        status: "Deleting...",
        message: "Deletion in progress",
      },
    });

    dispatch({
      type: ACTION_TYPES.DEPLOYED_SERVICES_SET_IS_DELETING,
      payload: true,
    });

    try {
      const response = await api.delete(
        APPLICATION_ENDPOINTS.DELETE_APPLICATION(rowId),
      );

      if (response.status !== 202) {
        throw new Error(`Delete failed (${response.status})`);
      }

      // Check if response contains status information
      const responseData = response.data;

      // If deletion is complete or no status info, remove row
      if (!responseData?.status || responseData.status === "deleted") {
        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_DELETE_ROW,
          payload: rowId,
        });
      } else if (
        responseData.status === "deleting" ||
        responseData.status === "pending"
      ) {
        // Deletion is async, keep showing "Deleting..." status
        // In a real scenario, you might want to poll for status updates here
        dispatch({
          type: ACTION_TYPES.DEPLOYED_SERVICES_UPDATE_ROW_STATUS,
          payload: {
            id: rowId,
            status: "Deleting...",
            message: responseData.message || "Deletion in progress",
          },
        });
      }
    } catch (err) {
      // On error, update status to "Error" and show toast
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_UPDATE_ROW_STATUS,
        payload: {
          id: rowId,
          status: "Error",
          message: "Deletion failed",
        },
      });

      const msg =
        err instanceof Error
          ? err.message
          : "Failed deleting service deployment";
      const name = state.rowsData.find((r) => r.id === rowId)?.name ?? "";

      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SHOW_ERROR,
        payload: { message: msg, rowName: name },
      });
    } finally {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_IS_DELETING,
        payload: false,
      });
    }
  };

  const downloadCSV = async () => {
    const name = state.csvFileName.trim();

    // Validate filename before closing modal
    if (!name) {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORT_ERROR,
        payload: "Provide a valid file name",
      });
      return;
    }

    // Validate data before closing modal
    if (filteredRows.length === 0) {
      dispatch({
        type: ACTION_TYPES.DEPLOYED_SERVICES_SET_EXPORT_ERROR,
        payload: "No data available to export",
      });
      return;
    }

    // Close modal immediately
    dispatch({ type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG });

    // Use utility function to handle export
    const result = downloadCSVWithChildren(filteredRows, HEADERS, name);

    // Show toast based on result
    dispatch({
      type: ACTION_TYPES.DEPLOYED_SERVICES_SHOW_EXPORT_TOAST,
      payload: {
        message: result.message,
        kind: result.success ? "success" : "error",
      },
    });
  };

  const filteredRows = state.rowsData.filter((row) => {
    const matchesSearch = [
      row.name,
      row.status,
      row.uptime,
      row.messages,
      row.service,
    ]
      .join(" ")
      .toLowerCase()
      .includes(state.search.toLowerCase());

    const matchesServiceFilter =
      state.selectedServices.length === 0 ||
      state.selectedServices.includes(row.service);

    return matchesSearch && matchesServiceFilter;
  });

  const paginatedRows = filteredRows.slice(
    (state.page - 1) * state.pageSize,
    state.page * state.pageSize,
  );

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
                            onClick={fetchDeployedServices}
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
                              <CheckboxGroup legendText="">
                                <Checkbox
                                  labelText="Digitize Documents"
                                  id="filter-digitize"
                                  checked={state.selectedServices.includes(
                                    "Digitize Documents",
                                  )}
                                  onChange={() =>
                                    dispatch({
                                      type: ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER,
                                      payload: "Digitize Documents",
                                    })
                                  }
                                />
                                <Checkbox
                                  labelText="Find Similar Items"
                                  id="filter-similar"
                                  checked={state.selectedServices.includes(
                                    "Find Similar Items",
                                  )}
                                  onChange={() =>
                                    dispatch({
                                      type: ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER,
                                      payload: "Find Similar Items",
                                    })
                                  }
                                />
                                <Checkbox
                                  labelText="Question and Answer"
                                  id="filter-qa"
                                  checked={state.selectedServices.includes(
                                    "Question and Answer",
                                  )}
                                  onChange={() =>
                                    dispatch({
                                      type: ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER,
                                      payload: "Question and Answer",
                                    })
                                  }
                                />
                                <Checkbox
                                  labelText="Summarize"
                                  id="filter-summary"
                                  checked={state.selectedServices.includes(
                                    "Summarize",
                                  )}
                                  onChange={() =>
                                    dispatch({
                                      type: ACTION_TYPES.DEPLOYED_SERVICES_TOGGLE_SERVICE_FILTER,
                                      payload: "Summarize",
                                    })
                                  }
                                />
                              </CheckboxGroup>
                              <div className={styles.overflowMenuActions}>
                                <Button
                                  kind="secondary"
                                  size="sm"
                                  onClick={() =>
                                    dispatch({
                                      type: ACTION_TYPES.DEPLOYED_SERVICES_RESET_SERVICE_FILTER,
                                    })
                                  }
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
                            onClick={() => {
                              console.log("Deploy clicked");
                            }}
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

                                    return renderCell({
                                      header: cell.info.header,
                                      value: cell.value,
                                      rowId: row.id as string,
                                      dispatch,
                                      cellKey,
                                      cellProps,
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

                  {filteredRows.length > 20 && (
                    <Pagination
                      page={state.page}
                      pageSize={state.pageSize}
                      pageSizes={[5, 10, 20, 30]}
                      totalItems={filteredRows.length}
                      onChange={({ page, pageSize }) => {
                        dispatch({
                          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE,
                          payload: page,
                        });
                        dispatch({
                          type: ACTION_TYPES.DEPLOYED_SERVICES_SET_PAGE_SIZE,
                          payload: pageSize,
                        });
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
              primaryButtonText="Delete"
              secondaryButtonText="Cancel"
              danger
              primaryButtonDisabled={!state.isConfirmed}
              onRequestClose={() => {
                dispatch({
                  type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_DELETE_DIALOG,
                });
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
              primaryButtonText="Export"
              secondaryButtonText="Cancel"
              onRequestSubmit={downloadCSV}
              onRequestClose={() =>
                dispatch({
                  type: ACTION_TYPES.DEPLOYED_SERVICES_CLOSE_EXPORT_DIALOG,
                })
              }
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
