import { useState, useEffect } from 'react';
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
  TableToolbarMenu,
  TableToolbarAction,
  TableBatchActions,
  TableBatchAction,
  TableSelectAll,
  TableSelectRow,
  Pagination,
  Button,
  Tag,
  Theme,
  Link,
  InlineNotification,
} from '@carbon/react';
import { SidePanel, NoDataEmptyState } from '@carbon/ibm-products';
import { Download, Renew, Settings, Add, CheckmarkFilled, WarningFilled, InProgress, ErrorFilled, TrashCan, Close } from '@carbon/icons-react';
import { useTheme } from '../../contexts/ThemeContext';
import { getAllJobs, getJobById, uploadDocuments, deleteJob, bulkDeleteJobs, Job, Document } from '../../services/api';
import IngestSidePanel from '../../components/IngestSidePanel';
import { calculateDuration } from '../../utils/dateUtils';
import { JOB_STATUS, DISPLAY_STATUS, JOB_OPERATION, JOB_TYPE_DISPLAY } from '../../constants/jobConstants';
import styles from './JobMonitorPage.module.scss';

const headers = [
  { key: 'job_name', header: 'Job name' },
  { key: 'type', header: 'Type' },
  { key: 'status', header: 'Status' },
  { key: 'started', header: 'Started' },
  { key: 'duration', header: 'Duration' },
  { key: 'actions', header: '' },
];

const getStatusIcon = (status: string) => {
  switch (status) {
    case JOB_STATUS.COMPLETED:
    case DISPLAY_STATUS.INGESTED:
    case DISPLAY_STATUS.DIGITIZED:
      return <CheckmarkFilled size={16} className={styles.statusIconSuccess} />;
    case JOB_STATUS.FAILED:
    case DISPLAY_STATUS.INGESTION_ERROR:
    case DISPLAY_STATUS.DIGITIZATION_ERROR:
      return <ErrorFilled size={16} className={styles.statusIconError} />;
    case JOB_STATUS.IN_PROGRESS:
    case DISPLAY_STATUS.INGESTING:
    case DISPLAY_STATUS.DIGITIZING:
      return <InProgress size={16} className={styles.statusIconProgress} />;
    default:
      return null;
  }
};

const getTypeTagStyle = (type: string) => {
  if (type === JOB_TYPE_DISPLAY.INGESTION) {
    return 'gray';
  } else if (type === JOB_TYPE_DISPLAY.DIGITIZATION) {
    return 'cool-gray';
  }
  return 'gray';
};

const JobMonitorPage = () => {
  const { effectiveTheme } = useTheme();
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loading, setLoading] = useState<boolean>(false);
  const [page, setPage] = useState<number>(1);
  const [pageSize, setPageSize] = useState<number>(100);
  const [totalItems, setTotalItems] = useState<number>(0);
  const [selectedJob, setSelectedJob] = useState<Job | null>(null);
  const [isSidePanelOpen, setIsSidePanelOpen] = useState<boolean>(false);
  const [searchValue, setSearchValue] = useState<string>('');
  const [isIngestSidePanelOpen, setIsIngestSidePanelOpen] = useState<boolean>(false);
  const [uploadStatus, setUploadStatus] = useState<{
    show: boolean;
    kind: 'success' | 'error' | 'info';
    title: string;
    subtitle?: string;
  }>({ show: false, kind: 'info', title: '' });
  const [deleteStatus, setDeleteStatus] = useState<{
    show: boolean;
    kind: 'success' | 'error' | 'info';
    title: string;
    subtitle?: string;
  }>({ show: false, kind: 'info', title: '' });

  const fetchJobs = async () => {
    setLoading(true);
    try {
      const offset = (page - 1) * pageSize;
      const response = await getAllJobs({
        limit: pageSize,
        offset: offset,
      });
      
      setJobs(response.data || []);
      setTotalItems(response.pagination?.total || 0);
    } catch (error) {
      console.error('Error fetching jobs:', error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchJobs();
    const interval = setInterval(fetchJobs, 10000);
    return () => clearInterval(interval);
  }, [page, pageSize]);

  const handleViewDetails = async (jobId: string) => {
    try {
      const jobDetails = await getJobById(jobId);
      setSelectedJob(jobDetails);
      setIsSidePanelOpen(true);
    } catch (error) {
      console.error('Error fetching job details:', error);
    }
  };

  const handleIngestSubmit = async (
    jobName: string,
    operation: string,
    outputFormat: string,
    files: File[]
  ) => {
    try {
      setUploadStatus({
        show: true,
        kind: 'info',
        title: 'Uploading documents...',
        subtitle: `Uploading ${files.length} file(s)`,
      });

      const response = await uploadDocuments(files, operation, outputFormat);

      setUploadStatus({
        show: true,
        kind: 'success',
        title: 'Documents uploaded successfully',
        subtitle: `Job ID: ${response.job_id}`,
      });

      // Refresh jobs list after successful upload
      setTimeout(() => {
        fetchJobs();
        setUploadStatus({ show: false, kind: 'info', title: '' });
      }, 3000);
    } catch (error: any) {
      console.error('Error uploading documents:', error);
      setUploadStatus({
        show: true,
        kind: 'error',
        title: 'Upload failed',
        subtitle: error.response?.data?.message || error.message || 'An error occurred',
      });

      // Hide error after 5 seconds
      setTimeout(() => {
        setUploadStatus({ show: false, kind: 'info', title: '' });
      }, 5000);
    }
  };

  const handleDeleteJobs = async (selectedRows: any[]) => {
    try {
      const jobIds = selectedRows.map(row => row.id);
      
      setDeleteStatus({
        show: true,
        kind: 'info',
        title: 'Deleting jobs...',
        subtitle: `Deleting ${jobIds.length} job(s)`,
      });

      if (jobIds.length === 1) {
        await deleteJob(jobIds[0]);
      } else {
        await bulkDeleteJobs(jobIds);
      }

      setDeleteStatus({
        show: true,
        kind: 'success',
        title: 'Jobs deleted successfully',
        subtitle: `${jobIds.length} job(s) deleted`,
      });

      // Refresh jobs list after successful deletion
      setTimeout(() => {
        fetchJobs();
        setDeleteStatus({ show: false, kind: 'info', title: '' });
      }, 2000);
    } catch (error: any) {
      console.error('Error deleting jobs:', error);
      setDeleteStatus({
        show: true,
        kind: 'error',
        title: 'Delete failed',
        subtitle: error.response?.data?.message || error.message || 'An error occurred',
      });

      // Hide error after 5 seconds
      setTimeout(() => {
        setDeleteStatus({ show: false, kind: 'info', title: '' });
      }, 5000);
    }
  };

  const getJobName = (job: Job) => {
    if (job.documents && job.documents.length > 0) {
      return job.documents[0].name || job.job_id;
    }
    return job.job_id;
  };

  const getJobType = (job: Job) => {
    return job.operation === JOB_OPERATION.INGESTION ? JOB_TYPE_DISPLAY.INGESTION : JOB_TYPE_DISPLAY.DIGITIZATION;
  };

  const getJobStatus = (job: Job) => {
    if (job.status === JOB_STATUS.COMPLETED) {
      return job.operation === JOB_OPERATION.INGESTION ? DISPLAY_STATUS.INGESTED : DISPLAY_STATUS.DIGITIZED;
    } else if (job.status === JOB_STATUS.FAILED) {
      return job.operation === JOB_OPERATION.INGESTION ? DISPLAY_STATUS.INGESTION_ERROR : DISPLAY_STATUS.DIGITIZATION_ERROR;
    } else if (job.status === JOB_STATUS.IN_PROGRESS) {
      return job.operation === JOB_OPERATION.INGESTION ? DISPLAY_STATUS.INGESTING : DISPLAY_STATUS.DIGITIZING;
    }
    return job.status;
  };

  const getErrorMessage = (job: Job) => {
    if (job.status === JOB_STATUS.FAILED && job.error) {
      return job.error;
    }
    return 'Error message goes here';
  };

  const filteredJobs = jobs.filter((job) => {
    if (searchValue === '') return true;
    const jobName = getJobName(job).toLowerCase();
    const jobType = getJobType(job).toLowerCase();
    const jobStatus = getJobStatus(job).toLowerCase();
    return jobName.includes(searchValue.toLowerCase()) ||
           jobType.includes(searchValue.toLowerCase()) ||
           jobStatus.includes(searchValue.toLowerCase());
  });

  const rows = filteredJobs.map((job) => {
    const jobStatus = getJobStatus(job);
    const hasError = job.status === 'failed';
    
    return {
      id: job.job_id,
      job_name: getJobName(job),
      type: (
        <Tag type={getTypeTagStyle(getJobType(job))} size="md">
          {getJobType(job)}
        </Tag>
      ),
      status: (
        <div className={styles.statusCell}>
          {getStatusIcon(jobStatus)}
          <span className={styles.statusText}>{jobStatus}</span>
        </div>
      ),
      started: job.submitted_at
        ? new Date(job.submitted_at).toLocaleString('en-US', {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
            hour: 'numeric',
            minute: '2-digit',
            hour12: true,
          })
        : 'N/A',
      duration: calculateDuration(job.submitted_at),
      actions: hasError ? (
        <div className={styles.errorMessage}>
          <ErrorFilled size={16} className={styles.errorIcon} />
          <span>{getErrorMessage(job)}</span>
        </div>
      ) : (
        <Button
          kind="ghost"
          size="sm"
          onClick={() => handleViewDetails(job.job_id)}
        >
          View documents
        </Button>
      ),
    };
  });

  return (
    <Theme theme={effectiveTheme}>
      <div className={styles.jobMonitorPage}>
        {/* Upload Status Notification */}
        {uploadStatus.show && (
          <div className={styles.notificationWrapper}>
            <InlineNotification
              kind={uploadStatus.kind}
              title={uploadStatus.title}
              subtitle={uploadStatus.subtitle}
              onClose={() => setUploadStatus({ show: false, kind: 'info', title: '' })}
              hideCloseButton={false}
              lowContrast
            />
          </div>
        )}

        {/* Delete Status Notification */}
        {deleteStatus.show && (
          <div className={styles.notificationWrapper}>
            <InlineNotification
              kind={deleteStatus.kind}
              title={deleteStatus.title}
              subtitle={deleteStatus.subtitle}
              onClose={() => setDeleteStatus({ show: false, kind: 'info', title: '' })}
              hideCloseButton={false}
              lowContrast
            />
          </div>
        )}

        {/* Page Header */}
        <div className={styles.pageHeader}>
          <div className={styles.headerContent}>
            <h1 className={styles.pageTitle}>Ingested documents log</h1>
            <Link href="#" className={styles.learnMore}>
              Learn more →
            </Link>
          </div>
        </div>

        {/* Data Table with Enhanced Toolbar */}
        <div className={styles.tableWrapper}>
          <DataTable rows={rows} headers={headers} size="lg">
            {({
              rows,
              headers,
              getHeaderProps,
              getRowProps,
              getTableProps,
              getSelectionProps,
              getToolbarProps,
              getBatchActionProps,
              selectedRows,
              getTableContainerProps,
            }) => {
              const batchActionProps = getBatchActionProps();
              
              return (
                <TableContainer
                  {...getTableContainerProps()}
                  className={styles.tableContainer}
                >
                  <TableToolbar {...getToolbarProps()}>
                    <TableBatchActions {...batchActionProps}>
                      <TableBatchAction
                        tabIndex={batchActionProps.shouldShowBatchActions ? 0 : -1}
                        renderIcon={TrashCan}
                        onClick={() => handleDeleteJobs(selectedRows)}
                      >
                        Delete
                      </TableBatchAction>
                    </TableBatchActions>
                    <TableToolbarContent>
                      <TableToolbarSearch
                        persistent
                        placeholder="Search"
                        onChange={(e: any, value?: string) => setSearchValue(value || '')}
                        value={searchValue}
                      />
                      <Button
                        kind="ghost"
                        hasIconOnly
                        renderIcon={Download}
                        iconDescription="Download"
                        tooltipPosition="bottom"
                      />
                      <Button
                        kind="ghost"
                        hasIconOnly
                        renderIcon={Renew}
                        iconDescription="Refresh"
                        onClick={fetchJobs}
                        disabled={loading}
                        tooltipPosition="bottom"
                      />
                      <TableToolbarMenu
                        renderIcon={Settings}
                        iconDescription="Settings"
                      >
                        <TableToolbarAction onClick={() => console.log('Action 1')}>
                          Action 1
                        </TableToolbarAction>
                        <TableToolbarAction onClick={() => console.log('Action 2')}>
                          Action 2
                        </TableToolbarAction>
                        <TableToolbarAction onClick={() => console.log('Action 3')}>
                          Action 3
                        </TableToolbarAction>
                      </TableToolbarMenu>
                      <Button
                        kind="primary"
                        renderIcon={Add}
                        onClick={() => setIsIngestSidePanelOpen(true)}
                      >
                        Ingest
                      </Button>
                    </TableToolbarContent>
                  </TableToolbar>
                  <Table {...getTableProps()} className={styles.table}>
                    <TableHead>
                      <TableRow>
                        <TableSelectAll {...getSelectionProps()} />
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
                      {rows.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={headers.length + 1} className={styles.emptyStateCell}>
                            <NoDataEmptyState
                              illustrationTheme="light"
                              size="lg"
                              title="Start by ingesting a document"
                              subtitle="To ingest a document, click Ingest."
                            />
                          </TableCell>
                        </TableRow>
                      ) : (
                        rows.map((row) => {
                          const { key: rowKey, ...rowProps } = getRowProps({ row });
                          return (
                            <TableRow key={rowKey} {...rowProps}>
                              <TableSelectRow {...getSelectionProps({ row })} />
                              {row.cells.map((cell) => (
                                <TableCell key={cell.id}>{cell.value}</TableCell>
                              ))}
                            </TableRow>
                          );
                        })
                      )}
                    </TableBody>
                  </Table>
                  {rows.length > 0 && (
                    <Pagination
                      page={page}
                      pageSize={pageSize}
                      pageSizes={[10, 25, 50, 100]}
                      totalItems={totalItems}
                      onChange={({ page, pageSize }) => {
                        setPage(page);
                        setPageSize(pageSize);
                      }}
                      itemsPerPageText="Items per page:"
                    />
                  )}
                </TableContainer>
              );
            }}
          </DataTable>
        </div>

        {/* Ingest Side Panel */}
        <IngestSidePanel
          open={isIngestSidePanelOpen}
          onClose={() => setIsIngestSidePanelOpen(false)}
          onSubmit={handleIngestSubmit}
        />

        {/* Job Details Side Panel */}
        <SidePanel
          open={isSidePanelOpen}
          onRequestClose={() => setIsSidePanelOpen(false)}
          title="Documents"
          slideIn
          selectorPageContent=".jobMonitorPage"
          placement="right"
          size="md"
          includeOverlay
        >
          {selectedJob && (
            <div className={styles.sidePanelContent}>
              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Job name</h6>
                <p className={styles.sectionValue}>{getJobName(selectedJob)}</p>
              </div>

              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Ingested PDF files</h6>
                {selectedJob.documents && selectedJob.documents.length > 0 ? (
                  <div className={styles.documentTagsList}>
                    {selectedJob.documents.map((doc, idx) => (
                      <Tag
                        key={idx}
                        type="gray"
                        size="md"
                        className={styles.documentTag}
                      >
                        {doc.name}
                      </Tag>
                    ))}
                  </div>
                ) : (
                  <p className={styles.noDocuments}>No documents</p>
                )}
              </div>
            </div>
          )}
        </SidePanel>
      </div>
    </Theme>
  );
};

export default JobMonitorPage;

// Made with Bob
