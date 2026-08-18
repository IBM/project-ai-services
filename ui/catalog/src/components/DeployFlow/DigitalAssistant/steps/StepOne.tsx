import { useMemo, useEffect } from "react";
import {
  TextInput,
  Dropdown,
  Grid,
  Column,
  InlineNotification,
} from "@carbon/react";
import styles from "../DigitalAssistantDeployFlow.module.scss";
import type { StepProps } from "../types";
import type { ComponentConfig } from "../../Shared/types";
import type { ProviderSchema } from "@/types/api.types";
import { useDeployStore } from "@/store/deploy.store";

export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  showNameError = false,
  onSchemaError,
}) => {
  const isNameValid = !!formData.name.trim();

  const versionOptions = [
    { id: deployOptions.version, text: deployOptions.version },
  ];

  const providerParams = useDeployStore((state) => state.providerParams);

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

  // Detect component types whose schemas failed to load (at least one provider missing).
  const failedComponentTypes = useMemo(() => {
    return deployOptions.global_components
      .filter((component) =>
        component.providers.some(
          (provider) => !providerParams[`${component.type}:${provider.id}`],
        ),
      )
      .map((c) => c.name);
  }, [deployOptions.global_components, providerParams]);

  // Extract model names from provider schemas for display
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

  // Notify parent when schema error state changes so it can gate the Next button.
  useEffect(() => {
    onSchemaError?.(failedComponentTypes.length > 0);
  }, [failedComponentTypes, onSchemaError]);

  // Initialize default model parameters when provider params are loaded
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

  // Build component data with provider options, deduplicate by preferring default provider
  const globalComponentsData = useMemo(() => {
    return deployOptions.global_components.map((component) => {
      const providersByDisplayName = new Map<
        string,
        (typeof component.providers)[0]
      >();

      component.providers.forEach((provider) => {
        const displayName = modelNames[provider.id] || provider.name;
        const existing = providersByDisplayName.get(displayName);
        if (!existing) {
          providersByDisplayName.set(displayName, provider);
        } else if (provider.default && !existing.default) {
          providersByDisplayName.set(displayName, provider);
        }
      });

      const providerOptions: Array<{ id: string; text: string }> = [];
      providersByDisplayName.forEach((provider, displayName) => {
        providerOptions.push({ id: provider.id, text: displayName });
      });

      const selectedProviderId =
        formData.globalComponents[component.type]?.providerId || "";

      return {
        type: component.type,
        name: component.name,
        providerOptions,
        selectedProviderId,
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
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      {failedComponentTypes.length > 0 && (
        <InlineNotification
          kind="error"
          title={`Failed to load configurations of ${failedComponentTypes.join(", ")}.`}
          subtitle="Cancel and reopen to try again."
          lowContrast
          hideCloseButton
        />
      )}

      <div className={styles.formSection}>
        <Grid narrow className={styles.formGrid}>
          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <TextInput
                id="assistant-name"
                labelText="Name"
                value={formData.name}
                invalid={showNameError && !isNameValid}
                invalidText="Name is required"
                onChange={(e) => {
                  onChange({ name: e.target.value });
                }}
              />
            </div>
          </Column>

          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <Dropdown
                id="assistant-version"
                titleText="Digital assistant version"
                label="Select version"
                items={versionOptions}
                itemToString={(item) => (item ? item.text : "")}
                selectedItem={
                  versionOptions.find((v) => v.id === formData.version) || null
                }
                onChange={({ selectedItem }) =>
                  onChange({ version: selectedItem?.id || "" })
                }
              />
            </div>
          </Column>

          {globalComponentsData.map((component) => (
            <Column key={component.type} sm={4} md={8} lg={16}>
              <div className={styles.formField}>
                <Dropdown
                  id={`${component.type}-provider`}
                  titleText={component.name}
                  label={`Select ${component.name.toLowerCase()}`}
                  items={component.providerOptions}
                  itemToString={(item) => (item ? item.text : "")}
                  selectedItem={
                    component.providerOptions.find(
                      (p) => p.id === component.selectedProviderId,
                    ) || null
                  }
                  onChange={({ selectedItem }) =>
                    handleProviderChange(component.type, selectedItem?.id || "")
                  }
                />
              </div>
            </Column>
          ))}
        </Grid>
      </div>
    </>
  );
};
