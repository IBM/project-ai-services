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

// The worker name used by the local (same-node) deployment target.
export const LOCAL_WORKER_NAME = "Local";

// The default runtime used when no worker runtime is known yet.
export const DEFAULT_RUNTIME = "podman" as const;

export const WORKER_RUNTIME_LABELS: Record<
  string,
  { short: string; label: string; description: string }
> = {
  podman: {
    short: "RHEL",
    label: "Red Hat Enterprise Linux (RHEL)",
    description:
      "Deploys services as standalone containers directly on a RHEL host using Podman. Ideal for lightweight, single-host deployments.",
  },
  openshift: {
    short: "OCP",
    label: "Red Hat OpenShift (OCP)",
    description:
      "Deploys services as managed workloads on a Red Hat OpenShift cluster. Ideal for Kubernetes-orchestrated, scalable enterprise environments.",
  },
} as const;
