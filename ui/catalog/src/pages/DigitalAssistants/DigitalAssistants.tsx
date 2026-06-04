import React, { useReducer, useEffect } from "react";
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
  Layer,
  Link,
} from "@carbon/react";
import {
  Export,
  Column as ColumnIcon,
  Deploy,
  Code,
  PlayOutline,
} from "@carbon/icons-react";
import styles from "./DigitalAssistants.module.scss";
import type { DigitalAssistantRow } from "./types";
import { ACTION_TYPES, HEADERS, INITIAL_STATE, appReducer } from "./types";
import { CELL_RENDERERS, StatusCell } from "./CellRenderers";
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

const DigitalAssistantsPage = () => {
  const [state, dispatch] = useReducer(appReducer, INITIAL_STATE);

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
          title={{ text: "Digital assistants" }}
          subtitle="Production-ready tools that help users complete tasks and access information through conversation or commands. Assistants integrate multiple services for complex use cases and support retrieval-augmented generation (RAG)."
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
                    modalLabel="Delete digital assistant deployment"
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
                      Deleting an digital assistant deployment permanently
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
            <div className={styles.aboutContent}>
              {/* Services Section */}
              <Layer withBackground>
                <section className={styles.aboutSection}>
                  <div className={styles.sectionHeader}>
                    <h4 className={styles.aboutSectionTitle}>Services</h4>
                    <Button
                      kind="primary"
                      size="md"
                      renderIcon={Deploy}
                      onClick={() => {
                        console.log("Deploy clicked");
                      }}
                    >
                      Deploy
                    </Button>
                  </div>
                  <ul className={styles.servicesList}>
                    <li>Digitize documents</li>
                    <li>Find similar items</li>
                    <li>Question and answer</li>
                    <li>Summarize</li>
                  </ul>
                </section>
              </Layer>

              {/* Use Case Domains Section */}
              <Layer withBackground>
                <section className={styles.aboutSection}>
                  <h4 className={styles.aboutSectionTitle}>Use case domains</h4>
                  <Grid narrow className={styles.gridWithTopMargin}>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>Agriculture</h5>
                      <ul className={styles.useCaseList}>
                        <li>Agriculture assistant</li>
                      </ul>
                    </Column>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>Banking</h5>
                      <ul className={styles.useCaseList}>
                        <li>Analyst assistant</li>
                        <li>Financial documents assistant</li>
                        <li>Open account agent</li>
                      </ul>
                    </Column>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>
                        Enterprise resource planning
                      </h5>
                      <ul className={styles.useCaseList}>
                        <li>BI assistant</li>
                        <li>Invoice matching assistant</li>
                        <li>Order processing assistant</li>
                      </ul>
                    </Column>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>Insurance</h5>
                      <ul className={styles.useCaseList}>
                        <li>Claims & policy management agent</li>
                      </ul>
                    </Column>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>IT operations</h5>
                      <ul className={styles.useCaseList}>
                        <li>Invoice matching assistant</li>
                      </ul>
                    </Column>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>Public sector</h5>
                      <ul className={styles.useCaseList}>
                        <li>Private documents assistant</li>
                        <li>Product sales assistant</li>
                      </ul>
                    </Column>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>
                        Professional services
                      </h5>
                      <ul className={styles.useCaseList}>
                        <li>Conference slide search</li>
                      </ul>
                    </Column>
                    <Column sm={4} md={4} lg={4}>
                      <h5 className={styles.useCaseDomain}>Real estate</h5>
                      <ul className={styles.useCaseList}>
                        <li>Real estate assistant</li>
                      </ul>
                    </Column>
                  </Grid>
                </section>
              </Layer>

              {/* Minimum Resource Allocation Section */}
              <Layer withBackground>
                <section className={styles.aboutSection}>
                  <h4 className={styles.aboutSectionTitle}>
                    Minimum resource allocation
                  </h4>
                  <Grid narrow className={styles.gridWithTopMargin}>
                    <Column sm={4} md={4} lg={5}>
                      <div className={styles.resourceItem}>
                        <span className={styles.resourceLabel}>
                          Required cores
                        </span>
                        <span className={styles.resourceValue}>0.5 - 2.0</span>
                      </div>
                    </Column>
                    <Column sm={4} md={4} lg={5}>
                      <div className={styles.resourceItem}>
                        <span className={styles.resourceLabel}>
                          Required memory
                        </span>
                        <span className={styles.resourceValue}>
                          15GB - 25GB
                        </span>
                      </div>
                    </Column>
                    <Column sm={4} md={4} lg={6}>
                      <div className={styles.resourceItem}>
                        <span className={styles.resourceLabel}>
                          Required Spyre cards
                        </span>
                        <span className={styles.resourceValue}>4 cards</span>
                      </div>
                    </Column>
                  </Grid>
                </section>
              </Layer>

              {/* Code and Architecture + Demos Section (Side by Side) */}
              <div className={styles.sideBySideGrid}>
                {/* Code and Architecture Section */}
                <Layer withBackground className={styles.sideBySideColumn}>
                  <section className={styles.sideBySideSection}>
                    <h4 className={styles.aboutSectionTitle}>
                      Code and architecture
                    </h4>
                    <Button
                      kind="tertiary"
                      size="sm"
                      className={styles.codeButton}
                      renderIcon={Code}
                      onClick={() =>
                        window.open(
                          "https://github.com/IBM/project-ai-services/tree/main/services/chatbot",
                          "_blank",
                        )
                      }
                    >
                      View code
                    </Button>
                    <div className={styles.architectureDiagram}>
                      <img
                        src="https://www.ibm.com/docs/en/SSXZBY_2026.03.0/IBM-AI-services/ai-services-assets/rag-arch-v020.png"
                        alt="RAG Architecture Diagram"
                        className={styles.diagramImage}
                      />
                    </div>
                  </section>
                </Layer>

                {/* Demos and Prototypes Section */}
                {/* TODO: This needs to be updated, awaiting for response from the design team */}
                <Layer withBackground className={styles.sideBySideColumn}>
                  <section className={styles.demosSection}>
                    <h4 className={styles.aboutSectionTitle}>
                      Demos and prototypes
                    </h4>
                    <div className={styles.demoCard}>
                      <img src="" alt="RAG Demo" className={styles.demoImage} />
                      <div className={styles.demoContent}>
                        <h5 className={styles.demoTitle}>
                          Retrieval-Augmented Generation (RAG)
                        </h5>
                        <p className={styles.demoDescription}>
                          Discover the architecture behind this pre-built
                          digital assistant
                        </p>
                        <div className={styles.demoActions}>
                          <Link
                            href="https://github.com/IBM/project-ai-services/tree/main/spyre-rag"
                            target="_blank"
                            renderIcon={PlayOutline}
                          >
                            Watch
                          </Link>
                        </div>
                      </div>
                    </div>
                  </section>
                </Layer>
              </div>
            </div>
          </TabPanel>
        </TabPanels>
      </Tabs>
    </>
  );
};

export default DigitalAssistantsPage;
