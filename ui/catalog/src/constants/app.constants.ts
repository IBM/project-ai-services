// runtime mapping for the workers table
export const RUNTIME_TYPE_LABELS: Record<string, string> = {
  podman: "RHAIIS",
  openshift: "RHOAI",
};

export const COMPONENT_TYPES = {
  LLM: "llm",
  RERANKER: "reranker",
  EMBEDDING: "embedding",
  VECTOR_STORE: "vector_store",
} as const;

export type ComponentType =
  (typeof COMPONENT_TYPES)[keyof typeof COMPONENT_TYPES];
