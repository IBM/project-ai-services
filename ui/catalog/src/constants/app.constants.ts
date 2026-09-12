export const APP_NAME = "IBM Power AI Launchpad";

export const COMPONENT_TYPES = {
  LLM: "llm",
  RERANKER: "reranker",
  EMBEDDING: "embedding",
  VECTOR_STORE: "vector_store",
} as const;

export type ComponentType =
  (typeof COMPONENT_TYPES)[keyof typeof COMPONENT_TYPES];

// The worker name used by the local (same-node) deployment target.
export const LOCAL_WORKER_NAME = "Local";

// The default runtime used when no worker runtime is known yet.
export const DEFAULT_RUNTIME = "podman" as const;

export const WORKER_RUNTIME_LABELS: Record<
  string,
  { short: string; label: string; description: string; disabled?: boolean }
> = {
  podman: {
    short: "RHAIIS",
    label: "Red Hat Enterprise Linux (RHAIIS)",
    description:
      "This mode deploys all services across multiple worker resources with standard or common resource allocation; and runs on the premises of the client, rather than at a remote facility.",
  },
  openshift: {
    short: "RHOAI",
    label: "Red Hat OpenShift (RHOAI)",
    description:
      "This mode deploys all services into a single worker resource, with additional resource requirements; and runs on the premises of the client, rather than at a remote facility.",
  },
  powervs: {
    short: "PowerVS",
    label: "IBM Power Virtual Server (PowerVS)",
    description: "Deploy on public cloud infrastructure with managed services.",
    disabled: true,
  },
} as const;
