import { useReducer, useMemo, useEffect, useCallback, useState } from "react";
import styles from "../DeployFlow.module.scss";
import type { StepProps, ServiceConfig } from "../types";
import {
  getServiceKey,
  getServiceIdFromKey,
  getAcceleratorLabel,
  getResourceStatus,
  bytesToGB,
  SHARED_BY_MODEL_PROVIDERS,
} from "../utils/StepTwo.utils";
import { ResourceRequirements } from "../components/ResourceRequirements";
import { ServiceConfigCard } from "../components/ServiceConfigCard";
import { fetchResources } from "@/api/digitalAssistants";
import { useBatchProviderParams } from "@/hooks/useProviderParams";
import type { ResourcesResponse } from "@/types/digitalAssistants";
import type {
  ResourceItem,
  StepTwoState,
  StepTwoAction,
} from "../types/StepTwo.types";

// Initial state
const INITIAL_STATE: StepTwoState = {
  editingService: null,
  tempConfig: null,
  showApiKey: {},
  llmModelNames: {},
  embeddingModelNames: {},
};

// Reducer function
const stepTwoReducer = (
  state: StepTwoState,
  action: StepTwoAction,
): StepTwoState => {
  switch (action.type) {
    case "SET_EDITING_SERVICE":
      return { ...state, editingService: action.payload };
    case "SET_TEMP_CONFIG":
      return { ...state, tempConfig: action.payload };
    case "UPDATE_TEMP_CONFIG":
      return {
        ...state,
        tempConfig: state.tempConfig
          ? { ...state.tempConfig, ...action.payload }
          : null,
      };
    case "TOGGLE_SHOW_API_KEY":
      return {
        ...state,
        showApiKey: {
          ...state.showApiKey,
          [action.payload]: !state.showApiKey[action.payload],
        },
      };
    case "SET_LLM_MODEL_NAMES":
      return { ...state, llmModelNames: action.payload };
    case "SET_EMBEDDING_MODEL_NAMES":
      return { ...state, embeddingModelNames: action.payload };
    case "RESET_EDITING":
      return {
        ...state,
        editingService: null,
        tempConfig: null,
      };
    default:
      return state;
  }
};

export const StepTwo: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  onEditingChange,
  onResourceStatusChange,
}) => {
  const [state, dispatch] = useReducer(stepTwoReducer, INITIAL_STATE);
  const [validationError, setValidationError] = useState<string | null>(null);

  // Fetch resources directly without caching
  const [resources, setResources] = useState<ResourcesResponse | null>(null);
  const [resourcesLoading, setResourcesLoading] = useState<boolean>(true);
  const [resourcesError, setResourcesError] = useState<string | null>(null);

  useEffect(() => {
    fetchResources()
      .then((data) => {
        setResources(data);
        setResourcesLoading(false);
      })
      .catch((err) => {
        const errorMessage =
          err instanceof Error ? err.message : "Failed to load resources";
        setResourcesError(errorMessage);
        setResourcesLoading(false);
      });
  }, []);

  // Calculate required resources based on selected services and providers
  const calculateRequiredResources = useMemo(() => {
    const uniqueProviders: Record<
      string,
      {
        cpu: number;
        memory: number;
        storage: number;
        accelerators: Record<string, number>;
      }
    > = {};

    Object.entries(formData.services).forEach(([serviceKey, serviceConfig]) => {
      if (!serviceConfig.enabled) return;

      const serviceId = getServiceIdFromKey(
        serviceKey as keyof typeof formData.services,
      );
      const service = deployOptions.services.find((s) => s.id === serviceId);

      if (!service) return;

      service.components.forEach((component) => {
        let selectedProviderId: string | undefined;
        let selectedModelId: string | undefined;

        if (component.type === "embedding") {
          selectedProviderId = serviceConfig.embeddingModel;
          selectedModelId = serviceConfig.embeddingModel;
        } else if (component.type === "reranker") {
          selectedProviderId = serviceConfig.rerankerModel;
          selectedModelId = serviceConfig.rerankerModel;
        } else if (component.type === "llm") {
          selectedProviderId =
            serviceConfig.inferenceMethod || serviceConfig.llm;
          selectedModelId = serviceConfig.llm;
        }

        if (!selectedProviderId) return;

        const provider = component.providers.find(
          (p) => p.id === selectedProviderId,
        );

        if (!provider?.resources) return;

        // Deduplication logic:
        // - For SHARED_BY_MODEL_PROVIDERS (vllm-cpu, vllm-spyre, watsonx):
        //   Same provider + same model = shared instance across services
        //   Use provider+model as unique key to deduplicate ALL resources
        // - For other providers:
        //   Each service component gets its own instance
        //   Use service+provider+component to ensure no deduplication
        const uniqueKey = SHARED_BY_MODEL_PROVIDERS.has(selectedProviderId)
          ? `${selectedProviderId}-${selectedModelId || "default"}`
          : `${serviceKey}-${selectedProviderId}-${component.type}`;

        if (!uniqueProviders[uniqueKey]) {
          uniqueProviders[uniqueKey] = {
            cpu: provider.resources.cpu || 0,
            memory: provider.resources.memory || 0,
            storage: provider.resources.storage || 0,
            accelerators: { ...(provider.resources.accelerators || {}) },
          };
        }
      });
    });

    let totalCPU = 0;
    let totalMemory = 0;
    let totalStorage = 0;
    const totalAccelerators: Record<string, number> = {};

    Object.values(uniqueProviders).forEach((resources) => {
      totalCPU += resources.cpu;
      totalMemory += resources.memory;
      totalStorage += resources.storage;

      Object.entries(resources.accelerators).forEach(([key, count]) => {
        totalAccelerators[key] = (totalAccelerators[key] || 0) + count;
      });
    });

    const memoryGB = Math.ceil(totalMemory / 1024 ** 3);
    const storageGB = Math.ceil(totalStorage / 1024 ** 3);

    return {
      cpu: totalCPU,
      memory: memoryGB,
      accelerators: totalAccelerators,
      storage: storageGB,
    };
  }, [formData, deployOptions.services]);

  // Format resources for display
  const resourceRequirements = useMemo((): ResourceItem[] => {
    if (!resources) {
      return [];
    }

    const resourceItems: ResourceItem[] = [];

    // 1. CPU (always present)
    resourceItems.push({
      label: "Processors",
      required: calculateRequiredResources.cpu.toString(),
      available: Math.floor(resources.cpu.available_cores).toString(),
      unit: "Cores",
      type: "cpu",
    });

    // 2. Memory (always present)
    resourceItems.push({
      label: "Memory",
      required: calculateRequiredResources.memory.toString(),
      available: bytesToGB(resources.memory.available_bytes).toString(),
      unit: "GB",
      type: "memory",
    });

    // 3. Accelerators (may be empty object or contain multiple types)
    const acceleratorKeys = Object.keys(resources.accelerators);
    const totalRequired = Object.values(
      calculateRequiredResources.accelerators,
    ).reduce((sum, val) => sum + val, 0);

    if (acceleratorKeys.length > 0) {
      // Handle each accelerator type separately
      acceleratorKeys.forEach((acceleratorKey) => {
        const acceleratorData = resources.accelerators[acceleratorKey];
        const acceleratorLabel = getAcceleratorLabel(acceleratorKey);
        const requiredCount =
          calculateRequiredResources.accelerators[acceleratorKey] || 0;

        resourceItems.push({
          label: acceleratorLabel,
          required: requiredCount.toString(),
          available: acceleratorData.available.toString(),
          unit: "Cards",
          type: "accelerator",
          acceleratorType: acceleratorKey,
        });
      });
    } else {
      // No accelerators available in system - always show with 0 available
      resourceItems.push({
        label: "Accelerators",
        required: totalRequired.toString(),
        available: "0",
        unit: "Cards",
        type: "accelerator",
      });
    }

    // 4. Storage (not provided by API, show required only)
    if (calculateRequiredResources.storage > 0) {
      resourceItems.push({
        label: "Disk storage",
        required: calculateRequiredResources.storage.toString(),
        available: "N/A",
        unit: "GB",
        type: "storage",
      });
    }

    return resourceItems;
  }, [resources, calculateRequiredResources]);

  // Check for insufficient resources and notify parent
  useEffect(() => {
    if (!resourcesLoading && !resourcesError && resources) {
      const hasInsufficientResources = resourceRequirements.some((resource) => {
        const status = getResourceStatus(resource.required, resource.available);
        return status === "insufficient";
      });
      onResourceStatusChange?.(hasInsufficientResources);
    } else {
      // If resources are loading, in error state, or not available, consider it as insufficient
      onResourceStatusChange?.(true);
    }
  }, [
    resourceRequirements,
    resourcesLoading,
    resourcesError,
    resources,
    onResourceStatusChange,
  ]);

  // Extract service version options from API response
  const serviceVersionOptions = [
    { id: deployOptions.version, text: deployOptions.version },
  ];

  // Helper function to get component providers by type from a service
  const getComponentProviders = useCallback(
    (
      serviceId: string,
      componentType: string,
    ): Array<{ id: string; text: string }> => {
      const service = deployOptions.services.find((s) => s.id === serviceId);
      if (!service) return [];

      const component = service.components.find(
        (c) => c.type === componentType,
      );
      return (
        component?.providers.map((provider) => ({
          id: provider.id,
          text: provider.name,
        })) || []
      );
    },
    [deployOptions.services],
  );

  // Get all embedding provider IDs
  const embeddingProviders = useMemo(
    () => getComponentProviders("chat", "embedding"),
    [getComponentProviders],
  );
  const embeddingProviderIds = useMemo(
    () => embeddingProviders.map((p) => p.id),
    [embeddingProviders],
  );

  // Fetch embedding params for all providers (cached)
  const { paramsMap: embeddingParamsMap } = useBatchProviderParams(
    "embedding",
    embeddingProviderIds,
  );

  // Extract embedding model names from cached params
  useEffect(() => {
    const modelNamesMap: Record<string, string> = {};

    for (const [providerId, params] of Object.entries(embeddingParamsMap)) {
      if (
        params &&
        typeof params === "object" &&
        "properties" in params &&
        params.properties &&
        typeof params.properties === "object"
      ) {
        const properties = params.properties as Record<
          string,
          { default?: unknown; oneOf?: Array<{ title?: string }> }
        >;
        const defaultModel = properties.model?.default;
        // Try to get the title from oneOf if available
        const modelTitle = properties.model?.oneOf?.[0]?.title;
        if (modelTitle && typeof modelTitle === "string") {
          modelNamesMap[providerId] = modelTitle;
        } else if (defaultModel && typeof defaultModel === "string") {
          // Fallback to default model path, extract just the model name
          const modelName = defaultModel.split("/").pop() || defaultModel;
          modelNamesMap[providerId] = modelName;
        }
      }
    }

    if (Object.keys(modelNamesMap).length > 0) {
      const hasChanges = Object.keys(modelNamesMap).some(
        (key) => state.embeddingModelNames[key] !== modelNamesMap[key],
      );

      if (hasChanges || Object.keys(state.embeddingModelNames).length === 0) {
        dispatch({
          type: "SET_EMBEDDING_MODEL_NAMES",
          payload: modelNamesMap,
        });
      }
    }
  }, [embeddingParamsMap, state.embeddingModelNames]);

  // Get embedding model options with model names
  const embeddingModelOptions = useMemo(() => {
    const providers = getComponentProviders("chat", "embedding");
    return providers.map((provider) => ({
      id: provider.id,
      text: state.embeddingModelNames[provider.id] || provider.text,
    }));
  }, [state.embeddingModelNames, getComponentProviders]);

  // Get reranker model options from services
  const rerankerModelOptions = getComponentProviders("chat", "reranker");

  // Get LLM options from services with model names (deduplicated)
  const llmOptions = useMemo(() => {
    const providers = getComponentProviders("chat", "llm");
    const seenModels = new Set<string>();
    const uniqueOptions: Array<{ id: string; text: string }> = [];

    providers.forEach((provider) => {
      const modelName = state.llmModelNames[provider.id] || provider.text;

      // Only add if we haven't seen this model name before
      if (!seenModels.has(modelName)) {
        seenModels.add(modelName);
        uniqueOptions.push({
          id: provider.id,
          text: modelName,
        });
      }
    });

    return uniqueOptions;
  }, [state.llmModelNames, getComponentProviders]);

  // Get inference method options from API
  const inferenceMethodOptions = getComponentProviders("chat", "llm").map(
    (provider) => ({
      id: provider.id,
      text: provider.text,
    }),
  );

  // Get all LLM provider IDs
  const llmProviders = useMemo(
    () => getComponentProviders("chat", "llm"),
    [getComponentProviders],
  );
  const llmProviderIds = useMemo(
    () => llmProviders.map((p) => p.id),
    [llmProviders],
  );

  // Fetch LLM params for all providers (cached)
  const { paramsMap } = useBatchProviderParams("llm", llmProviderIds);

  // Extract model names from cached params
  useEffect(() => {
    const modelNamesMap: Record<string, string> = {};

    for (const [providerId, params] of Object.entries(paramsMap)) {
      // Check if params has the expected structure
      if (
        params &&
        typeof params === "object" &&
        "properties" in params &&
        params.properties &&
        typeof params.properties === "object"
      ) {
        const properties = params.properties as Record<
          string,
          { default?: unknown }
        >;
        const defaultModel = properties.model?.default;
        if (defaultModel && typeof defaultModel === "string") {
          modelNamesMap[providerId] = defaultModel;
        }
      }
    }

    // Only dispatch if we have new model names and they differ from current state
    if (Object.keys(modelNamesMap).length > 0) {
      const hasChanges = Object.keys(modelNamesMap).some(
        (key) => state.llmModelNames[key] !== modelNamesMap[key],
      );

      if (hasChanges || Object.keys(state.llmModelNames).length === 0) {
        dispatch({ type: "SET_LLM_MODEL_NAMES", payload: modelNamesMap });
      }
    }
  }, [paramsMap, state.llmModelNames]);

  // Get vector store options from global_components
  const vectorStoreComponent = deployOptions.global_components?.find(
    (c) => c.type === "vector_store",
  );
  const vectorStoreOptions =
    vectorStoreComponent?.providers.map((provider) => ({
      id: provider.id,
      text: provider.name,
    })) || [];

  const handleEdit = (serviceName: string) => {
    const serviceKey = getServiceKey(serviceName);
    const config =
      formData.services[serviceKey as keyof typeof formData.services];
    setValidationError(null);
    dispatch({ type: "SET_TEMP_CONFIG", payload: { ...config } });
    dispatch({ type: "SET_EDITING_SERVICE", payload: serviceName });
    onEditingChange?.(true);
  };

  const handleApply = (serviceName: string) => {
    if (!state.tempConfig) {
      return;
    }

    const requiresWatsonxCredentials =
      state.tempConfig.inferenceMethod === "watsonx";
    const missingWatsonxFields = [
      !state.tempConfig.watsonxProjectId?.trim() ? "Project ID" : null,
      !state.tempConfig.watsonxApiEndpoint?.trim() ? "API endpoint" : null,
      !state.tempConfig.watsonxApiKey?.trim() ? "API key" : null,
    ].filter(Boolean) as string[];

    const requiresPrompt =
      serviceName === "Question and answer" &&
      state.tempConfig.editSystemPrompt;
    const isPromptMissing =
      requiresPrompt && !state.tempConfig.systemPromptText?.trim();

    if (requiresWatsonxCredentials && missingWatsonxFields.length > 0) {
      setValidationError(
        `Provide required watsonx fields: ${missingWatsonxFields.join(", ")}.`,
      );
      return;
    }

    if (isPromptMissing) {
      setValidationError(
        "Prompt text is required when system prompt is enabled.",
      );
      return;
    }

    setValidationError(null);
    const serviceKey = getServiceKey(serviceName);
    onChange({
      services: {
        ...formData.services,
        [serviceKey]: state.tempConfig,
      },
    });
    dispatch({ type: "RESET_EDITING" });
    onEditingChange?.(false);
  };

  const handleCancel = () => {
    setValidationError(null);
    dispatch({ type: "RESET_EDITING" });
    onEditingChange?.(false);
  };

  const updateTempConfig = (updates: Partial<ServiceConfig>) => {
    if (validationError) {
      setValidationError(null);
    }
    dispatch({ type: "UPDATE_TEMP_CONFIG", payload: updates });
  };

  const renderServiceConfig = (
    serviceName: string,
    config: ServiceConfig,
    description: string,
    fields: Array<{
      key: keyof ServiceConfig;
      label: string;
      options: Array<{ id: string; text: string }>;
      readonly?: boolean;
      globalValue?: string;
    }>,
  ) => {
    const isEditing = state.editingService === serviceName;
    const currentConfig = isEditing ? state.tempConfig : config;
    const hasWatsonxValidationError =
      isEditing &&
      currentConfig?.inferenceMethod === "watsonx" &&
      !!validationError;
    const hasPromptValidationError =
      isEditing &&
      serviceName === "Question and answer" &&
      !!currentConfig?.editSystemPrompt &&
      !!validationError;

    return (
      <ServiceConfigCard
        serviceName={serviceName}
        config={config}
        description={description}
        fields={fields}
        isEditing={isEditing}
        currentConfig={currentConfig}
        showApiKey={state.showApiKey[serviceName] || false}
        hasWatsonxValidationError={hasWatsonxValidationError}
        hasPromptValidationError={hasPromptValidationError}
        onEdit={() => handleEdit(serviceName)}
        onApply={() => handleApply(serviceName)}
        onCancel={handleCancel}
        onUpdateConfig={updateTempConfig}
        onToggleApiKey={() =>
          dispatch({ type: "TOGGLE_SHOW_API_KEY", payload: serviceName })
        }
      />
    );
  };

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      {validationError && (
        <div className={styles.errorContainer}>
          <p>{validationError}</p>
        </div>
      )}

      {/* Resource Requirements */}
      <ResourceRequirements
        resourceRequirements={resourceRequirements}
        resourcesLoading={resourcesLoading}
        resourcesError={resourcesError}
        resourceData={!!resources}
      />

      {/* Service Configurations */}
      <div className={styles.formSection}>
        {/* Digitize documents */}
        {renderServiceConfig(
          "Digitize documents",
          formData.services.digitizeDocuments,
          "Transforms documents such as manuals, invoices, and more into digitized and ingested texts.",
          [
            {
              key: "serviceVersion",
              label: "Service version",
              options: serviceVersionOptions,
            },
            {
              key: "embeddingModel",
              label: "Embedding model",
              options: embeddingModelOptions,
              readonly: true,
              globalValue: formData.embeddingModel,
            },
            {
              key: "vectorStore" as keyof ServiceConfig,
              label: "Vector store",
              options: vectorStoreOptions,
              readonly: true,
              globalValue: formData.vectorStore,
            },
            {
              key: "rerankerModel",
              label: "Reranker model",
              options: rerankerModelOptions,
            },
            {
              key: "llm",
              label: "Large Language Model (LLM)",
              options: llmOptions,
            },
            {
              key: "inferenceMethod",
              label: "Inference Backend",
              options: inferenceMethodOptions,
            },
          ],
        )}

        {/* Find similar items */}
        {renderServiceConfig(
          "Find similar items",
          formData.services.findSimilarItems,
          "Fetches similar items from the system's knowledge management for a given input item.",
          [
            {
              key: "serviceVersion",
              label: "Service version",
              options: serviceVersionOptions,
            },
            {
              key: "embeddingModel",
              label: "Embedding model",
              options: embeddingModelOptions,
              readonly: true,
              globalValue: formData.embeddingModel,
            },
            {
              key: "vectorStore" as keyof ServiceConfig,
              label: "Vector store",
              options: vectorStoreOptions,
              readonly: true,
              globalValue: formData.vectorStore,
            },
            {
              key: "rerankerModel",
              label: "Reranker model",
              options: rerankerModelOptions,
            },
            {
              key: "inferenceMethod",
              label: "Inference Backend",
              options: inferenceMethodOptions,
            },
          ],
        )}

        {/* Question and answer */}
        {renderServiceConfig(
          "Question and answer",
          formData.services.questionAndAnswer,
          "Answers questions in natural language by sourcing general & domain-specific knowledge.",
          [
            {
              key: "serviceVersion",
              label: "Service version",
              options: serviceVersionOptions,
            },
            {
              key: "embeddingModel",
              label: "Embedding model",
              options: embeddingModelOptions,
              readonly: true,
              globalValue: formData.embeddingModel,
            },
            {
              key: "vectorStore" as keyof ServiceConfig,
              label: "Vector store",
              options: vectorStoreOptions,
              readonly: true,
              globalValue: formData.vectorStore,
            },
            {
              key: "rerankerModel",
              label: "Reranker model",
              options: rerankerModelOptions,
            },
            {
              key: "llm",
              label: "Large Language Model (LLM)",
              options: llmOptions,
            },
            {
              key: "inferenceMethod",
              label: "Inference Backend",
              options: inferenceMethodOptions,
            },
          ],
        )}

        {/* Summarization */}
        {renderServiceConfig(
          "Summarization",
          formData.services.summarization,
          "Consolidates long input texts into a brief statement or account of the main points.",
          [
            {
              key: "serviceVersion",
              label: "Service version",
              options: serviceVersionOptions,
            },
            {
              key: "llm",
              label: "Large Language Model (LLM)",
              options: llmOptions,
            },
            {
              key: "inferenceMethod",
              label: "Inference Backend",
              options: inferenceMethodOptions,
            },
          ],
        )}
      </div>
    </>
  );
};
