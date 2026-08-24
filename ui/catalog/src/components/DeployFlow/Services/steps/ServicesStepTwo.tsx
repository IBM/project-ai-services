import { useMemo, useEffect } from "react";
import { InlineLoading, InlineNotification } from "@carbon/react";
import { sumProviderResources } from "../../Shared/utils/resources";
import { COMPONENT_TYPES } from "@/constants";
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
}) => {
  const { resources, resourcesLoading, resourcesError } = useResources();

  const componentModels = useServiceDeployStore(
    (state) => state.componentModels,
  );
  const componentModelsError = useServiceDeployStore(
    (state) => state.componentModelsError,
  );
  const providerSchemas = useServiceDeployStore(
    (state) => state.providerSchemas,
  );

  const inferenceComponentType = deployOptions.components.some(
    (c) => c.type === COMPONENT_TYPES.LLM,
  )
    ? COMPONENT_TYPES.LLM
    : deployOptions.components.some((c) => c.type === COMPONENT_TYPES.RERANKER)
      ? COMPONENT_TYPES.RERANKER
      : null;

  const inferenceModelsError =
    selectedServiceId && inferenceComponentType
      ? (componentModelsError[
          `${selectedServiceId}:${inferenceComponentType}`
        ] ?? null)
      : null;

  useEffect(() => {
    onComponentError?.(!!inferenceModelsError);
  }, [inferenceModelsError, onComponentError]);

  const selectedServiceConfig = selectedServiceId
    ? formData.services[selectedServiceId]
    : null;

  const calculatedResources = useMemo(
    () => calculateRequiredResources(formData, deployOptions),
    [formData, deployOptions],
  );

  const serviceVersionOptions = useMemo(
    () => [{ id: deployOptions.version, text: deployOptions.version }],
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
      const componentKey = `${selectedServiceId}:${component.type}`;
      const modelOptions = componentModels[componentKey] || [];
      const isStep1Component =
        component.type !== COMPONENT_TYPES.LLM &&
        component.type !== COMPONENT_TYPES.RERANKER;

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
      } else if (modelOptions.length > 0) {
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
  ]);

  const inferenceComponent = useMemo((): DeployOptionsComponent | null => {
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
          (
            componentModels[`${selectedServiceId}:${inferenceComponentType}`] ??
            []
          ).length === 0));

  const providerParamsByType = useMemo(() => {
    if (!selectedServiceId) return {};
    const prefix = `${selectedServiceId}:`;
    const result: Record<string, Record<string, ProviderSchema>> = {};
    for (const [storeKey, schema] of Object.entries(providerSchemas)) {
      if (!storeKey.startsWith(prefix)) continue;
      const rest = storeKey.slice(prefix.length);
      const colonIdx = rest.indexOf(":");
      if (colonIdx === -1) continue;
      result[rest.slice(0, colonIdx)] ??= {};
      result[rest.slice(0, colonIdx)][rest.slice(colonIdx + 1)] = schema;
    }
    return result;
  }, [selectedServiceId, providerSchemas]);

  const handleServiceChange = (serviceId: string, updated: ServiceConfig) => {
    onChange({ services: { ...formData.services, [serviceId]: updated } });
  };

  // Models for the inference component (LLM or reranker) used by handleLlmModelChange
  // to auto-resolve a compatible backend provider when the user picks a model.
  const inferenceModels = useMemo(() => {
    if (!selectedServiceId || !inferenceComponent) return [];
    return (
      componentModels[`${selectedServiceId}:${inferenceComponent.type}`] ?? []
    );
  }, [selectedServiceId, inferenceComponent, componentModels]);

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
        serviceSchema: null,
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
        <div className={styles.loadingContainer}>
          <InlineLoading description="Loading configuration options..." />
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
