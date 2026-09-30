// Job status constants.
// Wire values (from backend JobStatus enum) plus UI-only display labels derived
// from job.status + job.operation in getJobStatus().
export const JOB_STATUS = {
  // --- wire values ---
  ACCEPTED: 'accepted',
  IN_PROGRESS: 'in_progress',
  COMPLETED: 'completed',
  COMPLETED_WITH_ERRORS: 'completed_with_errors',
  FAILED: 'failed',
  CANCEL_PENDING: 'cancel_pending',
  CANCELLED: 'cancelled',
  INGESTED: 'ingested',
  DIGITIZED: 'digitized',
  INGESTION_ERROR: 'ingestion error',
  DIGITIZATION_ERROR: 'digitization error',
  INGESTING: 'ingesting...',
  DIGITIZING: 'digitizing...',
  CANCELLING: 'cancelling...',
} as const;

// Document status constants — mirrors backend DocStatus enum wire values
export const DOC_STATUS = {
  ACCEPTED: 'accepted',
  IN_PROGRESS: 'in_progress',
  DIGITIZED: 'digitized',
  PROCESSED: 'processed',
  CHUNKED: 'chunked',
  COMPLETED: 'completed',
  COMPLETED_WITH_ERRORS: 'completed_with_errors',
  FAILED: 'failed',
  ALREADY_EXISTS: 'already_exists',
  CANCELLED: 'cancelled',
} as const;

// Job operation types
export const JOB_OPERATION = {
  INGESTION: 'ingestion',
  DIGITIZATION: 'digitization',
} as const;

// Job type display names
export const JOB_TYPE_DISPLAY = {
  INGESTION: 'Ingestion',
  DIGITIZATION: 'Digitization only',
} as const;

// Made with Bob