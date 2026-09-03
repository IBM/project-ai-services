export const COMPONENT_TYPES = {
  LLM: "llm",
  RERANKER: "reranker",
  EMBEDDING: "embedding",
  VECTOR_STORE: "vector_store",
} as const;

export type ComponentType =
  (typeof COMPONENT_TYPES)[keyof typeof COMPONENT_TYPES];

// description and disabled will be used by the other worker PRs
export const WORKER_RUNTIME_LABELS: Record<
  string,
  { label: string; description: string; disabled?: boolean }
> = {
  podman: {
    label: "Red Hat Enterprise Linux (RHAIIS)",
    description:
      "This mode deploys all services across multiple worker resources with standard or common resource allocation; and runs on the premises of the client, rather than at a remote facility.",
  },
  openshift: {
    label: "Red Hat OpenShift (RHOAI)",
    description:
      "This mode deploys all services into a single worker resource, with additional resource requirements; and runs on the premises of the client, rather than at a remote facility.",
  },
  powervs: {
    label: "IBM Power Virtual Server (PowerVS)",
    description: "Deploy on public cloud infrastructure with managed services.",
    disabled: true,
  },
} as const;
