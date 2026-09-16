// Job status constants (matching backend enum values)
export const JOB_STATUS = {
  ACCEPTED: 'accepted',
  IN_PROGRESS: 'in_progress',
  COMPLETED: 'completed',
  COMPLETED_WITH_ERRORS: 'completed_with_errors',
  FAILED: 'failed',
  CANCEL_PENDING: 'cancel_pending',
  CANCELLED: 'cancelled',
} as const;

// Display status constants
export const DISPLAY_STATUS = {
  ACCEPTED: 'accepted',
  INGESTED: 'ingested',
  DIGITIZED: 'digitized',
  COMPLETED_WITH_ERRORS: 'completed_with_errors',
  INGESTION_ERROR: 'ingestion error',
  DIGITIZATION_ERROR: 'digitization error',
  INGESTING: 'ingesting...',
  DIGITIZING: 'digitizing...',
  CANCEL_PENDING: 'cancelling...',
  CANCELLED: 'cancelled',
} as const;

// Document status constants (matching backend DocStatus enum values)
export const DOC_STATUS = {
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