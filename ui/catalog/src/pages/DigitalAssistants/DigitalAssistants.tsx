import React, { useReducer, useEffect } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { useDeployOptions } from "@/hooks/useDeployOptions";
import { PageHeader, NoDataEmptyState } from "@carbon/ibm-products";
import {
  DataTable,
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
  TableExpandHeader,
  TableExpandRow,
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
  Tabs,
  TabList,
  Tab,
  TabPanels,
  TabPanel,
  DataTableSkeleton,
} from "@carbon/react";
import {
  Export,
  Column as ColumnIcon,
  Deploy,
  Reset,
} from "@carbon/icons-react";
import styles from "./DigitalAssistants.module.scss";
import type { DigitalAssistantRow } from "./types";
import { ACTION_TYPES, HEADERS, INITIAL_STATE, appReducer } from "./types";
import { CELL_RENDERERS, StatusCell } from "./CellRenderers";
import { downloadCSVWithChildren } from "@/utils/csv";
import type { Dispatch } from "react";
import type { AppAction } from "./types";
import { DeployFlow } from "@/components/DeployFlow";
import {
  fetchApplications,
  deleteApplication,
  transformApplicationToRow,
} from "@/api/digitalAssistants";
import { AboutTab } from "./components/AboutTab";
import DeploymentDetails from "@/components/DeploymentDetails";

// Generic cell renderer wrapper
interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction>;
  cellKey: string;
  cellProps: Record<string, unknown>;
  rowData?: DigitalAssistantRow;
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
  const CellRenderer = CELL_RENDERERS[header as keyof typeof CELL_RENDERERS];

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
        String(value || "")
      )}
    </TableCell>
  );
};

const DigitalAssistantsPage = () => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

  // Get deploy options with automatic cache management
  const { deployOptions: deployOptionsData } = useDeployOptions();
  const catalogId = deployOptionsData?.id;

  // Get architecture data from store for dynamic title and subtitle
  const architectures = useDeployStore((state) => state.architectures);
  const selectedArchitectureId = useDeployStore(
    (state) => state.selectedArchitectureId,
  );

  // Find the selected architecture to get name and description
  const selectedArchitecture = architectures.find(
    (arch) => arch.id === selectedArchitectureId,
  );

  // Use architecture data or fallback to defaults
  const pageTitle = selectedArchitecture?.name || "Digital assistants";
  const pageSubtitle =
    selectedArchitecture?.description ||
    "Production-ready tools that help users complete tasks and access information through conversation or commands. Assistants integrate multiple services for complex use cases and support retrieval-augmented generation (RAG).";

  // Fetch applications from API
  const loadApplications = async () => {
    // Don't fetch if we don't have a catalog_id yet
    if (!catalogId) {
      return;
    }

    dispatch({ type: ACTION_TYPES.FETCH_APPLICATIONS_START });

    try {
      const response = await fetchApplications({
        page: state.page,
        page_size: state.pageSize,
        catalog_id: catalogId,
      });

      const rows = response.data.map(transformApplicationToRow);

      dispatch({
        type: ACTION_TYPES.FETCH_APPLICATIONS_SUCCESS,
        payload: {
          rows,
          pagination: response.pagination,
        },
      });
    } catch (error) {
      const errorMessage =
        error instanceof Error ? error.message : "Failed to load applications";
      dispatch({
        type: ACTION_TYPES.FETCH_APPLICATIONS_ERROR,
        payload: errorMessage,
      });
    }
  };

  // Load applications on mount and when page/pageSize/catalogId changes
  useEffect(() => {
    loadApplications();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.page, state.pageSize, catalogId]);

  //auto-refresh every 2 minutes
  useEffect(() => {
    if (catalogId && state.rowsData.length > 0) {
      const intervalId = setInterval(() => {
        loadApplications();
      }, 120000);
      return () => clearInterval(intervalId);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [catalogId, state.rowsData.length]);

  const handleDeploySubmit = () => {
    loadApplications();
  };

  // Auto-dismiss success toast after 5 seconds
  useEffect(() => {
    if (state.exportToastOpen && state.exportToastKind === "success") {
      const timer = setTimeout(() => {
        dispatch({ type: ACTION_TYPES.HIDE_EXPORT_TOAST });
      }, 5000);

      return () => clearTimeout(timer);
    }
  }, [state.exportToastOpen, state.exportToastKind]);

  const handleDelete = async () => {
    if (!state.selectedRowId) {
      dispatch({
        type: ACTION_TYPES.SHOW_ERROR,
        payload: { message: "No digital assistant selected for deletion" },
      });
      return;
    }

    dispatch({ type: ACTION_TYPES.SET_IS_DELETING, payload: true });

    try {
      await deleteApplication(state.selectedRowId);
      dispatch({ type: ACTION_TYPES.CLOSE_DELETE_DIALOG });
      await loadApplications();
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : "Failed deleting digital assistant";
      const name =
        state.rowsData.find((r) => r.id === state.selectedRowId)?.name ?? "";
      dispatch({
        type: ACTION_TYPES.SHOW_ERROR,
        payload: { message: msg, rowName: name },
      });
    } finally {
      dispatch({ type: ACTION_TYPES.SET_IS_DELETING, payload: false });
    }
  };

  const downloadCSV = async () => {
    const name = state.csvFileName.trim();

    // Validate filename before closing modal
    if (!name) {
      dispatch({
        type: ACTION_TYPES.SET_EXPORT_ERROR,
        payload: "Provide a valid file name",
      });
      return;
    }

    // Validate data before closing modal
    if (filteredRows.length === 0) {
      dispatch({
        type: ACTION_TYPES.SET_EXPORT_ERROR,
        payload: "No data available to export",
      });
      return;
    }

    // Close modal immediately
    dispatch({ type: ACTION_TYPES.CLOSE_EXPORT_DIALOG });

    // Use utility function to handle export
    const result = downloadCSVWithChildren(filteredRows, HEADERS, name);

    // Show toast based on result
    dispatch({
      type: ACTION_TYPES.SHOW_EXPORT_TOAST,
      payload: {
        message: result.message,
        kind: result.success ? "success" : "error",
      },
    });
  };

  const filteredRows = state.rowsData.filter((row) => {
    if (!state.search) return true;
    const matchesSearch = [row.name, row.status, row.uptime, row.messages]
      .join(" ")
      .toLowerCase()
      .includes(state.search.toLowerCase());
    return matchesSearch;
  });

  const noApplications =
    state.rowsData.length === 0 && !state.isLoadingApplications;
  const noSearchResults =
    state.rowsData.length > 0 && filteredRows.length === 0;

  // Show DeploymentDetails if a deployment is selected
  if (state.showDeploymentDetails && state.selectedDeployment) {
    return (
      <DeploymentDetails
        deployment={state.selectedDeployment}
        onBack={() => dispatch({ type: ACTION_TYPES.HIDE_DEPLOYMENT_DETAILS })}
        deploymentSource="Digital assistants"
        onNameUpdate={(newName) =>
          dispatch({
            type: ACTION_TYPES.UPDATE_DEPLOYMENT_NAME,
            payload: newName,
          })
        }
      />
    );
  }

  return (
    <>
      {state.toastOpen && (
        <ActionableNotification
          actionButtonLabel="Try again"
          aria-label="close notification"
          kind="error"
          closeOnEscape
          title={`Delete digital assistant ${state.deleteErrorRowName} failed`}
          subtitle={state.deleteErrorMessage}
          onCloseButtonClick={() => {
            dispatch({ type: ACTION_TYPES.HIDE_ERROR });
          }}
          onActionButtonClick={async () => {
            const currentRowId = state.selectedRowId;
            dispatch({ type: ACTION_TYPES.HIDE_ERROR });
            dispatch({
              type: ACTION_TYPES.SET_SELECTED_ROW_ID,
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
            dispatch({ type: ACTION_TYPES.HIDE_EXPORT_TOAST });
          }}
          className={styles.customToast}
          hideCloseButton={false}
        />
      )}
      <Tabs>
        <PageHeader
          title={{ text: pageTitle }}
          subtitle={pageSubtitle}
          fullWidthGrid="xl"
          navigation={
            <TabList aria-label="Digital assistants tabs">
              <Tab>Deployments</Tab>
              <Tab>About</Tab>
            </TabList>
          }
        />

        <TabPanels>
          <TabPanel>
            <div className={styles.tableContent}>
              <Grid fullWidth>
                <Column lg={16} md={8} sm={4} className={styles.tableColumn}>
                  {state.isLoadingApplications ? (
                    <DataTableSkeleton
                      headers={HEADERS}
                      rowCount={state.pageSize}
                      columnCount={HEADERS.length}
                    />
                  ) : (
                    <DataTable
                      rows={filteredRows}
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
                        getExpandHeaderProps,
                        getCellProps,
                        getTableProps,
                      }) => (
                        <>
                          <TableContainer>
                            <TableToolbar>
                              <TableToolbarSearch
                                placeholder="Search"
                                persistent
                                value={state.search}
                                onChange={(e) => {
                                  if (typeof e !== "string") {
                                    dispatch({
                                      type: ACTION_TYPES.SET_SEARCH,
                                      payload: e.target.value,
                                    });
                                  }
                                }}
                              />

                              <TableToolbarContent>
                                <Button
                                  hasIconOnly
                                  kind="ghost"
                                  renderIcon={Reset}
                                  iconDescription="Refresh"
                                  size="lg"
                                  onClick={loadApplications}
                                />
                                <Button
                                  hasIconOnly
                                  kind="ghost"
                                  renderIcon={Export}
                                  iconDescription="Export"
                                  size="lg"
                                  onClick={() =>
                                    dispatch({
                                      type: ACTION_TYPES.OPEN_EXPORT_DIALOG,
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
                                      {HEADERS.filter(
                                        (h) => h.key !== "actions",
                                      ).map((header) => (
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
                                              type: ACTION_TYPES.TOGGLE_COLUMN_VISIBILITY,
                                              payload: header.key,
                                            })
                                          }
                                        />
                                      ))}
                                    </CheckboxGroup>
                                    <div className={styles.overflowMenuActions}>
                                      <Button
                                        kind="secondary"
                                        size="sm"
                                        onClick={() =>
                                          dispatch({
                                            type: ACTION_TYPES.RESET_COLUMN_VISIBILITY,
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
                                  onClick={() =>
                                    dispatch({
                                      type: ACTION_TYPES.OPEN_DEPLOY_FLOW,
                                    })
                                  }
                                >
                                  Deploy
                                </Button>
                              </TableToolbarContent>
                            </TableToolbar>

                            <Table {...getTableProps()}>
                              <TableHead>
                                <TableRow>
                                  <TableExpandHeader
                                    {...getExpandHeaderProps()}
                                  />
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
                              {!noApplications && !noSearchResults && (
                                <TableBody>
                                  {rows.map((row) => {
                                    const { key: rowKey, ...rowProps } =
                                      getRowProps({
                                        row,
                                      });
                                    const originalRow = filteredRows.find(
                                      (r: DigitalAssistantRow) =>
                                        r.id === row.id,
                                    );
                                    const hasChildren =
                                      originalRow?.children &&
                                      originalRow.children.length > 0;

                                    return (
                                      <React.Fragment key={rowKey}>
                                        <TableExpandRow
                                          {...rowProps}
                                          isExpanded={row.isExpanded}
                                        >
                                          {row.cells.map((cell) => {
                                            const {
                                              key: cellKey,
                                              ...cellProps
                                            } = getCellProps({ cell });

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
                                        </TableExpandRow>
                                        {hasChildren &&
                                          row.isExpanded &&
                                          originalRow.children?.map(
                                            (child: DigitalAssistantRow) => (
                                              <TableRow key={child.id}>
                                                <TableCell />
                                                <TableCell>
                                                  {child.name}
                                                </TableCell>
                                                <TableCell>
                                                  <StatusCell
                                                    value={child.status}
                                                    rowId={child.id}
                                                    dispatch={dispatch}
                                                  />
                                                </TableCell>
                                                <TableCell />
                                                <TableCell />
                                                <TableCell />
                                              </TableRow>
                                            ),
                                          )}
                                      </React.Fragment>
                                    );
                                  })}
                                </TableBody>
                              )}
                            </Table>
                            {noApplications && (
                              <NoDataEmptyState
                                title="Start by adding a digital assistant"
                                subtitle="To deploy a digital assistant, click Deploy."
                                className={styles.noDataContent}
                              />
                            )}
                            {noSearchResults && (
                              <NoDataEmptyState
                                title="No data"
                                subtitle="Try adjusting your search or filter."
                                className={styles.noDataContent}
                              />
                            )}
                          </TableContainer>

                          {state.totalItems > state.pageSize && (
                            <Pagination
                              page={state.page}
                              pageSize={state.pageSize}
                              pageSizes={[10, 20, 30, 50]}
                              totalItems={state.totalItems}
                              onChange={({ page, pageSize }) => {
                                dispatch({
                                  type: ACTION_TYPES.SET_PAGE,
                                  payload: page,
                                });
                                dispatch({
                                  type: ACTION_TYPES.SET_PAGE_SIZE,
                                  payload: pageSize,
                                });
                              }}
                            />
                          )}
                        </>
                      )}
                    </DataTable>
                  )}

                  <Modal
                    open={state.isDeleteDialogOpen}
                    size="sm"
                    modalLabel="Delete digital assistant deployment"
                    modalHeading="Confirm delete"
                    primaryButtonText={
                      state.isDeleting ? "Deleting..." : "Delete"
                    }
                    secondaryButtonText="Cancel"
                    danger
                    primaryButtonDisabled={
                      !state.isConfirmed || state.isDeleting
                    }
                    onRequestClose={() => {
                      if (!state.isDeleting) {
                        dispatch({ type: ACTION_TYPES.CLOSE_DELETE_DIALOG });
                      }
                    }}
                    onRequestSubmit={handleDelete}
                  >
                    <p>
                      Deleting a digital assistant deployment permanently
                      deletes all associated components, including connected
                      services, runtime metadata, and configurations will be
                      permanently deleted, and it cannot be undone.
                    </p>
                    <div>
                      <CheckboxGroup
                        className={styles.deleteConfirmation}
                        legendText="Confirm digital assistant deployment to be deleted"
                      >
                        <Checkbox
                          id="checkbox-label-1"
                          labelText={
                            <strong>
                              {state.selectedRowId
                                ? state.rowsData.find(
                                    (r: DigitalAssistantRow) =>
                                      r.id === state.selectedRowId,
                                  )?.name
                                : ""}
                            </strong>
                          }
                          checked={state.isConfirmed}
                          onChange={(_, { checked }) =>
                            dispatch({
                              type: ACTION_TYPES.SET_CONFIRMED,
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
                      dispatch({ type: ACTION_TYPES.CLOSE_EXPORT_DIALOG })
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
                          type: ACTION_TYPES.SET_CSV_FILENAME,
                          payload: e.target.value,
                        });
                        dispatch({ type: ACTION_TYPES.CLEAR_EXPORT_ERROR });
                      }}
                    />
                  </Modal>
                </Column>
              </Grid>
            </div>
          </TabPanel>
          <TabPanel>
            <AboutTab
              onDeployClick={() =>
                dispatch({ type: ACTION_TYPES.OPEN_DEPLOY_FLOW })
              }
            />
          </TabPanel>
        </TabPanels>
      </Tabs>
      <DeployFlow
        open={state.isDeployFlowOpen}
        onClose={() => dispatch({ type: ACTION_TYPES.CLOSE_DEPLOY_FLOW })}
        onSubmit={handleDeploySubmit}
      />
    </>
  );
};

export default DigitalAssistantsPage;
