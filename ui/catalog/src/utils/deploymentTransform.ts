import type {
  DeployFormData,
  ServiceConfig,
} from "@/components/DeployFlow/types";
import type {
  DeployOptionsResponse,
  Service,
  Component,
  Provider,
} from "@/types/digitalAssistants";
import { fetchProviderParams } from "@/api/digitalAssistants";
import { useDeployStore } from "@/store/deploy.store";
import { SERVICE_ID_MAP } from "@/constants/services.constants";

interface DeploymentComponent {
  component_type: string;
  provider_id: string;
  version: string;
  params?: {
    model?: string;
    watsonx_project_id?: string;
    watsonx_api_endpoint?: string;
    watsonx_api_key?: string;
    system_prompt?: string;
    [key: string]: unknown;
  };
}

interface DeploymentService {
  catalog_id: string;
  version: string;
  components: DeploymentComponent[];
}

export interface DeploymentPayload {
  name: string;
  catalog_id: string;
  version: string;
  services: DeploymentService[];
}

/**
 * Fetches provider schema and extracts parameters with their defaults
 * Uses Zustand cache to avoid redundant API calls
 */
async function fetchProviderSchema(
  componentType: string,
  providerId: string,
): Promise<Record<string, unknown>> {
  // Check cache first
  const cachedParams = useDeployStore
    .getState()
    .getProviderParams(componentType, providerId);

  if (cachedParams) {
    // Extract defaults from cached response
    const params: Record<string, unknown> = {};
    const properties = cachedParams?.properties as
      | Record<string, { default?: unknown }>
      | undefined;

    if (properties) {
      for (const [key, property] of Object.entries(properties)) {
        if (property.default !== undefined) {
          params[key] = property.default;
        }
      }
    }

    return params;
  }

  // Cache miss - fetch from API
  try {
    const response = await fetchProviderParams(componentType, providerId);

    // Store in cache for future use
    useDeployStore
      .getState()
      .setProviderParams(componentType, providerId, response);

    const params: Record<string, unknown> = {};

    // Extract all properties with default values from schema
    if (response?.properties) {
      for (const [key, property] of Object.entries(response.properties)) {
        if (property.default !== undefined) {
          params[key] = property.default;
        }
      }
    }

    return params;
  } catch {
    console.warn(`Failed to fetch schema for ${componentType}/${providerId}`);
    return {};
  }
}

/**
 * Builds an embedding component with schema defaults
 */
async function buildEmbeddingComponent(
  embeddingModel: string,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
  schemaPromise: Promise<Record<string, unknown>>,
): Promise<DeploymentComponent> {
  const schemaParams = await schemaPromise;

  return {
    component_type: "embedding",
    provider_id: embeddingModel,
    version: getProviderVersion(
      "embedding",
      embeddingModel,
      serviceDefinition,
      deployOptions,
    ),
    ...(Object.keys(schemaParams).length > 0 && {
      params: schemaParams,
    }),
  };
}

/**
 * Builds a reranker component with schema defaults and VLLM API key
 */
async function buildRerankerComponent(
  rerankerModel: string,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
  schemaPromise: Promise<Record<string, unknown>>,
  serviceConfig: ServiceConfig,
): Promise<DeploymentComponent> {
  const schemaParams = await schemaPromise;

  // Prepare user-provided values
  const userValues: Record<string, unknown> = {};

  // VLLM API key for reranker providers (provider-specific)
  if (rerankerModel === "vllm-cpu" && serviceConfig.vllmCpuApiKey) {
    userValues.apiKey = serviceConfig.vllmCpuApiKey;
  } else if (rerankerModel === "vllm-spyre" && serviceConfig.vllmSpyreApiKey) {
    userValues.apiKey = serviceConfig.vllmSpyreApiKey;
  }

  // Merge schema defaults with user values
  const params = mergeParamsWithUserValues(schemaParams, userValues);

  return {
    component_type: "reranker",
    provider_id: rerankerModel,
    version: getProviderVersion(
      "reranker",
      rerankerModel,
      serviceDefinition,
      deployOptions,
    ),
    ...(Object.keys(params).length > 0 && { params }),
  };
}

/**
 * Builds a vector store component
 */
function buildVectorStoreComponent(
  vectorStore: string,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
): DeploymentComponent {
  return {
    component_type: "vector_store",
    provider_id: vectorStore,
    version: getProviderVersion(
      "vector_store",
      vectorStore,
      serviceDefinition,
      deployOptions,
    ),
  };
}

/**
 * Builds an LLM component with watsonx credentials merged with schema defaults
 */
async function buildLLMComponent(
  llmProviderId: string,
  serviceConfig: ServiceConfig,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
  schemaPromise: Promise<Record<string, unknown>>,
  includeSystemPrompt: boolean = false,
): Promise<DeploymentComponent> {
  const schemaParams = await schemaPromise;

  // Prepare user-provided values
  const userValues: Record<string, unknown> = {};
  if (serviceConfig.watsonxProjectId)
    userValues.watsonxProjectId = serviceConfig.watsonxProjectId;
  if (serviceConfig.watsonxApiEndpoint)
    userValues.watsonxUrl = serviceConfig.watsonxApiEndpoint;
  if (serviceConfig.watsonxApiKey)
    userValues.watsonxApiKey = serviceConfig.watsonxApiKey;

  // VLLM API key for LLM providers (provider-specific)
  if (llmProviderId === "vllm-cpu" && serviceConfig.vllmCpuApiKey) {
    userValues.apiKey = serviceConfig.vllmCpuApiKey;
  } else if (llmProviderId === "vllm-spyre" && serviceConfig.vllmSpyreApiKey) {
    userValues.apiKey = serviceConfig.vllmSpyreApiKey;
  }

  if (
    includeSystemPrompt &&
    serviceConfig.editSystemPrompt &&
    serviceConfig.systemPromptText
  ) {
    userValues.system_prompt = serviceConfig.systemPromptText;
  }

  // Merge schema defaults with user values
  const params = mergeParamsWithUserValues(schemaParams, userValues);

  return {
    component_type: "llm",
    provider_id: llmProviderId,
    version: getProviderVersion(
      "llm",
      llmProviderId,
      serviceDefinition,
      deployOptions,
    ),
    ...(Object.keys(params).length > 0 && { params }),
  };
}

/**
 * Gets the provider version from the API response
 * Searches service-specific components first, then falls back to global components
 */
function getProviderVersion(
  componentType: string,
  providerId: string,
  serviceDefinition: Service | undefined,
  deployOptions: DeployOptionsResponse,
): string {
  // First, try to find in service-specific components
  if (serviceDefinition) {
    const component = serviceDefinition.components.find(
      (c: Component) => c.type === componentType,
    );
    const provider = component?.providers.find(
      (p: Provider) => p.id === providerId,
    );
    if (provider?.version) {
      return provider.version;
    }
  }

  // Fall back to global components (e.g., for vector_store)
  const globalComponent = deployOptions.global_components.find(
    (c: Component) => c.type === componentType,
  );
  const globalProvider = globalComponent?.providers.find(
    (p: Provider) => p.id === providerId,
  );
  if (globalProvider?.version) {
    return globalProvider.version;
  }

  // Final fallback
  return "1.0.0";
}

/**
 * Merges user-provided values with schema defaults
 * User values take precedence over defaults
 */
function mergeParamsWithUserValues(
  schemaParams: Record<string, unknown>,
  userValues: Record<string, unknown>,
): Record<string, unknown> {
  const merged = { ...schemaParams };

  // Override with user-provided values (non-empty strings, non-null values)
  for (const [key, value] of Object.entries(userValues)) {
    if (value !== undefined && value !== null && value !== "") {
      merged[key] = value;
    }
  }

  return merged;
}

/**
 * Transforms form data into deployment payload format
 */
export async function transformToDeploymentPayload(
  formData: DeployFormData,
  deployOptions: DeployOptionsResponse,
): Promise<DeploymentPayload> {
  const services: DeploymentService[] = [];

  // Collect all unique provider/component combinations to fetch in parallel
  const schemaFetchPromises = new Map<
    string,
    Promise<Record<string, unknown>>
  >();

  const getSchemaPromise = (componentType: string, providerId: string) => {
    const key = `${componentType}:${providerId}`;
    if (!schemaFetchPromises.has(key)) {
      schemaFetchPromises.set(
        key,
        fetchProviderSchema(componentType, providerId),
      );
    }
    return schemaFetchPromises.get(key)!;
  };

  // Process each enabled service
  for (const [serviceKey, serviceConfig] of Object.entries(formData.services)) {
    if (!serviceConfig.enabled) continue;

    const catalogId = SERVICE_ID_MAP[serviceKey];
    if (!catalogId) continue;

    // Find the service definition in deploy options
    const serviceDefinition = deployOptions.services.find(
      (s) => s.id === catalogId,
    );
    if (!serviceDefinition) continue;

    const componentPromises: Promise<DeploymentComponent | null>[] = [];

    // Service-specific component logic based on catalog_id
    // Order matters: components are added in the expected order
    switch (catalogId) {
      case "digitize":
        // digitize: llm, embedding, vector_store (NO reranker)
        if (serviceConfig.llm) {
          componentPromises.push(
            buildLLMComponent(
              serviceConfig.llm,
              serviceConfig,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("llm", serviceConfig.llm),
              false,
            ),
          );
        }
        if (serviceConfig.embeddingModel) {
          componentPromises.push(
            buildEmbeddingComponent(
              serviceConfig.embeddingModel,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("embedding", serviceConfig.embeddingModel),
            ),
          );
        }
        if (formData.vectorStore) {
          componentPromises.push(
            Promise.resolve(
              buildVectorStoreComponent(
                formData.vectorStore,
                serviceDefinition,
                deployOptions,
              ),
            ),
          );
        }
        break;

      case "chat":
        // chat: llm, vector_store, embedding, reranker
        if (serviceConfig.llm) {
          componentPromises.push(
            buildLLMComponent(
              serviceConfig.llm,
              serviceConfig,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("llm", serviceConfig.llm),
              true, // Include system prompt for chat
            ),
          );
        }
        if (formData.vectorStore) {
          componentPromises.push(
            Promise.resolve(
              buildVectorStoreComponent(
                formData.vectorStore,
                serviceDefinition,
                deployOptions,
              ),
            ),
          );
        }
        if (serviceConfig.embeddingModel) {
          componentPromises.push(
            buildEmbeddingComponent(
              serviceConfig.embeddingModel,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("embedding", serviceConfig.embeddingModel),
            ),
          );
        }
        if (serviceConfig.rerankerModel) {
          componentPromises.push(
            buildRerankerComponent(
              serviceConfig.rerankerModel,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("reranker", serviceConfig.rerankerModel),
              serviceConfig,
            ),
          );
        }
        break;

      case "summarize":
        // summarize: llm only (NO vector_store, NO embedding, NO reranker)
        if (serviceConfig.llm) {
          componentPromises.push(
            buildLLMComponent(
              serviceConfig.llm,
              serviceConfig,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("llm", serviceConfig.llm),
              false,
            ),
          );
        }
        break;

      case "similarity":
        // similarity: vector_store, embedding, reranker (NO llm)
        if (formData.vectorStore) {
          componentPromises.push(
            Promise.resolve(
              buildVectorStoreComponent(
                formData.vectorStore,
                serviceDefinition,
                deployOptions,
              ),
            ),
          );
        }
        if (serviceConfig.embeddingModel) {
          componentPromises.push(
            buildEmbeddingComponent(
              serviceConfig.embeddingModel,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("embedding", serviceConfig.embeddingModel),
            ),
          );
        }
        if (serviceConfig.rerankerModel) {
          componentPromises.push(
            buildRerankerComponent(
              serviceConfig.rerankerModel,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("reranker", serviceConfig.rerankerModel),
              serviceConfig,
            ),
          );
        }
        break;

      default:
        // Fallback: include all available components
        if (serviceConfig.llm) {
          componentPromises.push(
            buildLLMComponent(
              serviceConfig.llm,
              serviceConfig,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("llm", serviceConfig.llm),
              false,
            ),
          );
        }
        if (formData.vectorStore) {
          componentPromises.push(
            Promise.resolve(
              buildVectorStoreComponent(
                formData.vectorStore,
                serviceDefinition,
                deployOptions,
              ),
            ),
          );
        }
        if (serviceConfig.embeddingModel) {
          componentPromises.push(
            buildEmbeddingComponent(
              serviceConfig.embeddingModel,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("embedding", serviceConfig.embeddingModel),
            ),
          );
        }
        if (serviceConfig.rerankerModel) {
          componentPromises.push(
            buildRerankerComponent(
              serviceConfig.rerankerModel,
              serviceDefinition,
              deployOptions,
              getSchemaPromise("reranker", serviceConfig.rerankerModel),
              serviceConfig,
            ),
          );
        }
        break;
    }

    // Wait for all components of this service to be ready
    const components = (await Promise.all(componentPromises)).filter(
      (c): c is DeploymentComponent => c !== null,
    );

    services.push({
      catalog_id: catalogId,
      version: serviceConfig.serviceVersion || formData.version,
      components,
    });
  }

  return {
    name: formData.name,
    catalog_id: deployOptions.id,
    version: formData.version,
    services,
  };
}
