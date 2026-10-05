import React, { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router';
import {
  Breadcrumb,
  BreadcrumbItem,
  Button,
  Column,
  DataTable,
  DataTableSkeleton,
  Grid,
  Heading,
  InlineNotification,
  Pagination,
  ProgressBar,
  Tab,
  TabList,
  TabPanel,
  TabPanels,
  Tabs,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableHeader,
  TableRow,
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Tag,
} from '@carbon/react';
import { Renew, Add, DocumentView } from '@carbon/icons-react';
import { listJobs, InvoiceJob } from '../../services/api';

const STATUS_TAG_COLOR: Record<string, 'gray' | 'cool-gray' | 'warm-gray' | 'blue' | 'cyan' | 'teal' | 'purple' | 'green' | 'red' | 'magenta'> = {
  accepted: 'gray',
  routing: 'blue',
  digitizing: 'blue',
  extracting: 'cyan',
  staging: 'teal',
  review: 'purple',
  loading: 'blue',
  completed: 'green',
  rejected: 'red',
  failed: 'red',
  // support upper-case fallbacks
  ACCEPTED: 'gray',
  ROUTING: 'blue',
  DIGITIZING: 'blue',
  EXTRACTING: 'cyan',
  STAGING: 'teal',
  REVIEW: 'purple',
  PENDING_REVIEW: 'purple',
  LOADING: 'blue',
  COMPLETED: 'green',
  APPROVED: 'green',
  LOADED: 'green',
  REJECTED: 'red',
  FAILED: 'red',
};

function getStepProgress(status: string): { label: string; value: number } {
  const norm = status.toLowerCase();
  switch (norm) {
    case 'accepted':
    case 'routing':
      return { label: 'Upload (1/5)', value: 0.2 };
    case 'digitizing':
      return { label: 'OCR / Vision (2/5)', value: 0.4 };
    case 'extracting':
    case 'staging':
      return { label: 'Staging (3/5)', value: 0.6 };
    case 'review':
    case 'pending_review':
      return { label: 'Human Review (4/5)', value: 0.8 };
    case 'loading':
    case 'completed':
    case 'approved':
    case 'loaded':
      return { label: 'Completed (5/5)', value: 1.0 };
    case 'rejected':
      return { label: 'Rejected', value: 0.8 };
    case 'failed':
      return { label: 'Failed', value: 0.5 };
    default:
      return { label: norm, value: 0.1 };
  }
}

const HEADERS = [
  { key: 'filename', header: 'File Name' },
  { key: 'status', header: 'Status' },
  { key: 'pipeline_path', header: 'Path' },
  { key: 'progress', header: 'Pipeline Stage' },
  { key: 'error', header: 'Error' },
  { key: 'submitted_at', header: 'Submitted' },
  { key: 'actions', header: 'Actions' },
];

const JobsPage: React.FC = () => {
  const navigate = useNavigate();
  const [jobs, setJobs] = useState<InvoiceJob[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tabIndex, setTabIndex] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [totalItems, setTotalItems] = useState(0);

  const fetchJobs = useCallback(async (showSkeleton = false) => {
    if (showSkeleton) setLoading(true);
    try {
      const res = await listJobs({ page, page_size: pageSize });
      setJobs(res.data || []);
      setTotalItems(res.pagination?.total_items || res.data?.length || 0);
      setError(null);
    } catch (err: unknown) {
      const msg =
        (err as { response?: { data?: { detail?: string } }; message?: string })?.response?.data?.detail ||
        (err as Error)?.message ||
        'Failed to fetch jobs.';
      setError(msg);
    } finally {
      if (showSkeleton) setLoading(false);
    }
  }, [page, pageSize]);

  useEffect(() => {
    let ignore = false;

    const load = async () => {
      try {
        const res = await listJobs({ page, page_size: pageSize });
        if (!ignore) {
          setJobs(res.data || []);
          setTotalItems(res.pagination?.total_items || res.data?.length || 0);
          setError(null);
        }
      } catch (err: unknown) {
        if (!ignore) {
          const msg =
            (err as { response?: { data?: { detail?: string } }; message?: string })?.response?.data?.detail ||
            (err as Error)?.message ||
            'Failed to fetch jobs.';
          setError(msg);
        }
      } finally {
        if (!ignore) {
          setLoading(false);
        }
      }
    };

    load();
    const interval = setInterval(() => fetchJobs(false), 5000);
    return () => {
      ignore = true;
      clearInterval(interval);
    };
  }, [fetchJobs, page, pageSize]);

  const pendingReviewJobs = jobs.filter(
    (j) => j.status?.toLowerCase() === 'review' || j.status?.toLowerCase() === 'pending_review'
  );
  const completedJobs = jobs.filter(
    (j) => j.status?.toLowerCase() === 'completed' || j.status?.toLowerCase() === 'loaded'
  );

  const currentTabJobs =
    tabIndex === 1 ? pendingReviewJobs : tabIndex === 2 ? completedJobs : jobs;

  const tableRows = currentTabJobs.map((job) => ({
    id: job.job_id,
    filename: job.filename,
    status: job.status,
    pipeline_path: job.pipeline_path || 'oneshot',
    progress: job.status,
    error: job.error || '—',
    submitted_at: job.submitted_at ? new Date(job.submitted_at).toLocaleString() : '—',
    actions: job.job_id,
  }));

  return (
    <Grid className="cds--css-grid--full-width" style={{ padding: '2rem' }}>
      <Column lg={16} md={8} sm={4}>
        <Breadcrumb style={{ marginBottom: '1rem' }}>
          <BreadcrumbItem href="#" isCurrentPage>
            Pipeline Jobs
          </BreadcrumbItem>
        </Breadcrumb>

        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            marginBottom: '1.5rem',
            flexWrap: 'wrap',
            gap: '1rem',
          }}
        >
          <div>
            <Heading style={{ fontSize: '1.75rem', fontWeight: 600 }}>Invoice Processing Jobs</Heading>
            <p style={{ marginTop: '0.25rem', color: 'var(--cds-text-secondary)' }}>
              Monitor status, pipeline progress, and processing history.
            </p>
          </div>

          <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
            <Button
              kind="ghost"
              renderIcon={Renew}
              iconDescription="Refresh"
              onClick={() => fetchJobs(true)}
            >
              Refresh
            </Button>
            <Button
              renderIcon={Add}
              onClick={() => navigate('/submit')}
            >
              New Invoice
            </Button>
          </div>
        </div>

        {error && (
          <InlineNotification
            kind="error"
            title="Error loading jobs."
            subtitle={error}
            onCloseButtonClick={() => setError(null)}
            style={{ marginBottom: '1rem', maxWidth: '100%' }}
          />
        )}

        <Tabs selectedIndex={tabIndex} onChange={({ selectedIndex }) => setTabIndex(selectedIndex)}>
          <TabList aria-label="Job views" style={{ marginBottom: '1rem' }}>
            <Tab>All Jobs ({jobs.length})</Tab>
            <Tab>
              <span style={pendingReviewJobs.length > 0 ? { color: '#8a3ffc', fontWeight: 600 } : undefined}>
                Pending Review ({pendingReviewJobs.length})
              </span>
            </Tab>
            <Tab>Completed ({completedJobs.length})</Tab>
          </TabList>

          <TabPanels>
            <TabPanel style={{ padding: 0 }}>
              {loading && jobs.length === 0 ? (
                <DataTableSkeleton headers={HEADERS} rowCount={5} />
              ) : (
                <DataTable rows={tableRows} headers={HEADERS}>
                  {({
                    rows,
                    headers,
                    getHeaderProps,
                    getRowProps,
                    getTableProps,
                    getTableContainerProps,
                    onInputChange,
                  }) => (
                    <TableContainer
                      title="Pipeline History"
                      description="Real-time status of submitted invoices"
                      {...getTableContainerProps()}
                    >
                      <TableToolbar>
                        <TableToolbarContent>
                          <TableToolbarSearch onChange={onInputChange} placeholder="Search jobs…" />
                        </TableToolbarContent>
                      </TableToolbar>
                      <Table {...getTableProps()}>
                        <TableHead>
                          <TableRow>
                            {headers.map((header) => {
                              const headerProps = getHeaderProps({ header });
                              return (
                                <TableHeader {...headerProps} key={header.key}>
                                  {header.header}
                                </TableHeader>
                              );
                            })}
                          </TableRow>
                        </TableHead>
                        <TableBody>
                          {rows.length === 0 ? (
                            <TableRow>
                              <TableCell colSpan={headers.length} style={{ textAlign: 'center', padding: '2rem', color: 'var(--cds-text-secondary)' }}>
                                No jobs found.
                              </TableCell>
                            </TableRow>
                          ) : (
                            rows.map((row) => {
                              const job = jobs.find((j) => j.job_id === row.id);
                              const stepProgress = getStepProgress(row.cells.find((c) => c.info.header === 'status')?.value || '');
                              const isReviewable =
                                job?.status?.toLowerCase() === 'review' ||
                                job?.status?.toLowerCase() === 'pending_review';

                              const rowProps = getRowProps({ row });
                              return (
                                <TableRow {...rowProps} key={row.id}>
                                  {row.cells.map((cell) => {
                                    if (cell.info.header === 'status') {
                                      const tagColor = STATUS_TAG_COLOR[cell.value] || 'gray';
                                      return (
                                        <TableCell key={cell.id}>
                                          <Tag type={tagColor} size="sm">
                                            {cell.value}
                                          </Tag>
                                        </TableCell>
                                      );
                                    }
                                    if (cell.info.header === 'progress') {
                                      return (
                                        <TableCell key={cell.id} style={{ minWidth: '160px' }}>
                                          <ProgressBar
                                            label={stepProgress.label}
                                            value={stepProgress.value}
                                            max={1}
                                            size="small"
                                            hideLabel={false}
                                          />
                                        </TableCell>
                                      );
                                    }
                                    if (cell.info.header === 'error') {
                                      return (
                                        <TableCell key={cell.id}>
                                          {cell.value !== '—' ? (
                                            <span style={{ color: 'var(--cds-support-error, #da1e28)', fontSize: '0.75rem', fontFamily: 'monospace' }}>
                                              {cell.value}
                                            </span>
                                          ) : (
                                            '—'
                                          )}
                                        </TableCell>
                                      );
                                    }
                                    if (cell.info.header === 'actions') {
                                      return (
                                        <TableCell key={cell.id}>
                                          <div style={{ display: 'flex', gap: '0.5rem' }}>
                                            {isReviewable && (
                                              <Button
                                                kind="primary"
                                                size="sm"
                                                onClick={() => navigate(`/review/${row.id}`)}
                                              >
                                                Review
                                              </Button>
                                            )}
                                            <Button
                                              kind="ghost"
                                              size="sm"
                                              hasIconOnly
                                              renderIcon={DocumentView}
                                              iconDescription="View Job Details"
                                              onClick={() => navigate(`/jobs/${row.id}`)}
                                            />
                                          </div>
                                        </TableCell>
                                      );
                                    }
                                    return <TableCell key={cell.id}>{cell.value}</TableCell>;
                                  })}
                                </TableRow>
                              );
                            })
                          )}
                        </TableBody>
                      </Table>
                    </TableContainer>
                  )}
                </DataTable>
              )}

              {totalItems > 0 && (
                <Pagination
                  page={page}
                  pageSize={pageSize}
                  pageSizes={[10, 20, 50]}
                  totalItems={totalItems}
                  onChange={({ page: newPage, pageSize: newPageSize }) => {
                    setPage(newPage);
                    setPageSize(newPageSize);
                  }}
                />
              )}
            </TabPanel>

            <TabPanel style={{ padding: 0 }} />
            <TabPanel style={{ padding: 0 }} />
          </TabPanels>
        </Tabs>
      </Column>
    </Grid>
  );
};

export default JobsPage;
