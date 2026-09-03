import { Fragment, useReducer, useCallback, useRef } from "react";
import { useDeployStore } from "@/store/deploy.store";
import { useDeployOptions } from "@/components/DeployFlow/DigitalAssistant/hooks/useDeployOptions";
import { PageHeader } from "@carbon/ibm-products";
import {
  DataTable,
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableCell,
  TableContainer,
  TableExpandHeader,
  TableExpandRow,
  Pagination,
  Button,
  Grid,
  Column,
  DataTableSkeleton,
  Tabs,
  TabList,
  Tab,
  TabPanels,
  TabPanel,
} from "@carbon/react";
import { Deploy } from "@carbon/icons-react";
import styles from "./DigitalAssistants.module.scss";
import type { DigitalAssistantRow } from "./types";
import {
  ACTION_TYPES,
  DEFAULT_VISIBLE_COLUMNS,
  HEADERS,
  INITIAL_STATE,
  appReducer,
} from "./types";
import { CELL_RENDERERS, StatusCell } from "./CellRenderers";
import type { Dispatch } from "react";
import type { AppAction } from "./types";
import type { SharedTableAction } from "@/components/Table/types";
import { DeployFlow } from "@/components/DeployFlow/DigitalAssistant";
import {
  fetchApplications,
  deleteApplication,
  transformApplicationToRow,
} from "@/api/applications.api";
import { AboutTab } from "./components/AboutTab";
import DeploymentDetails from "@/components/DeploymentDetails";
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

// Generic cell renderer wrapper
interface RenderCellProps {
  header: string;
  value: unknown;
  rowId: string;
  dispatch: Dispatch<AppAction | SharedTableAction>;
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
  const { deployOptions: deployOptionsData } = useDeployOptions(true);
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
  const pageTitle = selectedArchitecture?.name || "Digital Assistants";
  const pageSubtitle =
    selectedArchitecture?.description ||
    "Production-ready tools that help users complete tasks and access information through conversation or commands. Assistants integrate multiple services for complex use cases and support retrieval-augmented generation (RAG).";

  // Refs mirror state.page/pageSize and are updated inline on every render

  const pageRef = useRef(INITIAL_STATE.page);
  const pageSizeRef = useRef(INITIAL_STATE.pageSize);
  pageRef.current = state.page;
  pageSizeRef.current = state.pageSize;

  const loadApplications = useCallback(
    async (page = pageRef.current, pageSize = pageSizeRef.current) => {
      if (!catalogId) {
        return;
      }

      dispatch({ type: "SHARED_SET_LOADING", payload: true });
      dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: null });

      try {
        const response = await fetchApplications({
          page,
          page_size: pageSize,
          catalog_id: catalogId,
        });

        const rows = response.data.map(transformApplicationToRow);

        // If the current page is beyond total_pages (e.g. last item on page N
        // was deleted), correct the page ref + state and immediately re-fetch
        // the last valid page so the table doesn't show stale rows.
        const totalPages = response.pagination?.total_pages ?? 1;
        if (page > totalPages && totalPages >= 1) {
          pageRef.current = totalPages;
          dispatch({ type: "SHARED_SET_PAGE", payload: totalPages });
          void loadApplications(totalPages, pageSize);
          return;
        }

        dispatch({
          type: ACTION_TYPES.FETCH_APPLICATIONS_SUCCESS,
          payload: {
            rows,
            pagination: response.pagination,
          },
        } as AppAction);
      } catch (error) {
        const errorMessage =
          error instanceof Error
            ? error.message
            : "Failed to load applications";
        dispatch({ type: "SHARED_SET_LOADING", payload: false });
        dispatch({ type: "SHARED_SET_FETCH_ERROR", payload: errorMessage });
      }
    },
    [catalogId],
  );

  // Mount fetch + 2-minute auto-refresh interval (shared hook).
  // hasTransitionalRow activates the additional 5-second poll whenever any
  // row is in a transitional state (Deploying / Deleting / Downloading).
  const hasTransitionalRow = state.rowsData.some(
    (row) =>
      row.status === "Deploying" ||
      row.status === "Deleting" ||
      row.status === "Downloading",
  );

  useAutoRefresh({
    fetchFn: loadApplications,
    hasData: state.rowsData.length > 0,
    isPaused: state.isDeleteDialogOpen || state.isDeleting,
    hasTransitionalRow,
  });

  // Auto-dismiss success export toast after 5 seconds
  useExportToastAutoDismiss({
    exportToastOpen: state.exportToastOpen,
    exportToastKind: state.exportToastKind,
    onDismiss: () => dispatch({ type: "SHARED_HIDE_EXPORT_TOAST" }),
  });

  const handleDeploySubmit = () => {
    loadApplications();
  };

  const handleDelete = async () => {
    if (!state.selectedRowId) {
      dispatch({
        type: "SHARED_SHOW_ERROR",
        payload: { message: "No digital assistant selected for deletion" },
      });
      return;
    }

    dispatch({ type: "SHARED_SET_DELETING", payload: true });

    try {
      await deleteApplication(state.selectedRowId);
      dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" });

      await loadApplications();
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : "Failed deleting digital assistant";
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
    searchFields: [
      "name",
      "status",
      "uptime",
      "workerResource",
      "workerType",
      "messages",
    ],
    visibleColumns: state.visibleColumns,
    headers: HEADERS,
    fetchAllRows: async () => {
      let currentPage = 1;
      let hasNext = true;
      const allData: import("@/types/api.types").Application[] = [];

      while (hasNext) {
        const response = await fetchApplications({
          page: currentPage,
          page_size: 100,
          catalog_id: catalogId,
        });
        allData.push(...response.data);
        hasNext = response.pagination?.has_next ?? false;
        currentPage++;
      }

      return allData.map(transformApplicationToRow) as unknown as Record<
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
    ["name", "status", "uptime", "workerResource", "workerType", "messages"],
  ) as unknown as DigitalAssistantRow[];

  const noApplications =
    state.rowsData.length === 0 && !state.isLoading && !state.fetchError;
  const noSearchResults =
    state.rowsData.length > 0 && filteredRows.length === 0 && !state.fetchError;

  // Visible headers for the DataTable (shared utility)
  const visibleHeaders = getVisibleHeaders(HEADERS, state.visibleColumns);

  // Show DeploymentDetails if a deployment is selected
  if (state.showDeploymentDetails && state.selectedDeployment) {
    return (
      <DeploymentDetails
        deployment={state.selectedDeployment}
        onBack={() => {
          dispatch({ type: ACTION_TYPES.HIDE_DEPLOYMENT_DETAILS } as AppAction);
          loadApplications();
        }}
        deploymentSource="Digital assistants"
        onNameUpdate={(newName) =>
          dispatch({
            type: ACTION_TYPES.UPDATE_DEPLOYMENT_NAME,
            payload: newName,
          } as AppAction)
        }
      />
    );
  }

  return (
    <>
      <TableToasts
        // Delete error toast
        toastOpen={state.toastOpen}
        deleteErrorRowName={state.deleteErrorRowName}
        deleteErrorMessage={state.deleteErrorMessage}
        entityLabel="digital assistant"
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
                  {state.isLoading ? (
                    <DataTableSkeleton
                      headers={HEADERS}
                      rowCount={state.pageSize}
                      columnCount={HEADERS.length}
                    />
                  ) : (
                    <DataTable
                      rows={filteredRows}
                      headers={visibleHeaders}
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
                              onRefresh={() => loadApplications()}
                              onExport={() =>
                                dispatch({
                                  type: "SHARED_OPEN_EXPORT_DIALOG",
                                })
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
                                renderIcon={Deploy}
                                onClick={() =>
                                  dispatch({
                                    type: ACTION_TYPES.OPEN_DEPLOY_FLOW,
                                  } as AppAction)
                                }
                              >
                                Deploy
                              </Button>
                            </TableToolbarActions>

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
                              {!state.fetchError &&
                                !noApplications &&
                                !noSearchResults && (
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
                                        <Fragment key={rowKey}>
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
                                                  {state.visibleColumns
                                                    .status && (
                                                    <TableCell>
                                                      <StatusCell
                                                        value={child.status}
                                                        rowId={child.id}
                                                      />
                                                    </TableCell>
                                                  )}
                                                  {state.visibleColumns
                                                    .uptime && <TableCell />}
                                                  {state.visibleColumns
                                                    .workerResource && (
                                                    <TableCell />
                                                  )}
                                                  {state.visibleColumns
                                                    .workerType && (
                                                    <TableCell />
                                                  )}
                                                  {state.visibleColumns
                                                    .messages && <TableCell />}
                                                  <TableCell />
                                                </TableRow>
                                              ),
                                            )}
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
                              entityName="digital assistant"
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
                                  void loadApplications(page, pageSize);
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
                      state.rowsData.find(
                        (r: DigitalAssistantRow) =>
                          r.id === state.selectedRowId,
                      )?.name ?? ""
                    }
                    modalLabel="Delete digital assistant deployment"
                    confirmLegend="Confirm digital assistant deployment to be deleted"
                    warningText="Deleting a digital assistant deployment permanently deletes all associated components, including connected services, runtime metadata, and configurations will be permanently deleted, and it cannot be undone."
                    onConfirm={() => handleDelete()}
                    onClose={() =>
                      dispatch({ type: "SHARED_CLOSE_DELETE_DIALOG" })
                    }
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
                    onClose={() =>
                      dispatch({ type: "SHARED_CLOSE_EXPORT_DIALOG" })
                    }
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
          </TabPanel>
          <TabPanel>
            <AboutTab
              onDeployClick={() =>
                dispatch({ type: ACTION_TYPES.OPEN_DEPLOY_FLOW } as AppAction)
              }
            />
          </TabPanel>
        </TabPanels>
      </Tabs>
      <DeployFlow
        open={state.isDeployFlowOpen}
        onClose={() =>
          dispatch({ type: ACTION_TYPES.CLOSE_DEPLOY_FLOW } as AppAction)
        }
        onSubmit={handleDeploySubmit}
      />
    </>
  );
};

export default DigitalAssistantsPage;
