import { useMemo, useEffect } from "react";
import { formatVersion } from "@/utils/string";
import { InlineNotification, SkeletonText } from "@carbon/react";
import { sumProviderResources } from "../../Shared/utils/resources";
import { COMPONENT_TYPES, DEFAULT_RUNTIME } from "@/constants";
import type {
  ServiceDeployOptions,
  DeployOptionsComponent,
  ProviderSchema,
} from "@/types/api.types";
import { useResources } from "../../Shared/hooks/useResources";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import styles from "../../Shared/DeployFlow.shared.module.scss";
import type { StepProps } from "../types";
import type {
  ServiceConfig,
  ServiceConfigField,
  DeployFormData,
} from "../../Shared/types";
import {
  ResourceRequirementsPanel,
  type CalculatedResources,
} from "../../Shared/components/ResourceRequirementsPanel";
import {
  SharedStepTwo,
  type SharedStepTwoServiceItem,
} from "../../Shared/steps/SharedStepTwo";

const calculateRequiredResources = (
  formData: DeployFormData,
  deployOptions: ServiceDeployOptions,
): CalculatedResources => {
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

    if (deployOptions.resources) {
      const serviceResourceKey = `service-${serviceKey}`;
      if (!uniqueProviders[serviceResourceKey]) {
        uniqueProviders[serviceResourceKey] = {
          cpu: deployOptions.resources.cpu || 0,
          memory: deployOptions.resources.memory || 0,
          storage: deployOptions.resources.storage || 0,
          accelerators: { ...(deployOptions.resources.accelerators || {}) },
        };
      }
    }

    Object.entries(serviceConfig.components).forEach(
      ([componentType, componentConfig]) => {
        const selectedProviderId = componentConfig.providerId;
        if (!selectedProviderId) return;

        const component = deployOptions.components.find(
          (c) => c.type === componentType,
        );
        if (!component) return;

        const provider = component.providers.find(
          (p) => p.id === selectedProviderId,
        );
        const uniqueKey = `${selectedProviderId}-${componentType}`;

        if (provider?.resources && !uniqueProviders[uniqueKey]) {
          uniqueProviders[uniqueKey] = {
            cpu: provider.resources.cpu || 0,
            memory: provider.resources.memory || 0,
            storage: provider.resources.storage || 0,
            accelerators: { ...(provider.resources.accelerators || {}) },
          };
        }
      },
    );
  });

  return sumProviderResources(uniqueProviders);
};

export const ServicesStepTwo: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  onEditingChange,
  onResourceStatusChange,
  selectedServiceId,
  llmModelsWithProviders = [],
  serviceDescription,
  isLoadingLlmModels = false,
  onComponentError,
  runtime = DEFAULT_RUNTIME,
  serviceSchema = null,
}) => {
  const { resources, resourcesLoading, resourcesError } = useResources(
    formData.workerName,
  );

  const componentModels = useServiceDeployStore(
    (state) => state.componentModels,
  );
  const componentModelsError = useServiceDeployStore(
    (state) => state.componentModelsError,
  );
  const componentModelsLoading = useServiceDeployStore(
    (state) => state.componentModelsLoading,
  );
  const providerSchemas = useServiceDeployStore(
    (state) => state.providerSchemas,
  );

  // Only LLM gets the dedicated inference-backend path (ServiceConfigCard inferenceComponent).
  // All other component types — including embedding, vector store, reranker — are routed
  // through the isCustomComponent path which already renders model + backend dropdowns.
  const inferenceComponentType = (() => {
    if (deployOptions.components.some((c) => c.type === COMPONENT_TYPES.LLM))
      return COMPONENT_TYPES.LLM;
    // Fall back to the first non-LLM component so the skeleton also covers
    // deploy-options/service-schema loading for services without an LLM.
    return (
      deployOptions.components.find((c) => c.type !== COMPONENT_TYPES.LLM)
        ?.type ?? null
    );
  })();

  // Every component (LLM, embedding, vector store, reranker, custom) can fail to
  // load its models independently — report all of them, not just one.
  const failedComponentNames = useMemo(() => {
    if (!selectedServiceId) return [];
    return deployOptions.components
      .filter(
        (c) =>
          !!componentModelsError[`${selectedServiceId}:${c.type}:${runtime}`],
      )
      .map((c) => c.name || c.type);
  }, [
    selectedServiceId,
    deployOptions.components,
    componentModelsError,
    runtime,
  ]);

  const inferenceModelsError = failedComponentNames.length > 0;

  useEffect(() => {
    onComponentError?.(inferenceModelsError);
  }, [inferenceModelsError, onComponentError]);

  // Seed default model param for the LLM component when its models arrive from
  // the store. Guarded by `if (llmConfig.params?.model) return` so it is
  // idempotent — safe to re-run whenever componentModels changes.
  useEffect(() => {
    if (!selectedServiceId) return;

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    const llmConfig = serviceConfig.components[COMPONENT_TYPES.LLM];
    if (!llmConfig || llmConfig.params?.model) return;

    const llmModels =
      componentModels[
        `${selectedServiceId}:${COMPONENT_TYPES.LLM}:${runtime}`
      ] ?? [];
    const matchingModel = llmModels.find(
      (m) => m.providerId === llmConfig.providerId,
    );
    if (!matchingModel) return;

    onChange({
      services: {
        ...formData.services,
        [selectedServiceId]: {
          ...serviceConfig,
          components: {
            ...serviceConfig.components,
            [COMPONENT_TYPES.LLM]: {
              ...llmConfig,
              params: { ...llmConfig.params, model: matchingModel.id },
            },
          },
        },
      },
    });
  }, [
    selectedServiceId,
    formData.services,
    componentModels,
    onChange,
    runtime,
  ]);

  // Seed default model for all non-LLM component types (embedding, vector store,
  // reranker, custom) when their models arrive from the store. Mirrors DA pattern:
  // prefer the default provider's first model, fall back to first overall.
  // Idempotent — skips when model is already set. Sets both providerId and params.model.
  useEffect(() => {
    if (!selectedServiceId) return;

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    const nonLlmComponents = deployOptions.components.filter(
      (c) => c.type !== COMPONENT_TYPES.LLM,
    );
    if (nonLlmComponents.length === 0) return;

    let hasUpdates = false;
    const updatedComponents = { ...serviceConfig.components };

    nonLlmComponents.forEach((component) => {
      const componentConfig = serviceConfig.components[component.type];
      if (!componentConfig || componentConfig.params?.model) return;

      const models =
        componentModels[`${selectedServiceId}:${component.type}:${runtime}`] ??
        [];
      if (models.length === 0) return;

      // Prefer the default provider's first model, fall back to first overall.
      const defaultProviderId =
        componentConfig.providerId ||
        component.providers.find((p) => p.default)?.id ||
        component.providers[0]?.id;
      const matchingModel =
        models.find((m) => m.providerId === defaultProviderId) ?? models[0];
      if (!matchingModel) return;

      updatedComponents[component.type] = {
        ...componentConfig,
        providerId: matchingModel.providerId,
        params: { ...componentConfig.params, model: matchingModel.id },
      };
      hasUpdates = true;
    });

    if (!hasUpdates) return;

    onChange({
      services: {
        ...formData.services,
        [selectedServiceId]: {
          ...serviceConfig,
          components: updatedComponents,
        },
      },
    });
  }, [
    selectedServiceId,
    formData.services,
    deployOptions.components,
    componentModels,
    onChange,
    runtime,
  ]);

  const selectedServiceConfig = selectedServiceId
    ? formData.services[selectedServiceId]
    : null;

  const calculatedResources = useMemo(
    () => calculateRequiredResources(formData, deployOptions),
    [formData, deployOptions],
  );

  const serviceVersionOptions = useMemo(
    () => [
      { id: deployOptions.version, text: formatVersion(deployOptions.version) },
    ],
    [deployOptions.version],
  );

  // Deduplicated LLM model options for display
  const llmOptions = useMemo(() => {
    if (llmModelsWithProviders.length === 0) return [];
    const seen = new Set<string>();
    return llmModelsWithProviders.filter((opt) => {
      if (seen.has(opt.id)) return false;
      seen.add(opt.id);
      return true;
    });
  }, [llmModelsWithProviders]);

  // Build fields list for the single service card
  const serviceFields = useMemo((): ServiceConfigField[] => {
    if (!selectedServiceConfig) return [];

    const fields: ServiceConfigField[] = [
      {
        key: "version" as keyof ServiceConfig,
        label: "Service version",
        options: serviceVersionOptions,
      },
    ];

    deployOptions.components.forEach((component) => {
      const componentKey = `${selectedServiceId}:${component.type}:${runtime}`;
      const modelOptions = componentModels[componentKey] || [];

      if (component.type === COMPONENT_TYPES.LLM) {
        // LLM gets the dedicated inference-backend path (model dropdown + separate
        // backend dropdown rendered by ServiceConfigCard's inferenceBackendField).
        if (llmOptions.length > 0) {
          fields.push({
            key: component.type as keyof ServiceConfig,
            label: component.name || "Large language model (LLM)",
            options: llmOptions,
            isModelFirst: true,
          });
        }
      } else {
        // All other components — embedding, vector store, reranker, custom —
        // use the isCustomComponent path: model dropdown + backend dropdown,
        // with credential fields when the provider schema defines them.
        fields.push({
          key: component.type as keyof ServiceConfig,
          label: component.name || component.type,
          options: modelOptions,
          isModelFirst: modelOptions.length > 0,
          isCustomComponent: true,
          providerOptions: component.providers.map((p) => ({
            id: p.id,
            text: p.name,
          })),
        });
      }
    });

    return fields;
  }, [
    selectedServiceConfig,
    deployOptions.components,
    serviceVersionOptions,
    llmOptions,
    componentModels,
    selectedServiceId,
    runtime,
  ]);

  const inferenceComponent = useMemo((): DeployOptionsComponent | null => {
    // Only LLM uses the dedicated inference-backend flow in ServiceConfigCard.
    // Reranker and all other types now go through the isCustomComponent path.
    return (deployOptions.components.find(
      (c) => c.type === COMPONENT_TYPES.LLM,
    ) ?? null) as DeployOptionsComponent | null;
  }, [deployOptions.components]);

  // isLoadingLlmModels covers LLM model fetching (and deploy-options/schema loading).
  const isLoadingInferenceOptions =
    !!inferenceComponentType && !inferenceModelsError && isLoadingLlmModels;

  // Every non-LLM component (embedding, vector store, reranker, custom) has its own
  // loading flag. Never use an empty model list as a proxy for "still loading" — a
  // schema that has no model property legitimately returns zero model options.
  const dynamicComponents = useMemo(
    () =>
      deployOptions.components.filter(
        (c) => c.type !== COMPONENT_TYPES.LLM && c.providers.length > 0,
      ),
    [deployOptions.components],
  );
  const isLoadingDynamicOptions =
    !!selectedServiceId &&
    !inferenceModelsError &&
    dynamicComponents.some(
      (c) =>
        !!componentModelsLoading[`${selectedServiceId}:${c.type}:${runtime}`],
    );

  const providerParamsByType = useMemo(() => {
    if (!selectedServiceId) return {};
    // Store key format: "serviceId:componentType:providerId:runtime"
    // Extract only keys matching this service and the active runtime.
    const prefix = `${selectedServiceId}:`;
    const runtimeSuffix = `:${runtime}`;
    const result: Record<string, Record<string, ProviderSchema>> = {};
    for (const [storeKey, schema] of Object.entries(providerSchemas)) {
      if (!storeKey.startsWith(prefix) || !storeKey.endsWith(runtimeSuffix))
        continue;
      // Strip serviceId prefix and :runtime suffix to get "componentType:providerId"
      const middle = storeKey.slice(prefix.length, -runtimeSuffix.length);
      const colonIdx = middle.indexOf(":");
      if (colonIdx === -1) continue;
      const componentType = middle.slice(0, colonIdx);
      const providerId = middle.slice(colonIdx + 1);
      result[componentType] ??= {};
      result[componentType][providerId] = schema;
    }
    return result;
  }, [selectedServiceId, providerSchemas, runtime]);

  const handleServiceChange = (serviceId: string, updated: ServiceConfig) => {
    onChange({ services: { ...formData.services, [serviceId]: updated } });
  };

  // Models for the inference component (LLM or reranker) used by handleLlmModelChange
  // to auto-resolve a compatible backend provider when the user picks a model.
  const inferenceModels = useMemo(() => {
    if (!selectedServiceId || !inferenceComponent) return [];
    return (
      componentModels[
        `${selectedServiceId}:${inferenceComponent.type}:${runtime}`
      ] ?? []
    );
  }, [selectedServiceId, inferenceComponent, componentModels, runtime]);

  // Build the single-item services array for SharedStepTwo
  const serviceItems = useMemo((): SharedStepTwoServiceItem[] => {
    if (!selectedServiceId || !selectedServiceConfig) return [];
    return [
      {
        serviceId: selectedServiceId,
        serviceName: deployOptions.name,
        config: selectedServiceConfig,
        description: serviceDescription ?? "",
        fields: serviceFields,
        inferenceComponent: inferenceComponent ?? null,
        serviceSchema: serviceSchema ?? null,
        llmModelsWithProviders: inferenceModels,
      },
    ];
  }, [
    selectedServiceId,
    selectedServiceConfig,
    deployOptions.name,
    serviceDescription,
    serviceFields,
    inferenceComponent,
    inferenceModels,
    serviceSchema,
  ]);

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      <ResourceRequirementsPanel
        calculatedResources={calculatedResources}
        resourceData={resources}
        resourcesLoading={resourcesLoading}
        resourcesError={resourcesError}
        onResourceStatusChange={onResourceStatusChange}
      />

      {inferenceModelsError && (
        <InlineNotification
          kind="error"
          title={`Failed to load ${failedComponentNames.join(", ")} models.`}
          subtitle="Cancel and reopen to try again."
          lowContrast
          hideCloseButton
        />
      )}

      {isLoadingInferenceOptions || isLoadingDynamicOptions ? (
        <div className={styles.skeletonContent}>
          <SkeletonText width="50%" className={styles.skeletonHeading} />
          <SkeletonText lineCount={3} />
          <SkeletonText width="50%" className={styles.skeletonHeading} />
          <SkeletonText lineCount={3} />
        </div>
      ) : (
        <div className={styles.formSection}>
          <SharedStepTwo
            services={serviceItems}
            providerParamsByType={providerParamsByType}
            onChange={handleServiceChange}
            onEditingChange={onEditingChange}
          />
        </div>
      )}
    </>
  );
};
