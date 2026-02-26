import { useState } from "react";
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
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Pagination,
  Button,
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
const ApplicationsListPage = () => {
  const [search, setSearch] = useState<string>("");
  const [page, setPage] = useState<number>(1);
  const [pageSize, setPageSize] = useState<number>(10);

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
      .includes(search.toLowerCase()),
  );

  const paginatedRows = filteredRows.slice(
    (page - 1) * pageSize,
    page * pageSize,
  );

  return (
    <>
      <PageHeader
        title={{ text: "Applications" }}
        pageActions={[
          {
            key: "learn-more",
            kind: "tertiary",
            label: "Learn more",
            renderIcon: ArrowRight,
            onClick: () => {},
          },
        ]}
        pageActionsOverflowLabel="More actions"
        fullWidthGrid="xl"
      />

      <div className={styles.applicationTable}>
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
                    value={search}
                    onChange={(e) => {
                      if (typeof e !== "string") {
                        setSearch(e.target.value);
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

                  <TableBody>
                    {rows.map((row) => {
                      const { key: rowKey, ...rowProps } = getRowProps({ row });

                      return (
                        <TableRow key={rowKey} {...rowProps}>
                          {row.cells.map((cell) => {
                            const { key: cellKey, ...cellProps } = getCellProps(
                              { cell },
                            );

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
              </TableContainer>

              <Pagination
                page={page}
                pageSize={pageSize}
                pageSizes={[5, 10, 20, 30]}
                totalItems={filteredRows.length}
                onChange={({ page, pageSize }) => {
                  setPage(page);
                  setPageSize(pageSize);
                }}
              />
            </>
          )}
        </DataTable>
      </div>
    </>
  );
};

export default ApplicationsListPage;
