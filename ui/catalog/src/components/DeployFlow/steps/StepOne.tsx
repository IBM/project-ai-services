import { useMemo, useEffect } from "react";
import { TextInput, Dropdown, Grid, Column } from "@carbon/react";
import styles from "../DeployFlow.module.scss";
import type { StepProps, ComponentConfig } from "../types";
import { useBatchProviderParams } from "@/hooks/useProviderParams";

export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  showNameError = false,
}) => {
  const isNameValid = !!formData.name.trim();

  const versionOptions = [
    { id: deployOptions.version, text: deployOptions.version },
  ];

  // Collect provider IDs for batch parameter fetching
  const providerIdsByType = useMemo(() => {
    const result: Record<string, string[]> = {};
    deployOptions.global_components.forEach((component) => {
      result[component.type] = component.providers.map((p) => p.id);
    });
    return result;
  }, [deployOptions.global_components]);

  // Dynamically fetch provider parameters for all component types
  const componentTypes = Object.keys(providerIdsByType);
  const providerParamsHooks = componentTypes.reduce(
    (acc, type) => {
      // eslint-disable-next-line react-hooks/rules-of-hooks
      acc[type] = useBatchProviderParams(type, providerIdsByType[type] || []);
      return acc;
    },
    {} as Record<string, ReturnType<typeof useBatchProviderParams>>,
  );

  const providerParamsByType = useMemo(
    () => providerParamsHooks,
    [providerParamsHooks],
  );

  // Extract model names from provider schemas for display
  const modelNames = useMemo(() => {
    const newModelNames: Record<string, string> = {};

    Object.entries(providerParamsByType).forEach(([_componentType, data]) => {
      const paramsMap = data.paramsMap || {};

      Object.entries(paramsMap).forEach(([providerId, params]) => {
        const properties = params?.properties as Record<
          string,
          { oneOf?: Array<{ title?: string }> }
        >;

        const modelTitle = properties?.model?.oneOf?.[0]?.title;

        if (modelTitle) {
          newModelNames[providerId] = modelTitle;
        }
      });
    });

    return newModelNames;
  }, [providerParamsByType]);

  // Initialize default model parameters when provider params are loaded
  useEffect(() => {
    if (Object.keys(providerParamsByType).length === 0) return;

    const updates: Record<string, ComponentConfig> = {};
    let hasUpdates = false;

    Object.entries(formData.globalComponents).forEach(
      ([componentType, config]) => {
        if (config.params?.model) return;

        const paramsMap = providerParamsByType[componentType]?.paramsMap || {};
        const cachedParams = paramsMap[config.providerId];
        const properties = cachedParams?.properties as Record<
          string,
          { default?: unknown }
        >;

        if (properties?.model?.default) {
          updates[componentType] = {
            ...config,
            params: {
              ...config.params,
              model: properties.model.default,
            },
          };
          hasUpdates = true;
        }
      },
    );

    if (hasUpdates) {
      onChange({
        globalComponents: {
          ...formData.globalComponents,
          ...updates,
        },
      });
    }
  }, [providerParamsByType, formData.globalComponents, onChange]);

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
          // First provider with this display name
          providersByDisplayName.set(displayName, provider);
        } else if (provider.default && !existing.default) {
          // Replace with default provider if current one isn't default
          providersByDisplayName.set(displayName, provider);
        }
      });

      const providerOptions: Array<{ id: string; text: string }> = [];
      providersByDisplayName.forEach((provider, displayName) => {
        providerOptions.push({
          id: provider.id,
          text: displayName,
        });
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
    // Extract default model from provider schema
    const paramsMap = providerParamsByType[componentType]?.paramsMap || {};
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
        [componentType]: {
          providerId,
          params: modelParam,
        },
      },
    });
  };

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

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
