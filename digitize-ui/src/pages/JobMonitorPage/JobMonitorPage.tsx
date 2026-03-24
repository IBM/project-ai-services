import { useReducer, useEffect } from 'react';
import {
  DataTable,
  DataTableSkeleton,
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
  Tag,
  Theme,
  Link,
  InlineNotification,
  Modal,
  Checkbox,
  CheckboxGroup,
  ActionableNotification,
  TextInput,
  InlineLoading,
  Tooltip,
} from '@carbon/react';
import { SidePanel, NoDataEmptyState } from '@carbon/ibm-products';
import { Download, Renew, Add, CheckmarkFilled, InProgress, ErrorFilled, TrashCan } from '@carbon/icons-react';
import { useTheme } from '../../contexts/useTheme';
import { getAllJobs, getJobById, uploadDocuments, deleteJob, Job } from '../../services/api';
import IngestSidePanel from '../../components/IngestSidePanel';
import { calculateDuration } from '../../utils/dateUtils';
import { exportToCSV, validateFilename } from '../../utils/csvExport';
import { JOB_STATUS, DISPLAY_STATUS, JOB_OPERATION, JOB_TYPE_DISPLAY } from '../../constants/jobConstants';
import styles from './JobMonitorPage.module.scss';

interface NotificationStatus {
  show: boolean;
  kind: 'success' | 'error' | 'info';
  title: string;
  subtitle?: string;
}

export type ExportStatus = 'idle' | 'exporting' | 'success' | 'error';

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
  showDeleteModal: boolean;
  jobToDelete: string | null;
  isConfirmed: boolean;
  toastOpen: boolean;
  errorMessage: string;
  errorJobName: string;
  isDeleting: boolean;
  isExportDialogOpen: boolean;
  csvFileName: string;
  exportStatus: ExportStatus;
  exportErrorMessage: string;
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
  | { type: 'HIDE_DELETE_STATUS' }
  | { type: 'OPEN_DELETE_MODAL'; payload: string }
  | { type: 'CLOSE_DELETE_MODAL' }
  | { type: 'SET_CONFIRMED'; payload: boolean }
  | { type: 'SHOW_ERROR'; payload: { message: string; jobName?: string } }
  | { type: 'HIDE_ERROR' }
  | { type: 'SET_IS_DELETING'; payload: boolean }
  | { type: 'DELETE_JOB'; payload: string }
  | { type: 'OPEN_EXPORT_DIALOG' }
  | { type: 'CLOSE_EXPORT_DIALOG' }
  | { type: 'SET_CSV_FILENAME'; payload: string }
  | { type: 'SET_EXPORT_STATUS'; payload: ExportStatus }
  | { type: 'SET_EXPORT_ERROR'; payload: string }
  | { type: 'CLEAR_EXPORT_ERROR' };

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
  showDeleteModal: false,
  jobToDelete: null,
  isConfirmed: false,
  toastOpen: false,
  errorMessage: '',
  errorJobName: '',
  isDeleting: false,
  isExportDialogOpen: false,
  csvFileName: '',
  exportStatus: 'idle',
  exportErrorMessage: '',
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
    case 'OPEN_DELETE_MODAL':
      return {
        ...state,
        jobToDelete: action.payload,
        showDeleteModal: true,
        toastOpen: false,
      };
    case 'CLOSE_DELETE_MODAL':
      return {
        ...state,
        showDeleteModal: false,
        isConfirmed: false,
        jobToDelete: null,
      };
    case 'SET_CONFIRMED':
      return { ...state, isConfirmed: action.payload };
    case 'DELETE_JOB':
      return {
        ...state,
        jobs: state.jobs.filter((j) => j.job_id !== action.payload),
        showDeleteModal: false,
        isConfirmed: false,
      };
    case 'SHOW_ERROR':
      return {
        ...state,
        errorMessage: action.payload.message,
        errorJobName: action.payload.jobName ?? '',
        toastOpen: true,
        isDeleting: false,
      };
    case 'HIDE_ERROR':
      return {
        ...state,
        toastOpen: false,
        jobToDelete: null,
        errorJobName: '',
      };
    case 'SET_IS_DELETING':
      return { ...state, isDeleting: action.payload };
    case 'OPEN_EXPORT_DIALOG':
      return {
        ...state,
        isExportDialogOpen: true,
        csvFileName: '',
        exportErrorMessage: '',
        exportStatus: 'idle',
      };
    case 'CLOSE_EXPORT_DIALOG':
      return {
        ...state,
        isExportDialogOpen: false,
      };
    case 'SET_CSV_FILENAME':
      return { ...state, csvFileName: action.payload };
    case 'SET_EXPORT_STATUS':
      return { ...state, exportStatus: action.payload };
    case 'SET_EXPORT_ERROR':
      return {
        ...state,
        exportErrorMessage: action.payload,
      };
    case 'CLEAR_EXPORT_ERROR':
      return { ...state, exportErrorMessage: '' };
    default:
      return state;
  }
};

const headers = [
  { key: 'job_name', header: 'Job Name' },
  { key: 'type', header: 'Type' },
  { key: 'status', header: 'Status' },
  { key: 'started', header: 'Started' },
  { key: 'duration', header: 'Duration' },
  { key: 'view_action', header: '' },
  { key: 'delete_action', header: '' },
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
    case JOB_STATUS.ACCEPTED:
    case JOB_STATUS.IN_PROGRESS:
    case DISPLAY_STATUS.ACCEPTED:
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
    } catch (error: any) {
      // Handle 5xx server errors with appropriate user-facing messages
      if (error.response && error.response.status >= 500 && error.response.status < 600) {
        const errorMessage = error.response.status === 503
          ? 'The service is temporarily unavailable. Please try again in a few moments.'
          : 'A server error occurred while fetching jobs. Please try again later.';

        dispatch({
          type: 'SHOW_ERROR',
          payload: {
            message: errorMessage,
          },
        });
      } else if (error.code === 'ECONNABORTED' || error.message === 'Network Error') {
        // Handle network/timeout errors
        dispatch({
          type: 'SHOW_ERROR',
          payload: {
            message: 'Unable to connect to the server. Please check your network connection and try again.',
          },
        });
      }
    } finally {
      dispatch({ type: 'SET_LOADING', payload: false });
    }
  };

  useEffect(() => {
    fetchJobs();
    const interval = setInterval(fetchJobs, 10000);
    return () => clearInterval(interval);
  // eslint-disable-next-line react-hooks/exhaustive-deps
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
    operation: string,
    outputFormat: string,
    files: File[],
    jobName: string
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

      const response = await uploadDocuments(files, operation, outputFormat, jobName);

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
      
      // Handle FastAPI validation errors (422) which return detail as an array
      let errorMessage = 'An error occurred';
      if (error.response?.data?.detail) {
        const detail = error.response.data.detail;
        if (Array.isArray(detail)) {
          // FastAPI validation error format
          errorMessage = detail.map((err: any) => err.msg || JSON.stringify(err)).join(', ');
        } else if (typeof detail === 'string') {
          errorMessage = detail;
        }
      } else if (error.response?.data?.message) {
        errorMessage = error.response.data.message;
      } else if (error.message) {
        errorMessage = error.message;
      }
      
      dispatch({
        type: 'SET_UPLOAD_STATUS',
        payload: {
          show: true,
          kind: 'error',
          title: 'Upload failed',
          subtitle: errorMessage,
        },
      });

      // Hide error after 5 seconds
      setTimeout(() => {
        dispatch({ type: 'HIDE_UPLOAD_STATUS' });
      }, 5000);
    }
  };

  const handleDeleteConfirm = async () => {
    if (!state.jobToDelete) return;

    const jobName = getJobName(state.jobs.find((j) => j.job_id === state.jobToDelete)!);
    dispatch({ type: 'SET_IS_DELETING', payload: true });

    try {
      await deleteJob(state.jobToDelete);
      dispatch({ type: 'DELETE_JOB', payload: state.jobToDelete });
      
      // Show success notification
      dispatch({
        type: 'SET_DELETE_STATUS',
        payload: {
          show: true,
          kind: 'success',
          title: 'Job deleted successfully',
          subtitle: `"${jobName}" has been removed`,
        },
      });

      // Hide success notification after 3 seconds
      setTimeout(() => {
        dispatch({ type: 'HIDE_DELETE_STATUS' });
      }, 3000);

      fetchJobs();
    } catch (error: any) {
      const msg = error.response?.data?.detail || error.message || 'Failed deleting job';
      
      // Show error notification
      dispatch({
        type: 'SET_DELETE_STATUS',
        payload: {
          show: true,
          kind: 'error',
          title: 'Failed to delete job',
          subtitle: `${jobName}: ${msg}`,
        },
      });

      // Hide error notification after 5 seconds
      setTimeout(() => {
        dispatch({ type: 'HIDE_DELETE_STATUS' });
      }, 5000);
    } finally {
      dispatch({ type: 'SET_IS_DELETING', payload: false });
      dispatch({ type: 'CLOSE_DELETE_MODAL' });
    }
  };

  const getJobName = (job: Job) => {
    // First priority: use job_name if available
    if (job.job_name) {
      return job.job_name;
    }
    // Fallback: use first document name if available
    if (job.documents && job.documents.length > 0) {
      return job.documents[0].name || job.job_id;
    }
    // Last resort: use job_id
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
    } else if (job.status === JOB_STATUS.ACCEPTED) {
      return DISPLAY_STATUS.ACCEPTED;
    }
    return job.status;
  };

  const getErrorMessage = (job: Job) => {
    if (job.status === JOB_STATUS.FAILED && job.error) {
      return job.error;
    }
    return 'Error message goes here';
  };

  const handleExportCSV = async () => {
    const filename = state.csvFileName.trim();
    const validationError = validateFilename(filename);

    if (validationError) {
      dispatch({
        type: 'SET_EXPORT_ERROR',
        payload: validationError,
      });
      return;
    }

    if (filteredJobs.length === 0) {
      dispatch({
        type: 'SET_EXPORT_ERROR',
        payload: 'No data available to export',
      });
      return;
    }

    dispatch({
      type: 'SET_EXPORT_STATUS',
      payload: 'exporting',
    });

    try {
      // Create rows for export (excluding action columns)
      const exportRows = filteredJobs.map((job) => ({
        job_name: getJobName(job),
        type: getJobType(job),
        status: getJobStatus(job),
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
        duration: calculateDuration(job.submitted_at, job.completed_at),
      }));

      exportToCSV({
        filename,
        headers,
        rows: exportRows,
        excludeColumns: ['view_action', 'delete_action'],
      });

      dispatch({
        type: 'SET_EXPORT_STATUS',
        payload: 'success',
      });
    } catch (error: any) {
      dispatch({
        type: 'SET_EXPORT_STATUS',
        payload: 'error',
      });

      dispatch({
        type: 'SET_EXPORT_ERROR',
        payload: error.message || 'An error occurred while exporting the CSV file. Please try again.',
      });
    }
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
          {hasError && (
            <Tooltip
              align="bottom"
              autoAlign={true}
              label={getErrorMessage(job)}
              className={styles.errorTooltip}
            >
              <button
                type="button"
                className={styles.errorInfoButton}
                aria-label="Error details"
              >
                <ErrorFilled size={16} className={styles.errorInfoIcon} />
              </button>
            </Tooltip>
          )}
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
      duration: calculateDuration(job.submitted_at, job.completed_at),
      view_action: (
        <Button
          kind="ghost"
          size="sm"
          onClick={() => handleViewDetails(job.job_id)}
        >
          View details
        </Button>
      ),
      delete_action: (
        <Button
          hasIconOnly
          kind="ghost"
          size="sm"
          renderIcon={TrashCan}
          iconDescription="Delete"
          onClick={() => dispatch({ type: 'OPEN_DELETE_MODAL', payload: job.job_id })}
        />
      ),
    };
  });

  return (
    <Theme theme={effectiveTheme}>
      <div className={styles.jobMonitorPage}>
        {state.toastOpen && (
          <ActionableNotification
            actionButtonLabel="Try again"
            aria-label="close notification"
            kind="error"
            closeOnEscape
            title={state.errorJobName ? `Delete job ${state.errorJobName} failed` : 'Error loading jobs'}
            subtitle={state.errorMessage}
            onActionButtonClick={() => {
              dispatch({ type: 'HIDE_ERROR' });
              fetchJobs();
            }}
            onCloseButtonClick={() => {
              dispatch({ type: 'HIDE_ERROR' });
            }}
            style={{
              position: 'fixed',
              top: '4rem',
              right: '2rem',
              zIndex: 9999,
            }}
          />
        )}
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
            <Link href="https://www.ibm.com/docs/en/aiservices/2025.12.0?topic=services-introduction" className={styles.learnMore} target="_blank" rel="noopener noreferrer">
              Learn more →
            </Link>
          </div>
        </div>

        {/* Data Table with Enhanced Toolbar */}
        <div className={styles.tableWrapper}>
          {state.loading && state.jobs.length === 0 ? (
            <DataTableSkeleton
              headers={headers}
              aria-label="Loading jobs"
              showHeader={false}
              showToolbar={false}
            />
          ) : (
            <DataTable rows={rows} headers={headers} size="lg">
              {({
                rows,
                headers,
                getHeaderProps,
                getRowProps,
                getTableProps,
                getTableContainerProps,
              }) => {
                return (
                  <TableContainer
                    {...getTableContainerProps()}
                    className={styles.tableContainer}
                  >
                    <TableToolbar>
                      <TableToolbarContent>
                        <TableToolbarSearch
                          persistent
                          placeholder="Search"
                          onChange={(_e: any, value?: string) => dispatch({ type: 'SET_SEARCH_VALUE', payload: value || '' })}
                          value={state.searchValue}
                        />
                        <Button
                          kind="ghost"
                          hasIconOnly
                          renderIcon={Download}
                          iconDescription="Download"
                          tooltipPosition="bottom"
                          onClick={() => dispatch({ type: 'OPEN_EXPORT_DIALOG' })}
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
                            <TableCell colSpan={headers.length} className={styles.emptyStateCell}>
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
          )}
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
          title="Job Details"
          slideIn
          selectorPageContent=".jobMonitorPage"
          placement="right"
          size="md"
          includeOverlay
        >
          {state.selectedJob && (
            <div className={styles.sidePanelContent}>
              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Job ID</h6>
                <p className={styles.sectionValue}>{state.selectedJob.job_id}</p>
              </div>

              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Operation</h6>
                <p className={styles.sectionValue}>{state.selectedJob.operation}</p>
              </div>

              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Status</h6>
                <div className={styles.statusCell}>
                  {getStatusIcon(getJobStatus(state.selectedJob))}
                  <span className={styles.statusText}>{getJobStatus(state.selectedJob)}</span>
                </div>
              </div>

              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Submitted At</h6>
                <p className={styles.sectionValue}>
                  {state.selectedJob.submitted_at
                    ? new Date(state.selectedJob.submitted_at).toLocaleString('en-US', {
                        month: 'short',
                        day: 'numeric',
                        year: 'numeric',
                        hour: 'numeric',
                        minute: '2-digit',
                        second: '2-digit',
                        hour12: true,
                      })
                    : 'N/A'}
                </p>
              </div>

              {state.selectedJob.completed_at && (
                <div className={styles.sidePanelSection}>
                  <h6 className={styles.sectionLabel}>Completed At</h6>
                  <p className={styles.sectionValue}>
                    {new Date(state.selectedJob.completed_at).toLocaleString('en-US', {
                      month: 'short',
                      day: 'numeric',
                      year: 'numeric',
                      hour: 'numeric',
                      minute: '2-digit',
                      second: '2-digit',
                      hour12: true,
                    })}
                  </p>
                </div>
              )}

              {state.selectedJob.stats && (
                <div className={styles.sidePanelSection}>
                  <h6 className={styles.sectionLabel}>Statistics</h6>
                  <div className={styles.statsGrid}>
                    <div className={styles.statItem}>
                      <span className={styles.statLabel}>Total Documents:</span>
                      <span className={styles.statValue}>{state.selectedJob.stats.total_documents}</span>
                    </div>
                    <div className={styles.statItem}>
                      <span className={styles.statLabel}>Completed:</span>
                      <span className={styles.statValue}>{state.selectedJob.stats.completed}</span>
                    </div>
                    <div className={styles.statItem}>
                      <span className={styles.statLabel}>Failed:</span>
                      <span className={styles.statValue}>{state.selectedJob.stats.failed}</span>
                    </div>
                    <div className={styles.statItem}>
                      <span className={styles.statLabel}>In Progress:</span>
                      <span className={styles.statValue}>{state.selectedJob.stats.in_progress}</span>
                    </div>
                  </div>
                </div>
              )}

              <div className={styles.sidePanelSection}>
                <h6 className={styles.sectionLabel}>Documents</h6>
                {state.selectedJob.documents && state.selectedJob.documents.length > 0 ? (
                  <div className={styles.documentsList}>
                    {state.selectedJob.documents.map((doc, idx) => (
                      <div key={idx} className={styles.documentItem}>
                        <div className={styles.documentInfo}>
                          <span className={styles.documentName}>{doc.name}</span>
                          <div className={styles.documentStatus}>
                            {getStatusIcon(doc.status)}
                            <span className={styles.statusText}>{doc.status}</span>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <p className={styles.noDocuments}>No documents</p>
                )}
              </div>

              {state.selectedJob.error && (
                <div className={styles.sidePanelSection}>
                  <h6 className={styles.sectionLabel}>Error</h6>
                  <p className={styles.errorText}>{state.selectedJob.error}</p>
                </div>
              )}
            </div>
          )}
        </SidePanel>

        {/* Delete Confirmation Modal */}
        <Modal
          open={state.showDeleteModal}
          danger
          size="sm"
          modalLabel="Delete Job"
          modalHeading="Confirm delete"
          primaryButtonText="Delete"
          secondaryButtonText="Cancel"
          primaryButtonDisabled={!state.isConfirmed}
          onRequestClose={() => dispatch({ type: 'CLOSE_DELETE_MODAL' })}
          onRequestSubmit={handleDeleteConfirm}
        >
          <p>
            Deleting a job permanently removes it from the system. This action cannot be undone.
          </p>
          <div>
            <CheckboxGroup
              legendText="Confirm job to be deleted"
            >
              <Checkbox
                id="checkbox-delete-job"
                labelText={
                  <strong>
                    {state.jobToDelete
                      ? getJobName(state.jobs.find((j) => j.job_id === state.jobToDelete)!)
                      : ''}
                  </strong>
                }
                checked={state.isConfirmed}
                onChange={(_, { checked }) =>
                  dispatch({
                    type: 'SET_CONFIRMED',
                    payload: checked,
                  })
                }
              />
            </CheckboxGroup>
          </div>
        </Modal>

        {/* Export CSV Modal */}
        <Modal
          open={state.isExportDialogOpen}
          size="sm"
          modalHeading="Export as CSV"
          passiveModal={state.exportStatus !== 'idle'}
          preventCloseOnClickOutside
          {...(state.exportStatus === 'idle' && {
            primaryButtonText: 'Export',
            secondaryButtonText: 'Cancel',
            onRequestSubmit: handleExportCSV,
          })}
          onRequestClose={() => dispatch({ type: 'CLOSE_EXPORT_DIALOG' })}
        >
          {state.exportStatus === 'idle' && (
            <TextInput
              id="csv-file-name"
              labelText="File name"
              value={state.csvFileName}
              invalid={!!state.exportErrorMessage}
              invalidText={state.exportErrorMessage}
              onChange={(e) => {
                dispatch({
                  type: 'SET_CSV_FILENAME',
                  payload: e.target.value,
                });
                dispatch({ type: 'CLEAR_EXPORT_ERROR' });
              }}
            />
          )}

          {state.exportStatus === 'exporting' && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <InlineLoading status="active" description="Exporting..." />
            </div>
          )}

          {state.exportStatus === 'success' && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <InlineLoading
                status="finished"
                description="The file has been exported"
              />
            </div>
          )}

          {state.exportStatus === 'error' && (
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <InlineLoading
                status="error"
                description={state.exportErrorMessage}
              />
            </div>
          )}
        </Modal>
      </div>
    </Theme>
  );
};

export default JobMonitorPage;

// Made with Bob
