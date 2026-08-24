import { useMemo, useEffect } from "react";
import { InlineNotification } from "@carbon/react";
import styles from "../../Shared/DeployFlow.shared.module.scss";
import type { StepProps } from "../types";
import type { ServiceConfig, ServiceConfigField } from "../../Shared/types";
import type { ComponentConfig, DeployFormData } from "../../Shared/types";
import { ResourceRequirementsPanel } from "../../Shared/components/ResourceRequirementsPanel";
import {
  SharedStepTwo,
  type SharedStepTwoServiceItem,
} from "../../Shared/steps/SharedStepTwo";
import { useResources } from "../../Shared/hooks/useResources";
import { getResourceSharingKey } from "../utils/resourceSharing";
import { sumProviderResources } from "../../Shared/utils/resources";
import { useDeployStore, type ServiceParamsCache } from "@/store/deploy.store";
import type {
  DeployOptionsResponse,
  DeployOptionsComponent as Component,
} from "@/types/api.types";
import { COMPONENT_TYPES } from "@/constants";

type InferenceOptions = {
  options: Array<{ id: string; text: string }>;

  modelsWithProviders: Array<{
    id: string;
    text: string;
    providerId: string;
    providerName: string;
  }>;
};

function buildInferenceOptions(
  providerParamsByType: Record<
    string,
    Record<string, { properties?: unknown; name?: string }>
  >,
  componentType: string,
): InferenceOptions {
  const schemas = providerParamsByType[componentType] || {};
  const seen = new Set<string>();
  const options: Array<{ id: string; text: string }> = [];
  const modelsWithProviders: Array<{
    id: string;
    text: string;
    providerId: string;
    providerName: string;
  }> = [];

  Object.entries(schemas).forEach(([providerId, schema]) => {
    const providerName = schema.name ?? providerId;
    const properties = schema.properties as Record<
      string,
      { oneOf?: Array<{ const: string; title?: string }>; default?: string }
    >;
    if (properties?.model?.oneOf) {
      properties.model.oneOf.forEach((opt) => {
        const text = opt.title || opt.const;
        modelsWithProviders.push({
          id: opt.const,
          text,
          providerId,
          providerName,
        });
        if (!seen.has(opt.const)) {
          seen.add(opt.const);
          options.push({ id: opt.const, text });
        }
      });
    } else if (properties?.model?.default) {
      const modelId = properties.model.default;
      modelsWithProviders.push({
        id: modelId,
        text: modelId,
        providerId,
        providerName,
      });
      if (!seen.has(modelId)) {
        seen.add(modelId);
        options.push({ id: modelId, text: modelId });
      }
    }
  });

  return { options, modelsWithProviders };
}

// StepProps narrowed so services carry the base ServiceConfig.
type DAStepProps = Omit<StepProps, "formData"> & {
  formData: Omit<DeployFormData, "services"> & {
    services: Record<string, ServiceConfig>;
  };
};

type DAFormData = DAStepProps["formData"];

const calculateDARequiredResources = (
  formData: DAFormData,
  deployOptions: DeployOptionsResponse,
) => {
  const uniqueProviders: Record<
    string,
    {
      cpu: number;
      memory: number;
      storage: number;
      accelerators: Record<string, number>;
    }
  > = {};

  // Global components — shared across all services
  deployOptions.global_components.forEach((globalComponent) => {
    const globalConfig = formData.globalComponents[globalComponent.type];
    if (!globalConfig?.providerId) return;

    let resourceProvider = null;
    for (const service of deployOptions.services) {
      const serviceComponent = service.components.find(
        (c) => c.type === globalComponent.type,
      );
      if (serviceComponent) {
        resourceProvider = serviceComponent.providers.find(
          (p) => p.id === globalConfig.providerId,
        );
        if (resourceProvider?.resources) break;
      }
    }

    if (!resourceProvider?.resources) return;

    const uniqueKey = getResourceSharingKey(
      "global",
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

  // Service-specific components for each enabled service
  Object.entries(formData.services).forEach(([serviceId, serviceConfig]) => {
    if (!serviceConfig.enabled) return;

    const service = deployOptions.services.find((s) => s.id === serviceId);
    if (!service) return;

    if (service.resources) {
      const serviceKey = `service-${serviceId}`;
      if (!uniqueProviders[serviceKey]) {
        uniqueProviders[serviceKey] = {
          cpu: service.resources.cpu || 0,
          memory: service.resources.memory || 0,
          storage: service.resources.storage || 0,
          accelerators: { ...(service.resources.accelerators || {}) },
        };
      }
    }

    service.components.forEach((component) => {
      const isGlobalComponent = deployOptions.global_components.some(
        (gc) => gc.type === component.type,
      );
      if (isGlobalComponent) return;

      const componentConfig = serviceConfig.components[component.type];
      if (!componentConfig) return;

      const selectedProviderId = componentConfig.providerId;
      if (!selectedProviderId) return;

      const provider = component.providers.find(
        (p) => p.id === selectedProviderId,
      );
      if (!provider?.resources) return;

      const uniqueKey = getResourceSharingKey(
        serviceId,
        component.type,
        selectedProviderId,
        componentConfig.params || {},
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

  return sumProviderResources(uniqueProviders);
};

export const DAStepTwo: React.FC<DAStepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  providerParamsByType,
  onEditingChange,
  onResourceStatusChange,
  onComponentError,
}) => {
  const { getServiceDescription } = useDeployStore();
  const serviceParamsError = useDeployStore(
    (state) => state.serviceParamsError,
  );
  const serviceParamsMap = useDeployStore((state) => state.serviceParams);

  const failedServiceNames = useMemo(() => {
    return deployOptions.services
      .filter((s) => !!serviceParamsError[s.id])
      .map((s) => s.name || s.id);
  }, [deployOptions.services, serviceParamsError]);

  useEffect(() => {
    onComponentError?.(failedServiceNames.length > 0);
  }, [failedServiceNames, onComponentError]);

  const { resources, resourcesLoading, resourcesError } = useResources();
  const calculatedResources = useMemo(
    () => calculateDARequiredResources(formData, deployOptions),
    [formData, deployOptions],
  );

  const serviceVersionOptions = useMemo(
    () => [{ id: deployOptions.version, text: deployOptions.version }],
    [deployOptions.version],
  );

  // Populate default model params once provider schemas are loaded.
  // Guarded by `if (config.params?.model) return` so this is idempotent —
  // safe to re-run whenever services or schemas change.
  useEffect(() => {
    if (Object.keys(providerParamsByType).length === 0) return;

    const serviceUpdates: Record<string, ServiceConfig> = {};
    let hasUpdates = false;

    Object.entries(formData.services).forEach(([serviceId, serviceConfig]) => {
      const componentUpdates: Record<string, ComponentConfig> = {};
      let hasComponentUpdates = false;

      Object.entries(serviceConfig.components).forEach(
        ([componentType, config]) => {
          if (config.params?.model) return;

          const paramsMap = providerParamsByType[componentType] || {};
          const cachedParams = paramsMap[config.providerId];
          const properties = cachedParams?.properties as Record<
            string,
            { default?: unknown }
          >;

          if (properties?.model?.default) {
            componentUpdates[componentType] = {
              ...config,
              params: { ...config.params, model: properties.model.default },
            };
            hasComponentUpdates = true;
          }
        },
      );

      if (hasComponentUpdates) {
        serviceUpdates[serviceId] = {
          ...serviceConfig,
          components: { ...serviceConfig.components, ...componentUpdates },
        };
        hasUpdates = true;
      }
    });

    if (hasUpdates) {
      onChange({ services: { ...formData.services, ...serviceUpdates } });
    }
  }, [providerParamsByType, formData.services, onChange]);

  const {
    options: deduplicatedLlmOptions,
    modelsWithProviders: daLlmModelsWithProviders,
  } = useMemo(
    () => buildInferenceOptions(providerParamsByType, COMPONENT_TYPES.LLM),
    [providerParamsByType],
  );

  const {
    options: deduplicatedRerankerOptions,
    modelsWithProviders: daRerankerModelsWithProviders,
  } = useMemo(
    () => buildInferenceOptions(providerParamsByType, COMPONENT_TYPES.RERANKER),
    [providerParamsByType],
  );

  // Build the services array for SharedStepTwo
  const serviceItems = useMemo((): SharedStepTwoServiceItem[] => {
    return deployOptions.services
      .flatMap((service) => {
        const serviceConfig = formData.services[service.id];
        if (!serviceConfig) return [];

        const fields: ServiceConfigField[] = [
          {
            key: "version" as keyof ServiceConfig,
            label: "Service version",
            options: serviceVersionOptions,
          },
        ];

        let llmComponent: Component | null = null;
        let rerankerComponent: Component | null = null;

        service.components.forEach((component) => {
          if (!llmComponent && component.type === COMPONENT_TYPES.LLM) {
            llmComponent = component as Component;
          }
          if (
            !rerankerComponent &&
            component.type === COMPONENT_TYPES.RERANKER
          ) {
            rerankerComponent = component as Component;
          }

          const isGlobalComponent = deployOptions.global_components.some(
            (gc) => gc.type === component.type,
          );

          if (!isGlobalComponent) {
            if (
              component.type === COMPONENT_TYPES.LLM &&
              deduplicatedLlmOptions.length > 0
            ) {
              fields.push({
                key: component.type as keyof ServiceConfig,
                label: component.name,
                options: deduplicatedLlmOptions,
                isModelFirst: true,
              });
              return;
            }
            if (
              component.type === COMPONENT_TYPES.RERANKER &&
              deduplicatedRerankerOptions.length > 0
            ) {
              fields.push({
                key: component.type as keyof ServiceConfig,
                label: component.name,
                options: deduplicatedRerankerOptions,
                isModelFirst: true,
              });
              return;
            }
          }

          const providersByDisplayName = new Map<
            string,
            (typeof component.providers)[0]
          >();
          component.providers.forEach((provider) => {
            const schema = providerParamsByType[component.type]?.[provider.id];
            const modelTitle = (
              schema?.properties as Record<
                string,
                { oneOf?: Array<{ title?: string }> }
              >
            )?.model?.oneOf?.[0]?.title;
            const displayName = modelTitle || provider.name;
            const existing = providersByDisplayName.get(displayName);
            if (!existing || (provider.default && !existing.default)) {
              providersByDisplayName.set(displayName, provider);
            }
          });

          const providers: Array<{ id: string; text: string }> = [];
          providersByDisplayName.forEach((provider, displayName) => {
            providers.push({ id: provider.id, text: displayName });
          });

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

        const serviceSchema =
          (serviceParamsMap[service.id] as ServiceParamsCache | undefined)
            ?.data ?? null;

        return [
          {
            serviceId: service.id,
            serviceName: service.name,
            description: getServiceDescription(service.id),
            config: serviceConfig,
            fields,
            inferenceComponent: llmComponent ?? rerankerComponent,
            serviceSchema,
            llmModelsWithProviders: llmComponent
              ? daLlmModelsWithProviders
              : daRerankerModelsWithProviders,
          },
        ];
      })
      .sort((a, b) => a.serviceName.localeCompare(b.serviceName));
  }, [
    deployOptions,
    formData.services,
    formData.globalComponents,
    serviceVersionOptions,
    providerParamsByType,
    serviceParamsMap,
    daLlmModelsWithProviders,
    daRerankerModelsWithProviders,
    deduplicatedLlmOptions,
    deduplicatedRerankerOptions,
    getServiceDescription,
  ]);

  const handleServiceChange = (serviceId: string, updated: ServiceConfig) => {
    onChange({ services: { ...formData.services, [serviceId]: updated } });
  };

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

      {failedServiceNames.length > 0 && (
        <InlineNotification
          kind="error"
          title={`Failed to load configuration for ${failedServiceNames.join(", ")}.`}
          subtitle="Cancel and reopen to try again."
          lowContrast
          hideCloseButton
        />
      )}

      <div className={styles.formSection}>
        <SharedStepTwo
          services={serviceItems}
          providerParamsByType={providerParamsByType}
          onChange={handleServiceChange}
          onEditingChange={onEditingChange}
        />
      </div>
    </>
  );
};
