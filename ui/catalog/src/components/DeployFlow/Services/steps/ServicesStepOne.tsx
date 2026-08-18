import { useMemo, useEffect } from "react";
import type { StepProps } from "../types";
import type { ComponentConfig } from "../../Shared/types";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import {
  SharedStepOne,
  type StepOneComponentRow,
} from "../../Shared/steps/SharedStepOne";

export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  selectedServiceId,
  showNameError = false,
  onComponentError,
}) => {
  const { getComponentModels } = useServiceDeployStore();
  const providerSchemas = useServiceDeployStore((s) => s.providerSchemas);
  const componentModels = useServiceDeployStore((s) => s.componentModels);
  const componentModelsError = useServiceDeployStore(
    (s) => s.componentModelsError,
  );

  // Collect component types whose models failed to load (Step 1 components only).
  const failedComponentTypes = useMemo(() => {
    if (!selectedServiceId || !deployOptions.components) return [];
    return deployOptions.components
      .filter(
        (c) =>
          !["llm", "reranker"].includes(c.type) &&
          !!componentModelsError[`${selectedServiceId}:${c.type}`],
      )
      .map((c) => c.name || c.type);
  }, [selectedServiceId, deployOptions.components, componentModelsError]);

  // Build component rows for SharedStepOne.
  // Shows all components EXCEPT llm and reranker — those belong in StepTwo.
  const components = useMemo<StepOneComponentRow[]>(() => {
    if (!selectedServiceId) return [];

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return [];

    const serviceComponentTypes = Object.keys(serviceConfig.components);

    return (
      deployOptions.components
        ?.filter(
          (c) =>
            serviceComponentTypes.includes(c.type) &&
            !["llm", "reranker"].includes(c.type),
        )
        .map((component) => {
          const selectedProviderId =
            serviceConfig.components[component.type]?.providerId || "";

          const schemaKey = `${selectedServiceId}:${component.type}:${selectedProviderId}`;
          const hasModelParameter =
            providerSchemas[schemaKey]?.properties?.model !== undefined;

          const models = getComponentModels(
            selectedServiceId,
            component.type,
          );
          const modelOptions = models.map((m) => ({
            id: m.id,
            text: m.text,
          }));

          return {
            type: component.type,
            name: component.name || component.type,
            hasModels: hasModelParameter && modelOptions.length > 0,
            modelOptions,
            selectedModel:
              (serviceConfig.components[component.type]?.params
                ?.model as string) || "",
            providerOptions: component.providers.map((p) => ({
              id: p.id,
              text: p.name,
            })),
            selectedProviderId,
            description: component.description,
          };
        }) ?? []
    );
  }, [
    deployOptions.components,
    formData.services,
    selectedServiceId,
    getComponentModels,
    providerSchemas,
  ]);

  // Set default model param for each component when its models arrive from the store.
  useEffect(() => {
    if (!selectedServiceId || !deployOptions.components) return;

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    const updates: Record<string, ComponentConfig> = {};
    let hasUpdates = false;

    deployOptions.components.forEach((component) => {
      const componentConfig = serviceConfig.components[component.type];
      if (!componentConfig || componentConfig.params?.model) return;

      const models =
        componentModels[`${selectedServiceId}:${component.type}`] || [];
      const matchingModel = models.find(
        (m) => m.providerId === componentConfig.providerId,
      );

      if (matchingModel) {
        updates[component.type] = {
          ...componentConfig,
          params: { ...componentConfig.params, model: matchingModel.id },
        };
        hasUpdates = true;
      }
    });

    if (hasUpdates) {
      onChange({
        services: {
          ...formData.services,
          [selectedServiceId]: {
            ...serviceConfig,
            components: { ...serviceConfig.components, ...updates },
          },
        },
      });
    }
  }, [
    selectedServiceId,
    deployOptions.components,
    formData.services,
    componentModels,
    onChange,
  ]);

  const handleProviderChange = (componentType: string, providerId: string) => {
    if (!selectedServiceId) return;
    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    onChange({
      services: {
        ...formData.services,
        [selectedServiceId]: {
          ...serviceConfig,
          components: {
            ...serviceConfig.components,
            // Clear params when provider changes so stale model selection is removed.
            [componentType]: { providerId, params: {} },
          },
        },
      },
    });
  };

  const handleModelChange = (componentType: string, model: string) => {
    if (!selectedServiceId) return;
    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    const currentComponent = serviceConfig.components[componentType];
    if (!currentComponent) return;

    // Resolve the provider that owns this model from the store.
    const models = getComponentModels(
      selectedServiceId,
      componentType,
    );
    const selectedModelOption = models.find((m) => m.id === model);
    if (!selectedModelOption) return;

    onChange({
      services: {
        ...formData.services,
        [selectedServiceId]: {
          ...serviceConfig,
          components: {
            ...serviceConfig.components,
            [componentType]: {
              ...currentComponent,
              providerId: selectedModelOption.providerId,
              params: { ...currentComponent.params, model },
            },
          },
        },
      },
    });
  };

  return (
    <SharedStepOne
      title={title}
      formData={formData}
      onChange={onChange}
      version={deployOptions.version}
      versionLabel="Service version"
      components={components}
      onComponentChange={handleProviderChange}
      onModelChange={handleModelChange}
      showNameError={showNameError}
      failedComponentNames={failedComponentTypes}
      onComponentError={onComponentError}
    />
  );
};
