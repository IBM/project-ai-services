import { useReducer, useMemo, useEffect, useState } from "react";
import styles from "../DeployFlow.module.scss";
import type { StepProps, ServiceConfig, ComponentConfig } from "../types";
import {
  getAcceleratorLabel,
  getResourceStatus,
  bytesToGB,
} from "../utils/StepTwo.utils";
import { ResourceRequirements } from "../components/ResourceRequirements";
import { ServiceConfigCard } from "../components/ServiceConfigCard";
import { fetchResources } from "@/api/digitalAssistants";
import {
  useBatchProviderParams,
  useMultiTypeProviderParams,
} from "@/hooks/useProviderParams";
import { getResourceSharingKey } from "@/utils/resourceSharing";
import { useDeployStore } from "@/store/deploy.store";
import type { ResourcesResponse, Component } from "@/types/digitalAssistants";
import type {
  ResourceItem,
  StepTwoState,
  StepTwoAction,
} from "../types/StepTwo.types";

// Initial state
const INITIAL_STATE: StepTwoState = {
  editingService: null,
  tempConfig: null,
  modelNamesByComponent: {},
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
    case "SET_MODEL_NAMES":
      return {
        ...state,
        modelNamesByComponent: {
          ...state.modelNamesByComponent,
          [action.payload.componentType]: action.payload.modelNames,
        },
      };
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

  // Get service description helper from store
  const { getServiceDescription } = useDeployStore();

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

    // First, add global components (shared across all services)
    // Note: Global components get their resources from service-specific component definitions
    deployOptions.global_components.forEach((globalComponent) => {
      const globalConfig = formData.globalComponents[globalComponent.type];
      if (!globalConfig?.providerId) return;

      // Find the first service that has this component type to get resource info
      let resourceProvider = null;
      for (const service of deployOptions.services) {
        const serviceComponent = service.components.find(
          (c) => c.type === globalComponent.type,
        );
        if (serviceComponent) {
          resourceProvider = serviceComponent.providers.find(
            (p) => p.id === globalConfig.providerId,
          );
          if (resourceProvider?.resources) {
            break;
          }
        }
      }

      if (!resourceProvider?.resources) return;

      // Global components are shared across all services, use provider+model as key
      const uniqueKey = getResourceSharingKey(
        "global", // Use "global" as serviceId for global components
        globalComponent.type,
        globalConfig.providerId,
        globalConfig.params || {},
      );

      if (!uniqueProviders[uniqueKey]) {
        uniqueProviders[uniqueKey] = {
          cpu: resourceProvider.resources.cpu || 0,
          memory: resourceProvider.resources.memory || 0,
          storage: resourceProvider.resources.storage || 0,
          accelerators: { ...(resourceProvider.resources.accelerators || {}) },
        };
      }
    });

    // Then, iterate through all services for service-specific components
    Object.entries(formData.services).forEach(([serviceId, serviceConfig]) => {
      if (!serviceConfig.enabled) return;

      const service = deployOptions.services.find((s) => s.id === serviceId);
      if (!service) return;

      // Iterate through all components in the service
      service.components.forEach((component) => {
        // Check if this is a global component (already counted above)
        const isGlobalComponent = deployOptions.global_components.some(
          (gc) => gc.type === component.type,
        );

        // Skip global components as they're already counted
        if (isGlobalComponent) return;

        const componentConfig = serviceConfig.components[component.type];
        if (!componentConfig) return;

        // For components that support inference backend, use it if available
        let selectedProviderId = componentConfig.providerId;
        if (serviceConfig.inferenceBackend) {
          selectedProviderId = serviceConfig.inferenceBackend;
        }

        if (!selectedProviderId) return;

        const provider = component.providers.find(
          (p) => p.id === selectedProviderId,
        );

        if (!provider?.resources) return;

        // Use dynamic resource sharing logic
        // Always use component-level params (which contains the model parameter)
        // This ensures proper resource sharing based on the model being used
        const paramsForSharing = componentConfig.params || {};

        const uniqueKey = getResourceSharingKey(
          serviceId,
          component.type,
          selectedProviderId,
          paramsForSharing,
        );

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
  }, [
    formData.services,
    formData.globalComponents,
    deployOptions.services,
    deployOptions.global_components,
  ]);

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
      available: Math.floor(resources.cpu.available_cpu).toString(),
      unit: "vCPUs",
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
  const serviceVersionOptions = useMemo(
    () => [{ id: deployOptions.version, text: deployOptions.version }],
    [deployOptions.version],
  );

  // Get all provider IDs for batch fetching params
  const allProviderIds = useMemo(() => {
    const providersByType: Record<string, Set<string>> = {};

    deployOptions.services.forEach((service) => {
      service.components.forEach((component) => {
        if (!providersByType[component.type]) {
          providersByType[component.type] = new Set();
        }
        component.providers.forEach((provider) => {
          providersByType[component.type].add(provider.id);
        });
      });
    });

    // Convert Sets to arrays
    const result: Record<string, string[]> = {};
    Object.entries(providersByType).forEach(([type, ids]) => {
      result[type] = Array.from(ids);
    });

    return result;
  }, [deployOptions.services]);

  // Fetch provider parameters for all component types dynamically
  // This single hook call handles all component types, respecting Rules of Hooks
  const {
    paramsByType,
    isLoading: _paramsLoading,
    errorsByType,
  } = useMultiTypeProviderParams(allProviderIds);

  // Transform to match the interface expected by the rest of the component
  const providerParamsByType = useMemo(() => {
    const result: Record<
      string,
      ReturnType<typeof useBatchProviderParams>
    > = {};

    Object.entries(paramsByType).forEach(([componentType, paramsMap]) => {
      result[componentType] = {
        paramsMap,
        isLoading: false, // Already loaded by useMultiTypeProviderParams
        errors: errorsByType[componentType] || {},
      };
    });

    return result;
  }, [paramsByType, errorsByType]);

  // Extract model names from params for display - DYNAMIC for all component types
  useEffect(() => {
    // Iterate through all component types dynamically
    Object.entries(providerParamsByType).forEach(([componentType, data]) => {
      const paramsMap = data.paramsMap || {};
      const modelNamesMap: Record<string, string> = {};

      // Extract model names for this component type
      for (const [providerId, params] of Object.entries(paramsMap)) {
        const properties = params?.properties as Record<
          string,
          { oneOf?: Array<{ title?: string }> }
        >;

        const modelTitle = properties?.model?.oneOf?.[0]?.title;

        if (modelTitle) {
          modelNamesMap[providerId] = modelTitle;
        }
      }

      // Only dispatch if we have model names and they've changed
      if (Object.keys(modelNamesMap).length > 0) {
        const existingNames = state.modelNamesByComponent[componentType] || {};
        const hasChanges = Object.keys(modelNamesMap).some(
          (key) => existingNames[key] !== modelNamesMap[key],
        );

        if (hasChanges || Object.keys(existingNames).length === 0) {
          dispatch({
            type: "SET_MODEL_NAMES",
            payload: { componentType, modelNames: modelNamesMap },
          });
        }
      }
    });
  }, [providerParamsByType, state.modelNamesByComponent]);

  // Populate model parameters for default providers once params are loaded
  useEffect(() => {
    if (Object.keys(providerParamsByType).length === 0) return;

    const serviceUpdates: Record<string, ServiceConfig> = {};
    let hasUpdates = false;

    // Check each service
    Object.entries(formData.services).forEach(([serviceId, serviceConfig]) => {
      const componentUpdates: Record<string, ComponentConfig> = {};
      let hasComponentUpdates = false;

      // Check each component in the service
      Object.entries(serviceConfig.components).forEach(
        ([componentType, config]) => {
          // Skip if already has model parameter
          if (config.params?.model) return;

          const paramsMap =
            providerParamsByType[componentType]?.paramsMap || {};
          const cachedParams = paramsMap[config.providerId];
          const properties = cachedParams?.properties as Record<
            string,
            { default?: unknown }
          >;

          if (properties?.model?.default) {
            componentUpdates[componentType] = {
              ...config,
              params: {
                ...config.params,
                model: properties.model.default,
              },
            };
            hasComponentUpdates = true;
          }
        },
      );

      // If this service has component updates, add to service updates
      if (hasComponentUpdates) {
        serviceUpdates[serviceId] = {
          ...serviceConfig,
          components: {
            ...serviceConfig.components,
            ...componentUpdates,
          },
        };
        hasUpdates = true;
      }
    });

    // Apply updates if any
    if (hasUpdates) {
      onChange({
        services: {
          ...formData.services,
          ...serviceUpdates,
        },
      });
    }
  }, [providerParamsByType, formData.services, onChange]);

  const handleEdit = (serviceId: string) => {
    const config = formData.services[serviceId];
    setValidationError(null);
    dispatch({ type: "SET_TEMP_CONFIG", payload: { ...config } });
    dispatch({ type: "SET_EDITING_SERVICE", payload: serviceId });
    onEditingChange?.(true);
  };

  const handleApply = (serviceId: string) => {
    if (!state.tempConfig) {
      return;
    }

    setValidationError(null);
    onChange({
      services: {
        ...formData.services,
        [serviceId]: state.tempConfig,
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
    serviceId: string,
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
    llmComponent: Component | null,
    rerankerComponent: Component | null,
  ) => {
    const isEditing = state.editingService === serviceId;
    const currentConfig = isEditing ? state.tempConfig : config;

    return (
      <ServiceConfigCard
        serviceId={serviceId}
        serviceName={serviceName}
        config={config}
        description={description}
        fields={fields}
        isEditing={isEditing}
        currentConfig={currentConfig}
        providerParamsByType={providerParamsByType}
        llmComponent={llmComponent}
        rerankerComponent={rerankerComponent}
        onEdit={() => handleEdit(serviceId)}
        onApply={() => handleApply(serviceId)}
        onCancel={handleCancel}
        onUpdateConfig={updateTempConfig}
      />
    );
  };

  // Build service configurations dynamically from API
  const serviceConfigurations = useMemo(() => {
    return deployOptions.services
      .map((service) => {
        const serviceConfig = formData.services[service.id];
        if (!serviceConfig) return null;

        // Build fields dynamically from service components
        const fields: Array<{
          key: keyof ServiceConfig;
          label: string;
          options: Array<{ id: string; text: string }>;
          readonly?: boolean;
          globalValue?: string;
        }> = [];

        // Always add service version first
        fields.push({
          key: "version" as keyof ServiceConfig,
          label: "Service version",
          options: serviceVersionOptions,
        });

        // Track components that need Inference Backend field (LLM, reranker, etc.)
        let llmComponent: Component | null = null;
        let rerankerComponent: Component | null = null;

        // Add component fields dynamically
        service.components.forEach((component) => {
          // Track components that may need inference backend (first one wins)
          if (!llmComponent && component.type === "llm") {
            llmComponent = component as Component;
          }
          if (!rerankerComponent && component.type === "reranker") {
            rerankerComponent = component as Component;
          }

          // Build provider options with model names, deduplicate by preferring default provider
          const componentModelNames =
            state.modelNamesByComponent[component.type];
          const providersByDisplayName = new Map<
            string,
            (typeof component.providers)[0]
          >();

          component.providers.forEach((provider) => {
            const displayName =
              componentModelNames?.[provider.id] || provider.name;

            const existing = providersByDisplayName.get(displayName);
            if (!existing) {
              // First provider with this display name
              providersByDisplayName.set(displayName, provider);
            } else if (provider.default && !existing.default) {
              // Replace with default provider if current one isn't default
              providersByDisplayName.set(displayName, provider);
            }
          });

          const providers: Array<{ id: string; text: string }> = [];
          providersByDisplayName.forEach((provider, displayName) => {
            providers.push({
              id: provider.id,
              text: displayName,
            });
          });

          // Check if this component is a global component (shared across services)
          const isGlobalComponent = deployOptions.global_components.some(
            (gc) => gc.type === component.type,
          );

          fields.push({
            key: component.type as keyof ServiceConfig,
            label: component.name,
            options: providers,
            readonly: isGlobalComponent,
            globalValue: isGlobalComponent
              ? formData.globalComponents[component.type]?.providerId
              : undefined,
          });
        });

        // Add Inference Backend field if service has LLM component
        if (llmComponent) {
          // Get the currently selected LLM model
          const selectedLlmModel = serviceConfig.components?.llm?.params?.model;

          // Get LLM provider params to check which providers support the selected model
          const llmParamsMap = providerParamsByType["llm"]?.paramsMap || {};

          // Filter providers that support the same model as the selected LLM
          const inferenceBackendOptions = (llmComponent as Component).providers
            .filter((provider) => {
              // If no LLM model selected yet, show all providers
              if (!selectedLlmModel) return true;

              // Get this provider's schema
              const providerSchema = llmParamsMap[provider.id];
              if (!providerSchema || !providerSchema.properties) return false;

              const properties = providerSchema.properties as Record<
                string,
                { default?: unknown }
              >;
              const providerDefaultModel = properties.model?.default;

              // Check if this provider's default model matches the selected model
              return providerDefaultModel === selectedLlmModel;
            })
            .map((provider) => ({
              id: provider.id,
              text: provider.name, // Use provider name, not model name
            }));

          fields.push({
            key: "inferenceBackend" as keyof ServiceConfig,
            label: "Inference backend",
            options: inferenceBackendOptions,
          });
        }

        // Add Inference Backend field if service has reranker component
        if (rerankerComponent) {
          // Get the currently selected reranker model
          const selectedRerankerModel =
            serviceConfig.components?.reranker?.params?.model;

          // Get reranker provider params to check which providers support the selected model
          const rerankerParamsMap =
            providerParamsByType["reranker"]?.paramsMap || {};

          // Filter providers that support the same model as the selected reranker
          const inferenceBackendOptions = (
            rerankerComponent as Component
          ).providers
            .filter((provider) => {
              // If no reranker model selected yet, show all providers
              if (!selectedRerankerModel) return true;

              // Get this provider's schema
              const providerSchema = rerankerParamsMap[provider.id];
              if (!providerSchema || !providerSchema.properties) return false;

              const properties = providerSchema.properties as Record<
                string,
                { default?: unknown }
              >;
              const providerDefaultModel = properties.model?.default;

              // Check if this provider's default model matches the selected model
              return providerDefaultModel === selectedRerankerModel;
            })
            .map((provider) => ({
              id: provider.id,
              text: provider.name, // Use provider name, not model name
            }));

          fields.push({
            key: "inferenceBackend" as keyof ServiceConfig,
            label: "Inference backend",
            options: inferenceBackendOptions,
          });
        }

        return {
          serviceId: service.id,
          serviceName: service.name,
          description: getServiceDescription(service.id),
          config: serviceConfig,
          fields,
          llmComponent: llmComponent as Component | null,
          rerankerComponent: rerankerComponent as Component | null,
        };
      })
      .sort((a, b) => {
        // Sort service cards alphabetically by service name
        if (!a || !b) return 0;
        return a.serviceName.localeCompare(b.serviceName);
      });
  }, [
    deployOptions.services,
    deployOptions.global_components,
    formData.services,
    formData.globalComponents,
    serviceVersionOptions,
    state.modelNamesByComponent,
    getServiceDescription,
    providerParamsByType,
  ]);

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

      {/* Service Configurations - Rendered Dynamically */}
      <div className={styles.formSection}>
        {serviceConfigurations.map((serviceConfig) => {
          if (!serviceConfig) return null;

          return (
            <div key={serviceConfig.serviceId}>
              {renderServiceConfig(
                serviceConfig.serviceId,
                serviceConfig.serviceName,
                serviceConfig.config,
                serviceConfig.description,
                serviceConfig.fields,
                serviceConfig.llmComponent,
                serviceConfig.rerankerComponent,
              )}
            </div>
          );
        })}
      </div>
    </>
  );
};
