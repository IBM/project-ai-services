import { useReducer } from "react";
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
  Pagination,
  Button,
  Grid,
  Column,
  Checkbox,
  CheckboxGroup,
  ActionableNotification,
  Modal,
  TextInput,
  InlineLoading,
  type DataTableHeader,
} from "@carbon/react";
import {
  Add,
  Download,
  Renew,
  Settings,
  ArrowUpRight,
  TrashCan,
  ArrowRight,
  CopyLink,
} from "@carbon/icons-react";
import styles from "./ApplicationsList.module.scss";
import type { ApplicationRow, AppState, AppAction } from "./types";
import { ACTION_TYPES } from "./types";

const headers: DataTableHeader[] = [
  { header: "Name", key: "name" },
  { header: "Template", key: "template" },
  { header: "Processors", key: "processors" },
  { header: "Memory", key: "memory" },
  { header: "Cards", key: "cards" },
  { header: "Storage", key: "storage" },
  { header: "", key: "actions" },
];

const rows: ApplicationRow[] = [
  {
    id: "1",
    name: "Incident troubleshooting",
    template: "Digital Assistant",
    processors: 1,
    memory: "3GB",
    cards: 4,
    storage: "180GB",
    actions: "actions",
  },
  {
    id: "2",
    name: "Customer onboarding bot",
    template: "Workflow Assistant",
    processors: 2,
    memory: "8GB",
    cards: 6,
    storage: "250GB",
    actions: "actions",
  },
  {
    id: "3",
    name: "Claims processing engine",
    template: "Automation Studio",
    processors: 4,
    memory: "16GB",
    cards: 8,
    storage: "500GB",
    actions: "actions",
  },
  {
    id: "4",
    name: "Knowledge base search",
    template: "Search Service",
    processors: 1,
    memory: "4GB",
    cards: 3,
    storage: "120GB",
    actions: "actions",
  },
  {
    id: "5",
    name: "Predictive analytics model",
    template: "ML Runtime",
    processors: 8,
    memory: "32GB",
    cards: 10,
    storage: "1TB",
    actions: "actions",
  },
  {
    id: "6",
    name: "Security monitoring",
    template: "Threat Detection AI",
    processors: 8,
    memory: "16GB",
    cards: 10,
    storage: "1TB",
    actions: "actions",
  },
];

const initialState: AppState = {
  search: "",
  page: 1,
  pageSize: 10,
  isDeleteDialogOpen: false,
  isConfirmed: false,
  rowsData: rows,
  selectedRowId: null,
  toastOpen: false,
  deleteErrorMessage: "",
  deleteErrorRowName: "",
  isDeleting: false,
  hasError: false,
  isExportDialogOpen: false,
  csvFileName: "",
  exportStatus: "idle",
  exportErrorMessage: "",
};

const appReducer = (state: AppState, action: AppAction): AppState => {
  switch (action.type) {
    case ACTION_TYPES.SET_SEARCH:
      return { ...state, search: action.payload };

    case ACTION_TYPES.SET_PAGE:
      return { ...state, page: action.payload };

    case ACTION_TYPES.SET_PAGE_SIZE:
      return { ...state, pageSize: action.payload };

    case ACTION_TYPES.OPEN_DELETE_DIALOG:
      return {
        ...state,
        selectedRowId: action.payload,
        isDeleteDialogOpen: true,
        toastOpen: false,
      };

    case ACTION_TYPES.CLOSE_DELETE_DIALOG:
      return {
        ...state,
        isDeleteDialogOpen: false,
        isConfirmed: false,
        selectedRowId: state.hasError ? state.selectedRowId : null,
      };

    case ACTION_TYPES.SET_CONFIRMED:
      return { ...state, isConfirmed: action.payload };

    case ACTION_TYPES.DELETE_ROW:
      return {
        ...state,
        rowsData: state.rowsData.filter((r) => r.id !== action.payload),
        isDeleteDialogOpen: false,
        isConfirmed: false,
      };

    case ACTION_TYPES.SHOW_ERROR:
      return {
        ...state,
        deleteErrorMessage: action.payload.message,
        deleteErrorRowName: action.payload.rowName ?? "",
        toastOpen: true,
        isDeleting: false,
        hasError: true,
      };

    case ACTION_TYPES.HIDE_ERROR:
      return {
        ...state,
        toastOpen: false,
        selectedRowId: null,
        hasError: false,
        deleteErrorRowName: "",
      };

    case ACTION_TYPES.SET_IS_DELETING:
      return { ...state, isDeleting: action.payload };

    case ACTION_TYPES.SET_SELECTED_ROW_ID:
      return { ...state, selectedRowId: action.payload };

    case ACTION_TYPES.OPEN_EXPORT_DIALOG:
      return {
        ...state,
        isExportDialogOpen: true,
        csvFileName: "",
        exportErrorMessage: "",
        exportStatus: "idle",
      };

    case ACTION_TYPES.CLOSE_EXPORT_DIALOG:
      return {
        ...state,
        isExportDialogOpen: false,
      };

    case ACTION_TYPES.SET_CSV_FILENAME:
      return { ...state, csvFileName: action.payload };

    case ACTION_TYPES.SET_EXPORT_STATUS:
      return { ...state, exportStatus: action.payload };

    case ACTION_TYPES.SET_EXPORT_ERROR:
      return {
        ...state,
        exportErrorMessage: action.payload,
      };

    case ACTION_TYPES.CLEAR_EXPORT_ERROR:
      return { ...state, exportErrorMessage: "" };

    default:
      return state;
  }
};

const ApplicationsListPage = () => {
  const [state, dispatch] = useReducer(appReducer, initialState);

  const handleDelete = async () => {
    if (!state.selectedRowId) {
      dispatch({
        type: ACTION_TYPES.SHOW_ERROR,
        payload: { message: "No application selected for deletion" },
      });
      return;
    }

    dispatch({ type: ACTION_TYPES.SET_IS_DELETING, payload: true });

    try {
      // Attempt server-side delete; if no backend exists this may fail.
      const res = await fetch(`/api/applications/${state.selectedRowId}`, {
        method: "DELETE",
      });

      if (!res.ok) {
        const text = await res
          .text()
          .catch(() => res.statusText || "Delete failed");
        throw new Error(text || `Delete failed (${res.status})`);
      }
      dispatch({ type: ACTION_TYPES.DELETE_ROW, payload: state.selectedRowId });
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : "Failed deleting application";
      const name =
        state.rowsData.find((r) => r.id === state.selectedRowId)?.name ?? "";
      dispatch({
        type: ACTION_TYPES.SHOW_ERROR,
        payload: { message: msg, rowName: name },
      });
    } finally {
      dispatch({ type: ACTION_TYPES.SET_IS_DELETING, payload: false });
      dispatch({ type: ACTION_TYPES.CLOSE_DELETE_DIALOG }); // still ok; the name is preserved
    }
  };

  const downloadCSV = async () => {
    const name = state.csvFileName.trim();

    if (!name) {
      dispatch({
        type: ACTION_TYPES.SET_EXPORT_ERROR,
        payload: "Provide a valid file name",
      });
      return;
    }

    const filename = `${name.replace(/\.[^/.]+$/, "")}.csv`;

    if (filteredRows.length === 0) {
      dispatch({
        type: ACTION_TYPES.SET_EXPORT_ERROR,
        payload: "No data available to export",
      });
      return;
    }

    dispatch({
      type: ACTION_TYPES.SET_EXPORT_STATUS,
      payload: "exporting",
    });

    try {
      const exportableHeaders = headers.filter((h) => h.key !== "actions");
      const csvHeaders = exportableHeaders.map((h) => h.header);

      const escapeCSV = (value: unknown) =>
        `"${String(value ?? "").replace(/"/g, '""')}"`;

      const csvRows = filteredRows.map((row) =>
        exportableHeaders.map((h) =>
          escapeCSV(row[h.key as keyof ApplicationRow]),
        ),
      );

      const csv = [csvHeaders, ...csvRows].map((r) => r.join(",")).join("\n");
      const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = filename;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      dispatch({
        type: ACTION_TYPES.SET_EXPORT_STATUS,
        payload: "success",
      });
    } catch {
      dispatch({
        type: ACTION_TYPES.SET_EXPORT_STATUS,
        payload: "error",
      });

      dispatch({
        type: ACTION_TYPES.SET_EXPORT_ERROR,
        payload:
          "An error occurred while exporting the CSV file. Please try again.",
      });
    }
  };

  const filteredRows = state.rowsData.filter((row) =>
    [
      row.name,
      row.template,
      row.memory,
      row.storage,
      String(row.processors),
      String(row.cards),
    ]
      .join(" ")
      .toLowerCase()
      .includes(state.search.toLowerCase()),
  );

  const paginatedRows = filteredRows.slice(
    (state.page - 1) * state.pageSize,
    state.page * state.pageSize,
  );

  const noApplications = state.rowsData.length === 0;
  const noSearchResults =
    state.rowsData.length > 0 && filteredRows.length === 0;

  return (
    <>
      {state.toastOpen && (
        <ActionableNotification
          actionButtonLabel="Try again"
          aria-label="close notification"
          kind="error"
          closeOnEscape
          title={`Delete technical template ${state.deleteErrorRowName} failed`}
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
          style={{
            position: "fixed",
            top: "4rem",
            right: "2rem",
            zIndex: "46567",
          }}
          className={styles.customToast}
        />
      )}
      <PageHeader
        title={{ text: "Applications" }}
        pageActions={[
          {
            key: "learn-more",
            kind: "tertiary",
            label: "Learn more",
            renderIcon: ArrowRight,
            onClick: () => {
              window.open(
                "https://www.ibm.com/docs/en/aiservices?topic=services-introduction",
                "_blank",
              );
            },
          },
        ]}
        pageActionsOverflowLabel="More actions"
        fullWidthGrid="xl"
      />

      <div className={styles.tableContent}>
        <Grid fullWidth>
          <Column lg={16} md={8} sm={4} className={styles.tableColumn}>
            <DataTable rows={paginatedRows} headers={headers} size="lg">
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
                          renderIcon={Download}
                          iconDescription="Download"
                          size="lg"
                          onClick={() =>
                            dispatch({ type: ACTION_TYPES.OPEN_EXPORT_DIALOG })
                          }
                        />
                        <Button
                          hasIconOnly
                          kind="ghost"
                          renderIcon={Renew}
                          iconDescription="Refresh"
                          size="lg"
                        />
                        <Button
                          hasIconOnly
                          kind="ghost"
                          renderIcon={Settings}
                          iconDescription="Settings"
                          size="lg"
                        />
                        <Button size="lg" kind="primary" renderIcon={Add}>
                          Deploy application
                        </Button>
                      </TableToolbarContent>
                    </TableToolbar>

                    {noApplications ? (
                      <NoDataEmptyState
                        title="Start by adding an application"
                        subtitle="To deploy an application using a template, click Deploy."
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
                              <TableRow key={rowKey} {...rowProps}>
                                {row.cells.map((cell) => {
                                  const { key: cellKey, ...cellProps } =
                                    getCellProps({ cell });

                                  if (cell.info.header === "actions") {
                                    return (
                                      <TableCell key={cellKey} {...cellProps}>
                                        <div className={styles.rowActions}>
                                          <Button
                                            kind="tertiary"
                                            size="sm"
                                            renderIcon={ArrowUpRight}
                                          >
                                            Open
                                          </Button>
                                          <Button
                                            hasIconOnly
                                            kind="tertiary"
                                            size="sm"
                                            renderIcon={CopyLink}
                                            iconDescription="Copy"
                                          />
                                          <Button
                                            hasIconOnly
                                            kind="ghost"
                                            size="sm"
                                            renderIcon={TrashCan}
                                            iconDescription="Delete"
                                            className={`${styles.deleteButton} ${
                                              state.selectedRowId === row.id
                                                ? styles.selectedDelete
                                                : ""
                                            }`}
                                            onClick={() => {
                                              dispatch({
                                                type: ACTION_TYPES.OPEN_DELETE_DIALOG,
                                                payload: row.id as string,
                                              });
                                            }}
                                          />
                                        </div>
                                      </TableCell>
                                    );
                                  }
                                  return (
                                    <TableCell key={cellKey} {...cellProps}>
                                      {cell.value}
                                    </TableCell>
                                  );
                                })}
                              </TableRow>
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

            <Modal
              open={state.isDeleteDialogOpen}
              size="sm"
              modalLabel={`Delete ${state.rowsData.find((r) => r.id === state.selectedRowId)?.name || "Application"}`}
              modalHeading="Confirm delete"
              primaryButtonText="Delete"
              secondaryButtonText="Cancel"
              danger
              primaryButtonDisabled={!state.isConfirmed}
              onRequestClose={() => {
                dispatch({ type: ACTION_TYPES.CLOSE_DELETE_DIALOG });
              }}
              onRequestSubmit={handleDelete}
            >
              <p>
                Deleting an application permanently removes all associated
                components, including connected services, runtime metadata, and
                any data or configurations created.
              </p>
              <div>
                <CheckboxGroup
                  className={styles.deleteConfirmation}
                  legendText="Confirm application to be deleted"
                >
                  <Checkbox
                    id="checkbox-label-1"
                    labelText={
                      <strong>
                        {state.selectedRowId
                          ? state.rowsData.find(
                              (r: ApplicationRow) =>
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
              passiveModal={state.exportStatus !== "idle"}
              preventCloseOnClickOutside
              {...(state.exportStatus === "idle" && {
                primaryButtonText: "Export",
                secondaryButtonText: "Cancel",
                onRequestSubmit: downloadCSV,
              })}
              onRequestClose={() =>
                dispatch({ type: ACTION_TYPES.CLOSE_EXPORT_DIALOG })
              }
            >
              {state.exportStatus === "idle" && (
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
              )}

              {state.exportStatus === "exporting" && (
                <div className={styles.exportStatus}>
                  <InlineLoading status="active" description="Exporting..." />
                </div>
              )}

              {state.exportStatus === "success" && (
                <div className={styles.exportStatus}>
                  <InlineLoading
                    status="finished"
                    description="The file has been exported"
                  />
                </div>
              )}

              {state.exportStatus === "error" && (
                <div className={styles.exportStatus}>
                  <InlineLoading
                    status="error"
                    description={state.exportErrorMessage}
                  />
                </div>
              )}
            </Modal>
          </Column>
        </Grid>
      </div>
    </>
  );
};

export default ApplicationsListPage;
