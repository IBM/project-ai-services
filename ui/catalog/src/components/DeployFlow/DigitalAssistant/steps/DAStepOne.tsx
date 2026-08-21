import { useMemo, useEffect } from "react";
import type { StepProps } from "../types";
import type { ComponentConfig } from "../../Shared/types";
import { useDeployStore } from "@/store/deploy.store";
import {
  SharedStepOne,
  type StepOneComponentRow,
} from "../../Shared/steps/SharedStepOne";

export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  providerParamsByType,
  showNameError = false,
  onComponentError,
}) => {
  const providerParamsError = useDeployStore(
    (state) => state.providerParamsError,
  );
  const { getGlobalComponentModels } = useDeployStore();

  // Use the selected provider; fall back to the default so cold-open failures are still surfaced.
  const failedComponentTypes = useMemo(() => {
    return deployOptions.global_components
      .filter((component) => {
        const selectedProviderId =
          formData.globalComponents[component.type]?.providerId ||
          component.providers.find((p) => p.default)?.id ||
          component.providers[0]?.id;
        if (!selectedProviderId) return false;
        return !!providerParamsError[`${component.type}:${selectedProviderId}`];
      })
      .map((c) => c.name);
  }, [
    deployOptions.global_components,
    formData.globalComponents,
    providerParamsError,
  ]);

  // providerId → display label for the provider-first fallback (hasModels: false).
  const modelNames = useMemo(() => {
    const result: Record<string, string> = {};
    deployOptions.global_components.forEach((component) => {
      getGlobalComponentModels(component.type).forEach((m) => {
        if (!result[m.providerId]) result[m.providerId] = m.text;
      });
    });
    return result;
  }, [deployOptions.global_components, getGlobalComponentModels]);

  // Set default model selection when model options arrive and nothing is selected yet.
  useEffect(() => {
    const updates: Record<string, ComponentConfig> = {};
    let hasUpdates = false;

    deployOptions.global_components.forEach((component) => {
      const config = formData.globalComponents[component.type];
      if (!config || config.params?.model) return;

      const models = getGlobalComponentModels(component.type);
      if (models.length === 0) return;

      // Prefer the default provider's first model, fall back to first overall.
      const defaultProviderId =
        component.providers.find((p) => p.default)?.id ??
        component.providers[0]?.id;
      const defaultModel =
        models.find((m) => m.providerId === defaultProviderId) ?? models[0];

      updates[component.type] = {
        providerId: defaultModel.providerId,
        params: { model: defaultModel.id },
      };
      hasUpdates = true;
    });

    if (hasUpdates) {
      onChange({
        globalComponents: { ...formData.globalComponents, ...updates },
      });
    }
  }, [
    deployOptions.global_components,
    formData.globalComponents,
    getGlobalComponentModels,
    onChange,
  ]);

  // Build component rows for SharedStepOne.
  const components = useMemo<StepOneComponentRow[]>(() => {
    return deployOptions.global_components.map((component) => {
      const models = getGlobalComponentModels(component.type);
      const selectedModel =
        (formData.globalComponents[component.type]?.params?.model as string) ||
        "";

      // Deduplicate by id — multiple providers can expose the same model const.
      const seen = new Set<string>();
      const modelOptions: Array<{ id: string; text: string }> = [];
      models.forEach((m) => {
        if (!seen.has(m.id)) {
          seen.add(m.id);
          modelOptions.push({ id: m.id, text: m.text });
        }
      });

      return {
        type: component.type,
        name: component.name,
        hasModels: modelOptions.length > 0,
        modelOptions,
        selectedModel,
        providerOptions: component.providers.map((p) => ({
          id: p.id,
          text: modelNames[p.id] || p.name,
        })),
        selectedProviderId:
          formData.globalComponents[component.type]?.providerId || "",
      };
    });
  }, [
    deployOptions.global_components,
    formData.globalComponents,
    getGlobalComponentModels,
    modelNames,
  ]);

  // Resolve provider from the selected model and update formData.
  const handleModelChange = (componentType: string, modelId: string) => {
    const models = getGlobalComponentModels(componentType);
    const selected = models.find((m) => m.id === modelId);
    if (!selected) return;

    onChange({
      globalComponents: {
        ...formData.globalComponents,
        [componentType]: {
          providerId: selected.providerId,
          params: { model: modelId },
        },
      },
    });
  };

  const handleProviderChange = (componentType: string, providerId: string) => {
    const paramsMap = providerParamsByType[componentType] || {};
    const cachedParams = paramsMap[providerId];
    const properties = cachedParams?.properties as Record<
      string,
      { default?: unknown }
    >;
    const modelParam: Record<string, unknown> = {};
    if (properties?.model?.default) {
      modelParam.model = properties.model.default;
    }

    onChange({
      globalComponents: {
        ...formData.globalComponents,
        [componentType]: { providerId, params: modelParam },
      },
    });
  };

  return (
    <SharedStepOne
      title={title}
      formData={formData}
      onChange={onChange}
      version={deployOptions.version}
      versionLabel="Digital assistant version"
      components={components}
      onComponentChange={handleProviderChange}
      onModelChange={handleModelChange}
      showNameError={showNameError}
      failedComponentNames={failedComponentTypes}
      onComponentError={onComponentError}
    />
  );
};
