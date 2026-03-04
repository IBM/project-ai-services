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
import type { ApplicationRow } from "./types";

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
    name: "Content goes here and can wrap to multiple lines if needed or be truncated with an ellipsis if it exceeds the maximum length allowed",
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

interface AppState {
  search: string;
  page: number;
  pageSize: number;
  isDeleteDialogOpen: boolean;
  isConfirmed: boolean;
  rowsData: ApplicationRow[];
  selectedRowId: string | null;
  toastOpen: boolean;
  errorMessage: string;
  errorRowName: string;
  isDeleting: boolean;
}

type AppAction =
  | { type: "SET_SEARCH"; payload: string }
  | { type: "SET_PAGE"; payload: number }
  | { type: "SET_PAGE_SIZE"; payload: number }
  | { type: "OPEN_DELETE_DIALOG"; payload: string }
  | { type: "CLOSE_DELETE_DIALOG" }
  | { type: "SET_CONFIRMED"; payload: boolean }
  | { type: "DELETE_ROW"; payload: string }
  | { type: "SHOW_ERROR"; payload: { message: string; rowName?: string } }
  | { type: "HIDE_ERROR" }
  | { type: "SET_IS_DELETING"; payload: boolean };

const initialState: AppState = {
  search: "",
  page: 1,
  pageSize: 10,
  isDeleteDialogOpen: false,
  isConfirmed: false,
  rowsData: rows,
  selectedRowId: null,
  toastOpen: false,
  errorMessage: "",
  errorRowName: "",
  isDeleting: false,
};

const appReducer = (state: AppState, action: AppAction): AppState => {
  switch (action.type) {
    case "SET_SEARCH":
      return { ...state, search: action.payload };
    case "SET_PAGE":
      return { ...state, page: action.payload };
    case "SET_PAGE_SIZE":
      return { ...state, pageSize: action.payload };
    case "OPEN_DELETE_DIALOG":
      return {
        ...state,
        selectedRowId: action.payload,
        isDeleteDialogOpen: true,
        toastOpen: false,
      };
    case "CLOSE_DELETE_DIALOG":
      return {
        ...state,
        isDeleteDialogOpen: false,
        isConfirmed: false,
        selectedRowId: null,
      };
    case "SET_CONFIRMED":
      return { ...state, isConfirmed: action.payload };
    case "DELETE_ROW":
      return {
        ...state,
        rowsData: state.rowsData.filter((r) => r.id !== action.payload),
        isDeleteDialogOpen: false,
        isConfirmed: false,
      };
    case "SHOW_ERROR":
      return {
        ...state,
        errorMessage: action.payload.message,
        errorRowName: action.payload.rowName ?? "",
        toastOpen: true,
        isDeleting: false,
      };
    case "HIDE_ERROR":
      return {
        ...state,
        toastOpen: false,
        selectedRowId: null,
        errorRowName: "",
      };
    case "SET_IS_DELETING":
      return { ...state, isDeleting: action.payload };
    default:
      return state;
  }
};

const ApplicationsListPage = () => {
  const [state, dispatch] = useReducer(appReducer, initialState);

  const handleDelete = async () => {
    if (!state.selectedRowId) {
      dispatch({
        type: "SHOW_ERROR",
        payload: { message: "No application selected for deletion" },
      });
      return;
    }

    dispatch({ type: "SET_IS_DELETING", payload: true });

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
      dispatch({ type: "DELETE_ROW", payload: state.selectedRowId });
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : "Failed deleting application";
      const name =
        state.rowsData.find((r) => r.id === state.selectedRowId)?.name ?? "";
      dispatch({
        type: "SHOW_ERROR",
        payload: { message: msg, rowName: name },
      });
    } finally {
      dispatch({ type: "SET_IS_DELETING", payload: false });
      dispatch({ type: "CLOSE_DELETE_DIALOG" }); // still ok; the name is preserved
    }
  };
  const filteredRows = rows.filter((row) =>
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

  const noApplications = rows.length === 0;
  const noSearchResults = rows.length > 0 && filteredRows.length === 0;

  return (
    <>
      {state.toastOpen && (
        <ActionableNotification
          actionButtonLabel="Try again"
          aria-label="close notification"
          kind="error"
          closeOnEscape
          title={`Delete technical template ${state.errorRowName} failed`}
          subtitle={state.errorMessage}
          onCloseButtonClick={() => {
            dispatch({ type: "HIDE_ERROR" });
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

      <Grid fullWidth>
        <Column lg={16} md={8} sm={4}>
          <div className={styles.tableContent}>
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
                              type: "SET_SEARCH",
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
                    <Table {...getTableProps()}>
                      <TableHead>
                        <TableRow>
                          {headers.map((header) => {
                            const { key, ...rest } = getHeaderProps({ header });

                            return (
                              <TableHeader key={key} {...rest}>
                                {header.header}
                              </TableHeader>
                            );
                          })}
                        </TableRow>
                      </TableHead>

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
                                                type: "OPEN_DELETE_DIALOG",
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
                      )}
                    </Table>
                  </TableContainer>

                  {filteredRows.length > 20 && (
                    <Pagination
                      page={state.page}
                      pageSize={state.pageSize}
                      pageSizes={[5, 10, 20, 30]}
                      totalItems={filteredRows.length}
                      onChange={({ page, pageSize }) => {
                        dispatch({ type: "SET_PAGE", payload: page });
                        dispatch({ type: "SET_PAGE_SIZE", payload: pageSize });
                      }}
                    />
                  )}
                </>
              )}
            </DataTable>
          </div>
          <Modal
            open={state.isDeleteDialogOpen}
            size="xs"
            modalLabel="Delete Case routing"
            modalHeading="Confirm delete"
            primaryButtonText="Delete"
            secondaryButtonText="Cancel"
            danger
            primaryButtonDisabled={!state.isConfirmed}
            onRequestClose={() => {
              dispatch({ type: "CLOSE_DELETE_DIALOG" });
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
                            (r: ApplicationRow) => r.id === state.selectedRowId,
                          )?.name
                        : ""}
                    </strong>
                  }
                  checked={state.isConfirmed}
                  onChange={(_, { checked }) =>
                    dispatch({ type: "SET_CONFIRMED", payload: checked })
                  }
                />
              </CheckboxGroup>
            </div>
          </Modal>
        </Column>
      </Grid>
    </>
  );
};

export default ApplicationsListPage;
