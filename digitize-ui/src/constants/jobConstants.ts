// Job status constants
export const JOB_STATUS = {
  COMPLETED: 'completed',
  FAILED: 'failed',
  IN_PROGRESS: 'in_progress',
} as const;

// Display status constants
export const DISPLAY_STATUS = {
  INGESTED: 'Ingested',
  DIGITIZED: 'Digitized',
  INGESTION_ERROR: 'Ingestion error',
  DIGITIZATION_ERROR: 'Digitization error',
  INGESTING: 'Ingesting...',
  DIGITIZING: 'Digitizing...',
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