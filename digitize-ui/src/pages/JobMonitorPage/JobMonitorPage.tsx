import { useReducer, useEffect } from 'react';
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

interface NotificationStatus {
  show: boolean;
  kind: 'success' | 'error' | 'info';
  title: string;
  subtitle?: string;
}

interface JobMonitorState {
  jobs: Job[];
  loading: boolean;
  page: number;
  pageSize: number;
  totalItems: number;
  selectedJob: Job | null;
  isSidePanelOpen: boolean;
  searchValue: string;
  isIngestSidePanelOpen: boolean;
  uploadStatus: NotificationStatus;
  deleteStatus: NotificationStatus;
}

type JobMonitorAction =
  | { type: 'SET_JOBS'; payload: { jobs: Job[]; totalItems: number } }
  | { type: 'SET_LOADING'; payload: boolean }
  | { type: 'SET_PAGE'; payload: number }
  | { type: 'SET_PAGE_SIZE'; payload: number }
  | { type: 'SET_SELECTED_JOB'; payload: Job | null }
  | { type: 'SET_SIDE_PANEL_OPEN'; payload: boolean }
  | { type: 'SET_SEARCH_VALUE'; payload: string }
  | { type: 'SET_INGEST_SIDE_PANEL_OPEN'; payload: boolean }
  | { type: 'SET_UPLOAD_STATUS'; payload: NotificationStatus }
  | { type: 'SET_DELETE_STATUS'; payload: NotificationStatus }
  | { type: 'HIDE_UPLOAD_STATUS' }
  | { type: 'HIDE_DELETE_STATUS' };

const initialState: JobMonitorState = {
  jobs: [],
  loading: false,
  page: 1,
  pageSize: 100,
  totalItems: 0,
  selectedJob: null,
  isSidePanelOpen: false,
  searchValue: '',
  isIngestSidePanelOpen: false,
  uploadStatus: { show: false, kind: 'info', title: '' },
  deleteStatus: { show: false, kind: 'info', title: '' },
};

const jobMonitorReducer = (
  state: JobMonitorState,
  action: JobMonitorAction
): JobMonitorState => {
  switch (action.type) {
    case 'SET_JOBS':
      return {
        ...state,
        jobs: action.payload.jobs,
        totalItems: action.payload.totalItems,
      };
    case 'SET_LOADING':
      return {
        ...state,
        loading: action.payload,
      };
    case 'SET_PAGE':
      return {
        ...state,
        page: action.payload,
      };
    case 'SET_PAGE_SIZE':
      return {
        ...state,
        pageSize: action.payload,
      };
    case 'SET_SELECTED_JOB':
      return {
        ...state,
        selectedJob: action.payload,
      };
    case 'SET_SIDE_PANEL_OPEN':
      return {
        ...state,
        isSidePanelOpen: action.payload,
      };
    case 'SET_SEARCH_VALUE':
      return {
        ...state,
        searchValue: action.payload,
      };
    case 'SET_INGEST_SIDE_PANEL_OPEN':
      return {
        ...state,
        isIngestSidePanelOpen: action.payload,
      };
    case 'SET_UPLOAD_STATUS':
      return {
        ...state,
        uploadStatus: action.payload,
      };
    case 'SET_DELETE_STATUS':
      return {
        ...state,
        deleteStatus: action.payload,
      };
    case 'HIDE_UPLOAD_STATUS':
      return {
        ...state,
        uploadStatus: { show: false, kind: 'info', title: '' },
      };
    case 'HIDE_DELETE_STATUS':
      return {
        ...state,
        deleteStatus: { show: false, kind: 'info', title: '' },
      };
    default:
      return state;
  }
};

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
  const [state, dispatch] = useReducer(jobMonitorReducer, initialState);

  const fetchJobs = async () => {
    dispatch({ type: 'SET_LOADING', payload: true });
    try {
      const offset = (state.page - 1) * state.pageSize;
      const response = await getAllJobs({
        limit: state.pageSize,
        offset: offset,
      });
      
      dispatch({
        type: 'SET_JOBS',
        payload: {
          jobs: response.data || [],
          totalItems: response.pagination?.total || 0,
        },
      });
    } catch (error) {
      console.error('Error fetching jobs:', error);
    } finally {
      dispatch({ type: 'SET_LOADING', payload: false });
    }
  };

  useEffect(() => {
    fetchJobs();
    const interval = setInterval(fetchJobs, 10000);
    return () => clearInterval(interval);
  }, [state.page, state.pageSize]);

  const handleViewDetails = async (jobId: string) => {
    try {
      const jobDetails = await getJobById(jobId);
      dispatch({ type: 'SET_SELECTED_JOB', payload: jobDetails });
      dispatch({ type: 'SET_SIDE_PANEL_OPEN', payload: true });
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
      dispatch({
        type: 'SET_UPLOAD_STATUS',
        payload: {
          show: true,
          kind: 'info',
          title: 'Uploading documents...',
          subtitle: `Uploading ${files.length} file(s)`,
        },
      });

      const response = await uploadDocuments(files, operation, outputFormat);

      dispatch({
        type: 'SET_UPLOAD_STATUS',
        payload: {
          show: true,
          kind: 'success',
          title: 'Documents uploaded successfully',
          subtitle: `Job ID: ${response.job_id}`,
        },
      });

      // Refresh jobs list after successful upload
      setTimeout(() => {
        fetchJobs();
        dispatch({ type: 'HIDE_UPLOAD_STATUS' });
      }, 3000);
    } catch (error: any) {
      console.error('Error uploading documents:', error);
      dispatch({
        type: 'SET_UPLOAD_STATUS',
        payload: {
          show: true,
          kind: 'error',
          title: 'Upload failed',
          subtitle: error.response?.data?.message || error.message || 'An error occurred',
        },
      });

      // Hide error after 5 seconds
      setTimeout(() => {
        dispatch({ type: 'HIDE_UPLOAD_STATUS' });
      }, 5000);
    }
  };

  const handleDeleteJobs = async (selectedRows: any[]) => {
    try {
      const jobIds = selectedRows.map(row => row.id);
      
      dispatch({
        type: 'SET_DELETE_STATUS',
        payload: {
          show: true,
          kind: 'info',
          title: 'Deleting jobs...',
          subtitle: `Deleting ${jobIds.length} job(s)`,
        },
      });

      if (jobIds.length === 1) {
        await deleteJob(jobIds[0]);
      } else {
        await bulkDeleteJobs(jobIds);
      }

      dispatch({
        type: 'SET_DELETE_STATUS',
        payload: {
          show: true,
          kind: 'success',
          title: 'Jobs deleted successfully',
          subtitle: `${jobIds.length} job(s) deleted`,
        },
      });

      // Refresh jobs list after successful deletion
      setTimeout(() => {
        fetchJobs();
        dispatch({ type: 'HIDE_DELETE_STATUS' });
      }, 2000);
    } catch (error: any) {
      console.error('Error deleting jobs:', error);
      dispatch({
        type: 'SET_DELETE_STATUS',
        payload: {
          show: true,
          kind: 'error',
          title: 'Delete failed',
          subtitle: error.response?.data?.message || error.message || 'An error occurred',
        },
      });

      // Hide error after 5 seconds
      setTimeout(() => {
        dispatch({ type: 'HIDE_DELETE_STATUS' });
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

  const filteredJobs = state.jobs.filter((job) => {
    if (state.searchValue === '') return true;
    const jobName = getJobName(job).toLowerCase();
    const jobType = getJobType(job).toLowerCase();
    const jobStatus = getJobStatus(job).toLowerCase();
    return jobName.includes(state.searchValue.toLowerCase()) ||
           jobType.includes(state.searchValue.toLowerCase()) ||
           jobStatus.includes(state.searchValue.toLowerCase());
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
        {state.uploadStatus.show && (
          <div className={styles.notificationWrapper}>
            <InlineNotification
              kind={state.uploadStatus.kind}
              title={state.uploadStatus.title}
              subtitle={state.uploadStatus.subtitle}
              onClose={() => dispatch({ type: 'HIDE_UPLOAD_STATUS' })}
              hideCloseButton={false}
              lowContrast
            />
          </div>
        )}

        {/* Delete Status Notification */}
        {state.deleteStatus.show && (
          <div className={styles.notificationWrapper}>
            <InlineNotification
              kind={state.deleteStatus.kind}
              title={state.deleteStatus.title}
              subtitle={state.deleteStatus.subtitle}
              onClose={() => dispatch({ type: 'HIDE_DELETE_STATUS' })}
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
                        onChange={(e: any, value?: string) => dispatch({ type: 'SET_SEARCH_VALUE', payload: value || '' })}
                        value={state.searchValue}
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
                        disabled={state.loading}
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
                        onClick={() => dispatch({ type: 'SET_INGEST_SIDE_PANEL_OPEN', payload: true })}
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
                      page={state.page}
                      pageSize={state.pageSize}
                      pageSizes={[10, 25, 50, 100]}
                      totalItems={state.totalItems}
                      onChange={({ page, pageSize }) => {
                        dispatch({ type: 'SET_PAGE', payload: page });
                        dispatch({ type: 'SET_PAGE_SIZE', payload: pageSize });
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
          open={state.isIngestSidePanelOpen}
          onClose={() => dispatch({ type: 'SET_INGEST_SIDE_PANEL_OPEN', payload: false })}
          onSubmit={handleIngestSubmit}
        />

        {/* Job Details Side Panel */}
        <SidePanel
          open={state.isSidePanelOpen}
          onRequestClose={() => dispatch({ type: 'SET_SIDE_PANEL_OPEN', payload: false })}
          title="Documents"
          slideIn
          selectorPageContent=".jobMonitorPage"
          placement="right"
          size="md"
          includeOverlay
        >
          {state.selectedJob && (
            <div className={styles.sidePanelContent}>
              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Job name</h6>
                <p className={styles.sectionValue}>{getJobName(state.selectedJob)}</p>
              </div>

              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Ingested PDF files</h6>
                {state.selectedJob.documents && state.selectedJob.documents.length > 0 ? (
                  <div className={styles.documentTagsList}>
                    {state.selectedJob.documents.map((doc, idx) => (
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
