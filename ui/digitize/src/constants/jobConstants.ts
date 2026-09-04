// Job status constants (matching backend enum values)
export const JOB_STATUS = {
  ACCEPTED: 'accepted',
  IN_PROGRESS: 'in_progress',
  COMPLETED: 'completed',
  FAILED: 'failed',
  CANCEL_PENDING: 'cancel_pending',
  CANCELLED: 'cancelled',
} as const;

// Display status constants
export const DISPLAY_STATUS = {
  ACCEPTED: 'Accepted',
  INGESTED: 'Ingested',
  DIGITIZED: 'Digitized',
  INGESTION_ERROR: 'Ingestion error',
  DIGITIZATION_ERROR: 'Digitization error',
  INGESTING: 'Ingesting...',
  DIGITIZING: 'Digitizing...',
  CANCEL_PENDING: 'Cancelling...',
  CANCELLED: 'Cancelled',
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