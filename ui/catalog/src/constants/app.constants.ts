export const APP_NAME = "IBM Power AI Launchpad";

export const GENERIC_ERROR_MESSAGE =
  "An unexpected error occurred. Please try again.";

export const COMPONENT_TYPES = {
  LLM: "llm",
  RERANKER: "reranker",
  EMBEDDING: "embedding",
  VECTOR_STORE: "vector_store",
} as const;

export type ComponentType =
  (typeof COMPONENT_TYPES)[keyof typeof COMPONENT_TYPES];

export const DEPLOYMENT_STATUS = {
  INITIALIZING: "Initializing",
  DOWNLOADING: "Downloading",
  DEPLOYING: "Deploying",
  RUNNING: "Running",
  DELETING: "Deleting",
  STOPPED: "Stopped",
  ERROR: "Error",
} as const;

export type DeploymentStatus =
  (typeof DEPLOYMENT_STATUS)[keyof typeof DEPLOYMENT_STATUS];

export const WORKER_RUNTIME_TYPES = {
  UNKNOWN: "unknown",
  PODMAN: "podman",
  OPENSHIFT: "openshift",
} as const;

export type WorkerRuntimeType =
  (typeof WORKER_RUNTIME_TYPES)[keyof typeof WORKER_RUNTIME_TYPES];

// The worker name used by the local (same-node) deployment target.
export const LOCAL_WORKER_NAME = "Local";

// The default runtime used when no worker runtime is known yet.
export const DEFAULT_RUNTIME = WORKER_RUNTIME_TYPES.PODMAN;

export const WORKER_RUNTIME_LABELS: Record<
  string,
  { short: string; label: string; description: string; disabled?: boolean }
> = {
  [WORKER_RUNTIME_TYPES.PODMAN]: {
    short: "RHAIIS",
    label: "Red Hat Enterprise Linux (RHAIIS)",
    description:
      "This mode deploys all services across multiple worker resources with standard or common resource allocation; and runs on the premises of the client, rather than at a remote facility.",
  },
  [WORKER_RUNTIME_TYPES.OPENSHIFT]: {
    short: "RHOAI",
    label: "Red Hat OpenShift (RHOAI)",
    description:
      "This mode deploys all services into a single worker resource, with additional resource requirements; and runs on the premises of the client, rather than at a remote facility.",
  },
} as const;

export const CONNECTOR_STATUS = {
  CONNECTED: "connected",
  OFFLINE: "offline",
} as const;

export type ConnectorStatus =
  (typeof CONNECTOR_STATUS)[keyof typeof CONNECTOR_STATUS];

export const DATASOURCE_SYNC_STATUS = {
  UP_TO_DATE: "up to date",
  SYNCING: "syncing",
  OUT_OF_SYNC: "out of sync",
  DELETE_PENDING: "delete pending",
  UNKNOWN: "unknown",
} as const;

export type DatasourceSyncStatus =
  (typeof DATASOURCE_SYNC_STATUS)[keyof typeof DATASOURCE_SYNC_STATUS];

export const WORKER_STATUS = {
  PENDING: "pending",
  READY: "ready",
  DISCONNECTED: "disconnected",
} as const;

export type WorkerStatus = (typeof WORKER_STATUS)[keyof typeof WORKER_STATUS];

export const DEPLOYMENT_DETAILS_SECTIONS = {
  DETAILS: "details",
  SERVICES: "services",
  INTEGRATION: "integration",
  DATASOURCES: "datasources",
} as const;

export type DeploymentDetailsSection =
  (typeof DEPLOYMENT_DETAILS_SECTIONS)[keyof typeof DEPLOYMENT_DETAILS_SECTIONS];

export const DETAILS_PANEL_MODES = {
  VIEW: "view",
  UPDATE_KEY: "update-key",
} as const;

export type DetailsPanelMode =
  (typeof DETAILS_PANEL_MODES)[keyof typeof DETAILS_PANEL_MODES];
