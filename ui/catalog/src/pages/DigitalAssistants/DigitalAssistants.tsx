import React, { useReducer, useState } from "react";
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
  InlineLoading,
  OverflowMenu,
  Tabs,
  TabList,
  Tab,
  TabPanels,
  TabPanel,
} from "@carbon/react";
import { Export, Column as ColumnIcon, Deploy } from "@carbon/icons-react";
import styles from "./DigitalAssistants.module.scss";
import type { DigitalAssistantRow } from "./types";
import { ACTION_TYPES, HEADERS, INITIAL_STATE, appReducer } from "./types";
import { renderCell, StatusCell } from "./CellRenderers";
import { exportToCSV, ensureCSVExtension } from "@/utils/csv";

const DigitalAssistantsPage = () => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);
  const [selectedTabIndex, setSelectedTabIndex] = useState(0);

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

    const filename = ensureCSVExtension(name);

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
      exportToCSV(filteredRows, exportableHeaders, filename);

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

  const filteredRows = state.rowsData.filter((row) => {
    const matchesSearch = [row.name, row.status, row.uptime, row.messages]
      .join(" ")
      .toLowerCase()
      .includes(state.search.toLowerCase());

    return matchesSearch;
  });

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
      <Tabs
        selectedIndex={selectedTabIndex}
        onChange={(evt) => setSelectedTabIndex(evt.selectedIndex)}
      >
        <PageHeader
          title={{ text: "Digital assistants" }}
          subtitle="Production-ready AI solutions that combine multiple services into intelligent, integrated systems for complex use cases using Retrieval-Augmented Generation (RAG)."
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
                                onClick={() => {
                                  console.log("Deploy clicked");
                                }}
                              >
                                Deploy
                              </Button>
                            </TableToolbarContent>
                          </TableToolbar>

                          {noApplications ? (
                            <NoDataEmptyState
                              title="Start by adding a digital assistant"
                              subtitle="To deploy a digital assistant using a template, click Deploy."
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
                              <TableBody>
                                {rows.map((row) => {
                                  const { key: rowKey, ...rowProps } =
                                    getRowProps({
                                      row,
                                    });
                                  const originalRow = paginatedRows.find(
                                    (r) => r.id === row.id,
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
                                          const { key: cellKey, ...cellProps } =
                                            getCellProps({ cell });

                                          return renderCell({
                                            header: cell.info.header,
                                            value: cell.value,
                                            rowId: row.id as string,
                                            dispatch,
                                            selectedRowId: state.selectedRowId,
                                            cellKey,
                                            cellProps,
                                          });
                                        })}
                                      </TableExpandRow>
                                      {hasChildren &&
                                        row.isExpanded &&
                                        originalRow.children?.map((child) => (
                                          <TableRow key={child.id}>
                                            <TableCell />
                                            <TableCell>{child.name}</TableCell>
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
                                        ))}
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
                    modalLabel={`Delete ${state.rowsData.find((r) => r.id === state.selectedRowId)?.name || "digital assistant"}`}
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
                      Deleting an AI deployment permanently deletes all
                      associated components, including connected services,
                      runtime metadata, and configurations will be permanently
                      deleted, and it cannot be undone.
                    </p>
                    <div>
                      <CheckboxGroup
                        className={styles.deleteConfirmation}
                        legendText="Confirm digital assistant to be deleted"
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
                        <InlineLoading
                          status="active"
                          description="Exporting..."
                        />
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
          </TabPanel>
          <TabPanel>
            <div className={styles.tableContent}>
              <Grid fullWidth>
                <Column lg={16} md={8} sm={4} className={styles.tableColumn}>
                  <h4>About Digital Assistants</h4>
                  <p>
                    Digital assistants are production-ready AI solutions that
                    combine multiple services into intelligent, integrated
                    systems for complex use cases using Retrieval-Augmented
                    Generation (RAG).
                  </p>
                </Column>
              </Grid>
            </div>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </>
  );
};

export default DigitalAssistantsPage;
