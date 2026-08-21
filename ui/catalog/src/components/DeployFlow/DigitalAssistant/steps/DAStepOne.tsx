import { useMemo, useEffect } from "react";
import type { StepProps } from "../types";
import type { ComponentConfig } from "../../Shared/types";
import type { ProviderSchema } from "@/types/api.types";
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
  showNameError = false,
  onComponentError,
}) => {
  const providerParams = useDeployStore((state) => state.providerParams);
  const providerParamsError = useDeployStore(
    (state) => state.providerParamsError,
  );

  // Provider schemas from store, keyed by componentType → providerId.
  const paramsByType = useMemo(() => {
    const result: Record<string, Record<string, ProviderSchema>> = {};
    deployOptions.global_components.forEach((component) => {
      result[component.type] = {};
      component.providers.forEach((provider) => {
        const cached = providerParams[`${component.type}:${provider.id}`];
        if (cached) result[component.type][provider.id] = cached.data;
      });
    });
    return result;
  }, [deployOptions.global_components, providerParams]);

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

  // Extract model names from provider schemas for display in the dropdown labels.
  const modelNames = useMemo(() => {
    const result: Record<string, string> = {};
    Object.entries(paramsByType).forEach(([_componentType, paramsMap]) => {
      Object.entries(paramsMap).forEach(([providerId, params]) => {
        const properties = params?.properties as Record<
          string,
          { oneOf?: Array<{ title?: string }> }
        >;
        const modelTitle = properties?.model?.oneOf?.[0]?.title;
        if (modelTitle) result[providerId] = modelTitle;
      });
    });
    return result;
  }, [paramsByType]);

  // Set default model param for each component when its provider schema loads.
  useEffect(() => {
    if (Object.keys(paramsByType).length === 0) return;

    const updates: Record<string, ComponentConfig> = {};
    let hasUpdates = false;

    Object.entries(formData.globalComponents).forEach(
      ([componentType, config]) => {
        if (config.params?.model) return;

        const paramsMap = paramsByType[componentType] || {};
        const cachedParams = paramsMap[config.providerId];
        const properties = cachedParams?.properties as Record<
          string,
          { default?: unknown }
        >;

        if (properties?.model?.default) {
          updates[componentType] = {
            ...config,
            params: { ...config.params, model: properties.model.default },
          };
          hasUpdates = true;
        }
      },
    );

    if (hasUpdates) {
      onChange({
        globalComponents: { ...formData.globalComponents, ...updates },
      });
    }
  }, [paramsByType, formData.globalComponents, onChange]);

  // Build component rows — deduplicate providers by display name, preferring the default.
  const components = useMemo<StepOneComponentRow[]>(() => {
    return deployOptions.global_components.map((component) => {
      const byDisplayName = new Map<string, (typeof component.providers)[0]>();

      component.providers.forEach((provider) => {
        const displayName = modelNames[provider.id] || provider.name;
        const existing = byDisplayName.get(displayName);
        if (!existing || (provider.default && !existing.default)) {
          byDisplayName.set(displayName, provider);
        }
      });

      const providerOptions: Array<{ id: string; text: string }> = [];
      byDisplayName.forEach((provider, displayName) => {
        providerOptions.push({ id: provider.id, text: displayName });
      });

      return {
        type: component.type,
        name: component.name,
        // TODO(PR 8c): switch to model-first selection — hasModels will become true here.
        hasModels: false,
        modelOptions: [],
        selectedModel: "",
        providerOptions,
        selectedProviderId:
          formData.globalComponents[component.type]?.providerId || "",
      };
    });
  }, [deployOptions.global_components, formData.globalComponents, modelNames]);

  const handleProviderChange = (componentType: string, providerId: string) => {
    const paramsMap = paramsByType[componentType] || {};
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
      showNameError={showNameError}
      failedComponentNames={failedComponentTypes}
      onComponentError={onComponentError}
    />
  );
};
