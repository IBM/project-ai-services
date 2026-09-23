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

  // Step 1 component types are the known selector types (embedding, vector store).
  // Everything else is a Step 2 / inference component, including unknown custom types.
  const isStep1ComponentType = (type: string) =>
    type === COMPONENT_TYPES.EMBEDDING || type === COMPONENT_TYPES.VECTOR_STORE;

  // Known inference types that have their own dedicated rendering paths.
  const isKnownInferenceType = (type: string) =>
    type === COMPONENT_TYPES.LLM || type === COMPONENT_TYPES.RERANKER;

  // Resolve the primary inference component type for error reporting / loading state.
  // Prefer the known LLM type; fall back to reranker; finally pick the first non-Step-1 type.
  const inferenceComponentType = (() => {
    if (deployOptions.components.some((c) => c.type === COMPONENT_TYPES.LLM))
      return COMPONENT_TYPES.LLM;
    if (
      deployOptions.components.some((c) => c.type === COMPONENT_TYPES.RERANKER)
    )
      return COMPONENT_TYPES.RERANKER;
    // Pick the first unknown/custom component type as the representative inference type.
    return (
      deployOptions.components.find((c) => !isStep1ComponentType(c.type))
        ?.type ?? null
    );
  })();

  const inferenceModelsError =
    selectedServiceId && inferenceComponentType
      ? (componentModelsError[
          `${selectedServiceId}:${inferenceComponentType}:${runtime}`
        ] ?? null)
      : null;

  useEffect(() => {
    onComponentError?.(!!inferenceModelsError);
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

  // Seed default model for custom component types from componentModels.
  // Custom types are now model-first (same as LLM): pick the model whose
  // providerId matches the initialised default provider, falling back to
  // the first model in the list. Idempotent — skips when model is already set.
  useEffect(() => {
    if (!selectedServiceId) return;

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    const customComponents = deployOptions.components.filter(
      (c) =>
        c.type !== COMPONENT_TYPES.LLM &&
        c.type !== COMPONENT_TYPES.RERANKER &&
        c.type !== COMPONENT_TYPES.EMBEDDING &&
        c.type !== COMPONENT_TYPES.VECTOR_STORE,
    );
    if (customComponents.length === 0) return;

    let hasUpdates = false;
    const updatedComponents = { ...serviceConfig.components };

    customComponents.forEach((component) => {
      const componentConfig = serviceConfig.components[component.type];
      if (!componentConfig || componentConfig.params?.model) return;

      const models =
        componentModels[`${selectedServiceId}:${component.type}:${runtime}`] ??
        [];
      // Match by default provider first; fall back to first model in list.
      const matchingModel =
        models.find((m) => m.providerId === componentConfig.providerId) ??
        models[0];
      if (!matchingModel) return;

      updatedComponents[component.type] = {
        ...componentConfig,
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
      const isStep1Component = isStep1ComponentType(component.type);

      if (isStep1Component) {
        // Step 1 components (embedding, vector store) are readonly in step 2.
        // globalValue is the raw id so the card can resolve the display name via options.
        const currentModel = selectedServiceConfig?.components?.[component.type]
          ?.params?.model as string | undefined;
        const currentProviderId =
          selectedServiceConfig?.components?.[component.type]?.providerId;
        // If model-first options exist, the id to match is the model name.
        // Otherwise the id is the provider id.
        const globalValue =
          modelOptions.length > 0
            ? currentModel || currentProviderId || ""
            : currentProviderId || "";
        fields.push({
          key: component.type as keyof ServiceConfig,
          label: component.name || component.type,
          options:
            modelOptions.length > 0
              ? modelOptions
              : component.providers.map((p) => ({ id: p.id, text: p.name })),
          isModelFirst: modelOptions.length > 0,
          readonly: true,
          globalValue,
        });
      } else if (component.type === COMPONENT_TYPES.LLM) {
        if (llmOptions.length > 0) {
          fields.push({
            key: component.type as keyof ServiceConfig,
            label: component.name || "Large language model (LLM)",
            options: llmOptions,
            isModelFirst: true,
          });
        }
      } else if (!isKnownInferenceType(component.type)) {
        // Custom component type (e.g. custom_llm): model-first, then backend —
        // matching the LLM pattern. options = flat model list across all providers.
        // providerOptions is kept for the backend dropdown filtered by selected model.
        fields.push({
          key: component.type as keyof ServiceConfig,
          label: component.name || component.type,
          options: modelOptions,
          isModelFirst: true,
          isCustomComponent: true,
          providerOptions: component.providers.map((p) => ({
            id: p.id,
            text: p.name,
          })),
        });
      } else if (modelOptions.length > 0) {
        // Known inference types other than LLM (e.g. reranker) with model options
        fields.push({
          key: component.type as keyof ServiceConfig,
          label: component.name || component.type,
          options: modelOptions,
          isModelFirst: true,
        });
      } else {
        fields.push({
          key: component.type as keyof ServiceConfig,
          label: component.name || component.type,
          options: component.providers.map((p) => ({ id: p.id, text: p.name })),
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
    // Only LLM and reranker use the inference-backend/model-first flow.
    // Custom component types have their own provider-first two-dropdown path
    // and must NOT be assigned as the inferenceComponent — doing so would cause
    // ServiceConfigCard to render a spurious "LLM inference backend" dropdown.
    return (deployOptions.components.find(
      (c) => c.type === COMPONENT_TYPES.LLM,
    ) ??
      deployOptions.components.find(
        (c) => c.type === COMPONENT_TYPES.RERANKER,
      ) ??
      null) as DeployOptionsComponent | null;
  }, [deployOptions.components]);

  const isLoadingInferenceOptions =
    !!inferenceComponentType &&
    !inferenceModelsError &&
    (isLoadingLlmModels ||
      (inferenceComponentType === COMPONENT_TYPES.LLM
        ? llmModelsWithProviders.length === 0 && llmOptions.length === 0
        : !selectedServiceId ||
          // For non-LLM types (reranker, custom), rely on the explicit loading flag
          // rather than an empty model list — custom types may legitimately have no
          // model (no schema) and should still render their dropdowns when loaded.
          !!componentModelsLoading[
            `${selectedServiceId}:${inferenceComponentType}:${runtime}`
          ]));

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
          title={`Failed to load ${inferenceComponentType ?? "inference"} models.`}
          subtitle="Cancel and reopen to try again."
          lowContrast
          hideCloseButton
        />
      )}

      {isLoadingInferenceOptions ? (
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
