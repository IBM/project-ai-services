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
  OverflowMenu,
  OverflowMenuItem,
  MenuButton,
  MenuItem,
} from "@carbon/react";
import {
  Export,
  Filter,
  Column as ColumnIcon,
  ArrowRight,
} from "@carbon/icons-react";
import styles from "./AiDeployments.module.scss";
import type { AiDeploymentRow } from "./types";
import { ACTION_TYPES, HEADERS, INITIAL_STATE, appReducer } from "./types";

const AiDeploymentsPage = () => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

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
      const exportableHeaders = HEADERS.filter((h) => h.key !== "actions");
      const csvHeaders = exportableHeaders.map((h) => h.header);

      const escapeCSV = (value: unknown) =>
        `"${String(value ?? "").replace(/"/g, '""')}"`;

      const csvRows = filteredRows.map((row) =>
        exportableHeaders.map((h) =>
          escapeCSV(row[h.key as keyof AiDeploymentRow]),
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
    [row.name, row.status, row.uptime, row.type, row.messages]
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
          className={styles.customToast}
        />
      )}
      <PageHeader
        title={{ text: "AI Deployments" }}
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
            <DataTable rows={paginatedRows} headers={HEADERS} size="lg">
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
                          renderIcon={Filter}
                          iconDescription="Filter"
                          size="lg"
                        />
                        <Button
                          hasIconOnly
                          kind="ghost"
                          renderIcon={Export}
                          iconDescription="Export"
                          size="lg"
                          onClick={() =>
                            dispatch({ type: ACTION_TYPES.OPEN_EXPORT_DIALOG })
                          }
                        />
                        <Button
                          hasIconOnly
                          kind="ghost"
                          renderIcon={ColumnIcon}
                          iconDescription="Edit columns"
                          size="lg"
                        />
                        <div className={styles.deployButtonWrapper}>
                          <MenuButton
                            label="Deploy"
                            kind="primary"
                            size="lg"
                            menuAlignment="bottom-end"
                          >
                            <MenuItem
                              label="Architecture"
                              onClick={() => {
                                console.log("Deploy Architecture");
                              }}
                            />
                            <MenuItem
                              label="Service"
                              onClick={() => {
                                console.log("Deploy Service");
                              }}
                            />
                          </MenuButton>
                        </div>
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
                                        <OverflowMenu
                                          size="sm"
                                          flipped
                                          aria-label="Actions"
                                        >
                                          <OverflowMenuItem
                                            itemText="Delete"
                                            onClick={() => {
                                              dispatch({
                                                type: ACTION_TYPES.OPEN_DELETE_DIALOG,
                                                payload: row.id as string,
                                              });
                                            }}
                                          />
                                        </OverflowMenu>
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
                              (r: AiDeploymentRow) =>
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

export default AiDeploymentsPage;
