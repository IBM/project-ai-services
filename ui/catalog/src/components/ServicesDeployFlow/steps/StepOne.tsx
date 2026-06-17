import {
  TextInput,
  Dropdown,
  Grid,
  Column,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
} from "@carbon/react";
import { Information } from "@carbon/icons-react";
import styles from "../ServicesDeployFlow.module.scss";
import type { StepProps } from "../types";
import { useMemo } from "react";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";

export const StepOne: React.FC<StepProps> = ({
  title,
  formData,
  onChange,
  deployOptions,
  selectedServiceId,
}) => {
  // Get component models and provider schemas from store
  const { getComponentModels } = useServiceDeployStore();
  const providerSchemas = useServiceDeployStore(
    (state) => state.providerSchemas,
  );
  // Extract version options from API response
  const versionOptions = useMemo(() => {
    return [{ id: deployOptions.version, text: deployOptions.version }];
  }, [deployOptions.version]);

  // Process service components for Step 1 (basic configuration)
  // Show all components EXCEPT llm and reranker (those are shown in Step 2)
  const serviceComponentsData = useMemo(() => {
    if (!selectedServiceId) return [];

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return [];

    // Get all component types that exist in the service
    const serviceComponentTypes = Object.keys(serviceConfig.components);

    // Find matching components from deployOptions to get provider info and display names
    // Filter out llm and reranker - they belong in Step 2 (advanced configuration)
    return deployOptions.components
      ?.filter(
        (component) =>
          serviceComponentTypes.includes(component.type) &&
          !["llm", "reranker"].includes(component.type),
      )
      .map((component) => {
        const providerOptions = component.providers.map((provider) => ({
          id: provider.id,
          text: provider.name,
        }));

        const selectedProviderId =
          serviceConfig.components[component.type]?.providerId || "";

        // Get models for this component type from store
        const componentModels = getComponentModels(
          selectedServiceId,
          component.type,
        );
        const modelOptions = componentModels.map((model) => ({
          id: model.id,
          text: model.text,
        }));

        // Check if the provider's schema actually has a model parameter
        const schemaKey = `${selectedServiceId}:${component.type}:${selectedProviderId}`;
        const providerSchema = providerSchemas[schemaKey];
        const hasModelParameter =
          providerSchema?.properties?.model !== undefined;

        // Get selected model from params
        const selectedModel =
          (serviceConfig.components[component.type]?.params?.model as string) ||
          "";

        return {
          type: component.type,
          name: component.name || component.type, // Use API-provided name
          providerOptions,
          selectedProviderId,
          modelOptions,
          selectedModel,
          hasModels: hasModelParameter && modelOptions.length > 0, // Only show if schema has model parameter
          description: component.description, // Use API-provided description if available
        };
      });
  }, [
    deployOptions.components,
    formData.services,
    selectedServiceId,
    getComponentModels,
    providerSchemas,
  ]);

  // Handle provider selection change for service components
  const handleServiceComponentChange = (
    componentType: string,
    providerId: string,
  ) => {
    if (!selectedServiceId) return;

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    // When provider changes, clear the model selection
    onChange({
      services: {
        ...formData.services,
        [selectedServiceId]: {
          ...serviceConfig,
          components: {
            ...serviceConfig.components,
            [componentType]: {
              providerId,
              params: {}, // Clear params when provider changes
            },
          },
        },
      },
    });
  };

  // Handle model selection change for service components
  const handleServiceComponentModelChange = (
    componentType: string,
    model: string,
  ) => {
    if (!selectedServiceId) return;

    const serviceConfig = formData.services[selectedServiceId];
    if (!serviceConfig) return;

    const currentComponent = serviceConfig.components[componentType];
    if (!currentComponent) return;

    // Find the correct provider for this model from the store
    const componentModels = getComponentModels(
      selectedServiceId,
      componentType,
    );
    const selectedModelOption = componentModels.find((m) => m.id === model);

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
              providerId: selectedModelOption.providerId, // Update provider to match model
              params: {
                ...currentComponent.params,
                model, // Set the model parameter
              },
            },
          },
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
          {/* Name field - always shown */}
          <Column sm={4} md={8} lg={16}>
            <div className={styles.formField}>
              <TextInput
                id="assistant-name"
                labelText="Name"
                value={formData.name}
                onChange={(e) => onChange({ name: e.target.value })}
              />
            </div>
          </Column>

          {/* Version field - always shown */}
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

          {/* Dynamic service components - ALL components from API */}
          {serviceComponentsData?.map((component) => (
            <Column key={component.type} sm={4} md={8} lg={16}>
              <div className={styles.formField}>
                {/* For components WITH models: Show ONLY the model dropdown */}
                {component.hasModels ? (
                  <Dropdown
                    id={`${component.type}-model`}
                    titleText={
                      component.description ? (
                        <div className={styles.labelWithInfo}>
                          <span>{component.name}</span>
                          <Toggletip align="top">
                            <ToggletipButton label="Additional information">
                              <Information />
                            </ToggletipButton>
                            <ToggletipContent>
                              <p>{component.description}</p>
                            </ToggletipContent>
                          </Toggletip>
                        </div>
                      ) : (
                        component.name
                      )
                    }
                    label={`Select ${component.name.toLowerCase()}`}
                    items={component.modelOptions}
                    itemToString={(item) => (item ? item.text : "")}
                    selectedItem={
                      component.modelOptions.find(
                        (m) => m.id === component.selectedModel,
                      ) || null
                    }
                    onChange={({ selectedItem }) =>
                      handleServiceComponentModelChange(
                        component.type,
                        selectedItem?.id || "",
                      )
                    }
                  />
                ) : (
                  /* For components WITHOUT models: Show provider dropdown */
                  <Dropdown
                    id={`${component.type}-provider`}
                    titleText={
                      component.description ? (
                        <div className={styles.labelWithInfo}>
                          <span>{component.name}</span>
                          <Toggletip align="top">
                            <ToggletipButton label="Additional information">
                              <Information />
                            </ToggletipButton>
                            <ToggletipContent>
                              <p>{component.description}</p>
                            </ToggletipContent>
                          </Toggletip>
                        </div>
                      ) : (
                        component.name
                      )
                    }
                    label={`Select ${component.name.toLowerCase()}`}
                    items={component.providerOptions}
                    itemToString={(item) => (item ? item.text : "")}
                    selectedItem={
                      component.providerOptions.find(
                        (p) => p.id === component.selectedProviderId,
                      ) || null
                    }
                    onChange={({ selectedItem }) =>
                      handleServiceComponentChange(
                        component.type,
                        selectedItem?.id || "",
                      )
                    }
                  />
                )}
              </div>
            </Column>
          ))}
        </Grid>
      </div>
    </>
  );
};

// Made with Bob
