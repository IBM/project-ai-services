export const APP_NAME = "IBM Power AI Launchpad";

export const COMPONENT_TYPES = {
  LLM: "llm",
  RERANKER: "reranker",
  EMBEDDING: "embedding",
  VECTOR_STORE: "vector_store",
} as const;

export type ComponentType =
  (typeof COMPONENT_TYPES)[keyof typeof COMPONENT_TYPES];
