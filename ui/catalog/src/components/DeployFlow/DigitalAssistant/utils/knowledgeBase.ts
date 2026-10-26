import type {
  DeployOptionsComponent,
  DeployOptionsResponse,
} from "@/types/api.types";

/**
 * Returns all components that belong to the Knowledge Base section:
 * architecture-level global components (vector_store, embedding).
 */
export function getKnowledgeBaseComponents(
  deployOptions: DeployOptionsResponse,
): DeployOptionsComponent[] {
  return deployOptions.global_components;
}

/**
 * Determines whether a given component type is managed in the Knowledge Base
 * section (and should therefore be excluded from individual service cards).
 */
export function isKnowledgeBaseComponent(
  componentType: string,
  deployOptions: DeployOptionsResponse,
): boolean {
  return deployOptions.global_components.some(
    (gc) => gc.type === componentType,
  );
}
