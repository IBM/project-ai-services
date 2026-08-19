import { useState, useMemo, useCallback } from "react";
import { sumProviderResources } from "../../Shared/utils/resources";
import { parseSchema, validateField } from "@/utils/schemaParser";
import {
  Button,
  Dropdown,
  TextInput,
  InlineLoading,
  Accordion,
  AccordionItem,
} from "@carbon/react";
import type { ServiceDeployOptions } from "@/types/api.types";
import { useResources } from "../../Shared/hooks/useResources";
import { useProviderSchema } from "../hooks/useProviderSchema";
import { useServiceDeployStore } from "@/store/serviceDeploy.store";
import { ProductiveCard } from "@carbon/ibm-products";
import { Checkmark, Edit } from "@carbon/icons-react";
import styles from "../ServicesDeployFlow.module.scss";
import type { StepProps } from "../types";
import type { ServiceConfig, DeployFormData } from "../../Shared/types";
import {
  ResourceRequirementsPanel,
  type CalculatedResources,
} from "../../Shared/components/ResourceRequirementsPanel";
import { DynamicSchemaFields } from "../../Shared/components/DynamicSchemaFields";
import { ProviderCredentialDisplay } from "../../Shared/components/ProviderCredentialDisplay";
import { getDisplayName } from "../../Shared/utils/displayHelpers";

const calculateRequiredResources = (
  formData: DeployFormData,
  deployOptions: ServiceDeployOptions,
): CalculatedResources => {
  // Key: providerId-componentType — ensures vllm-cpu counts separately per component role
  const uniqueProviders: Record<
    string,
    {
      cpu: number;
      memory: number;
      storage: number;
      accelerators: Record<string, number>;
    }
  > = {};

  Object.entries(formData.services).forEach(([_serviceKey, serviceConfig]) => {
    if (!serviceConfig.enabled) return;

    if (deployOptions.resources) {
      const serviceResourceKey = `service-${_serviceKey}`;
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

export const StepTwo: React.FC<StepProps> = ({
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
}) => {
  const { resources, resourcesLoading, resourcesError } = useResources();
  const [editingService, setEditingService] = useState<string | null>(null);
  const [tempConfig, setTempConfig] = useState<ServiceConfig | null>(null);
  const [showValidationError, setShowValidationError] = useState(false);

  // Get component models from store for all component types
  const componentModels = useServiceDeployStore(
    (state) => state.componentModels,
  );

  // Get the service configuration for the selected service
  const selectedServiceConfig = selectedServiceId
    ? formData.services[selectedServiceId]
    : null;

  // Get the current inference method (LLM provider) to fetch its schema
  const currentLlmProviderId =
    tempConfig?.components?.llm?.providerId ||
    selectedServiceConfig?.components?.llm?.providerId;

  // Get provider schema from store (already cached when LLM options were fetched)
  const { schema: providerSchema } = useProviderSchema(
    selectedServiceId || null,
    currentLlmProviderId ? "llm" : null,
    currentLlmProviderId || null,
  );

  const calculatedResources = useMemo(
    () => calculateRequiredResources(formData, deployOptions),
    [formData, deployOptions],
  );

  // Extract service version options from API response
  const serviceVersionOptions = useMemo(() => {
    return [{ id: deployOptions.version, text: deployOptions.version }];
  }, [deployOptions.version]);

  // Helper function to get component providers by type
  const getComponentProviders = useCallback(
    (componentType: string) => {
      const component = deployOptions.components.find(
        (c) => c.type === componentType,
      );
      return (
        component?.providers.map((provider) => ({
          id: provider.id,
          text: provider.name,
        })) || []
      );
    },
    [deployOptions.components],
  );

  // Get LLM options from fetched models with provider information
  // Deduplicate for display (users should only see each model once)
  const llmOptions = useMemo(() => {
    // Don't fallback to providers - wait for actual model data
    if (llmModelsWithProviders.length === 0) {
      return [];
    }

    // Deduplicate models for display - show each model only once
    const uniqueModels = llmModelsWithProviders.reduce(
      (acc, option) => {
        const exists = acc.some((existing) => existing.id === option.id);
        if (!exists) {
          acc.push({
            id: option.id,
            text: option.text,
          });
        }
        return acc;
      },
      [] as Array<{ id: string; text: string }>,
    );

    return uniqueModels;
  }, [llmModelsWithProviders]);

  // Get the currently selected LLM model
  const selectedLlmModel = useMemo(() => {
    return (
      tempConfig?.components?.llm?.params?.model ||
      selectedServiceConfig?.components?.llm?.params?.model
    );
  }, [tempConfig, selectedServiceConfig]);

  // Inference method options - filtered based on selected LLM model
  const inferenceMethodOptions = useMemo(() => {
    // If no LLM model is selected, show all providers
    if (!selectedLlmModel) {
      return getComponentProviders("llm");
    }

    // Find which providers support the selected model
    const supportingProviders = llmModelsWithProviders
      .filter((option) => option.id === selectedLlmModel)
      .map((option) => option.providerId);

    // Filter providers to only show those that support the selected model
    const allProviders = getComponentProviders("llm");
    return allProviders.filter((provider) =>
      supportingProviders.includes(provider.id),
    );
  }, [selectedLlmModel, llmModelsWithProviders, getComponentProviders]);

  // Set default LLM model if not already set and options are available

  // Validates LLM credential fields. TODO PR 8a: extend to reranker once eager schema fetching lands.
  const validateAllFields = useCallback((): {
    isValid: boolean;
    errors: Record<string, string>;
  } => {
    if (!providerSchema || !tempConfig?.components?.llm) {
      return { isValid: true, errors: {} };
    }

    const llmParams = tempConfig.components.llm.params || {};
    const errors: Record<string, string> = {};

    parseSchema(providerSchema).forEach((field) => {
      if (field.key === "model") return;
      const error = validateField(llmParams[field.key], field);
      if (error) {
        errors[field.key] = error;
      }
    });

    return {
      isValid: Object.keys(errors).length === 0,
      errors,
    };
  }, [providerSchema, tempConfig]);

  // Re-computed only when validation state, schema, or tempConfig change —
  // not on every keystroke render.
  const fieldErrors = useMemo(
    () => (showValidationError ? validateAllFields().errors : {}),
    [showValidationError, validateAllFields],
  );

  const handleEdit = useCallback(() => {
    if (selectedServiceConfig && selectedServiceId) {
      setTempConfig({ ...selectedServiceConfig });
      setEditingService(selectedServiceId);
      setShowValidationError(false);
      onEditingChange?.(true);
    }
  }, [selectedServiceConfig, selectedServiceId, onEditingChange]);

  const handleApply = useCallback(() => {
    const { isValid } = validateAllFields();
    if (!isValid) {
      setShowValidationError(true);
      return; // Stay in edit mode
    }

    if (tempConfig && selectedServiceId) {
      onChange({
        services: {
          ...formData.services,
          [selectedServiceId]: tempConfig,
        },
      });
    }
    setEditingService(null);
    setTempConfig(null);
    setShowValidationError(false);
    onEditingChange?.(false);
  }, [
    validateAllFields,
    tempConfig,
    selectedServiceId,
    onChange,
    formData.services,
    onEditingChange,
  ]);

  const handleCancel = useCallback(() => {
    setEditingService(null);
    setTempConfig(null);
    setShowValidationError(false);
    onEditingChange?.(false);
  }, [onEditingChange]);

  const updateTempConfig = useCallback(
    (updates: Partial<ServiceConfig>) => {
      if (tempConfig) {
        setTempConfig({ ...tempConfig, ...updates });
      }
    },
    [tempConfig],
  );

  // Helper function to get model description from provider schema
  const getModelDescription = (modelId: string | undefined) => {
    if (
      !modelId ||
      !providerSchema ||
      !providerSchema.properties?.model?.oneOf
    ) {
      return null;
    }

    const modelOption = providerSchema.properties.model.oneOf.find(
      (option) => option.const === modelId,
    );

    return modelOption?.description || null;
  };

  // Helper function to parse model description into structured sections
  const parseModelDescription = (description: string) => {
    const sections: {
      mainDescription?: string;
      languages?: string;
      strengths?: string;
    } = {};

    // Split by ** markers to find section titles
    const parts = description.split(/\*\*(.*?)\*\*/g);

    // First part (before any ** markers) is the main description
    if (parts[0]) {
      sections.mainDescription = parts[0].trim();
    }

    // Parse remaining sections
    for (let i = 1; i < parts.length; i += 2) {
      const title = parts[i].trim().replace(/:$/, ""); // Remove trailing colon
      const content = parts[i + 1]?.trim().replace(/^:\s*/, "") || ""; // Remove leading colon and whitespace

      if (title && content) {
        // Map section titles to keys
        if (title.toLowerCase().includes("language")) {
          sections.languages = content;
        } else if (title.toLowerCase().includes("strength")) {
          sections.strengths = content;
        }
      }
    }

    return sections;
  };

  // Build service configuration fields dynamically from API
  const serviceFields = useMemo(() => {
    if (!selectedServiceConfig) return [];

    const fields: Array<{
      key: string;
      label: string;
      options: Array<{ id: string; text: string }>;
      readonly?: boolean;
      isGlobal?: boolean;
      isInferenceMethod?: boolean;
    }> = [];

    // Always add version first
    fields.push({
      key: "version",
      label: "Service version",
      options: serviceVersionOptions,
    });

    // Add component fields dynamically from API
    deployOptions.components.forEach((component) => {
      const providerOptions = component.providers.map((provider) => ({
        id: provider.id,
        text: provider.name,
      }));

      // Get model options for this component type from store
      const componentKey = `${selectedServiceId}:${component.type}`;
      const modelOptions = componentModels[componentKey] || [];
      const hasModels = modelOptions.length > 0;

      // Special handling for LLM: Add both Inference Method and LLM Model fields
      if (component.type === "llm") {
        // 1. Add LLM Model field first (shows model names)
        if (llmOptions.length > 0) {
          fields.push({
            key: component.type,
            label: component.name || "Large language model (LLM)",
            options: llmOptions,
          });
        }

        // 2. Add Inference Method field second (shows provider names)
        // Use inferenceMethodOptions which is filtered based on selected model
        fields.push({
          key: "llm_provider",
          label: "Inference backend",
          options: inferenceMethodOptions,
          isInferenceMethod: true,
        });
      }
      // For components with models (embedding, reranker): show model dropdown
      else if (hasModels) {
        fields.push({
          key: component.type,
          label:
            component.name ||
            `${component.type.charAt(0).toUpperCase() + component.type.slice(1).replace("_", " ")} model`,
          options: modelOptions,
        });
      }
      // For components without models (vector_store): show provider dropdown
      else {
        fields.push({
          key: component.type,
          label: component.name || component.type.replace("_", " "),
          options: providerOptions,
        });
      }
    });

    return fields;
  }, [
    selectedServiceConfig,
    deployOptions.components,
    serviceVersionOptions,
    llmOptions,
    inferenceMethodOptions,
    componentModels,
    selectedServiceId,
  ]);

  const renderServiceConfig = () => {
    if (!selectedServiceConfig || !selectedServiceId) return null;

    // Show loading state if LLM models are being fetched or not yet available
    const isLlmComponent = deployOptions.components.some(
      (c) => c.type === "llm",
    );

    // Check if we're still loading OR if we have LLM component but no options yet
    const isLoadingLlmOptions =
      isLlmComponent &&
      (isLoadingLlmModels ||
        (llmModelsWithProviders.length === 0 && llmOptions.length === 0));

    if (isLoadingLlmOptions) {
      return (
        <div className={styles.loadingContainer}>
          <InlineLoading description="Loading configuration options..." />
        </div>
      );
    }

    const isEditing = editingService === selectedServiceId;
    const currentConfig = isEditing ? tempConfig : selectedServiceConfig;

    return (
      <ProductiveCard
        title={deployOptions.name}
        description={serviceDescription}
        className={styles.serviceConfigCard}
      >
        {!isEditing && (
          <div className={styles.cardEditAction}>
            <Button
              kind="ghost"
              size="sm"
              renderIcon={Edit}
              iconDescription="Edit"
              onClick={handleEdit}
            >
              Edit
            </Button>
          </div>
        )}
        {isEditing && (
          <div className={styles.cardActions}>
            <Button kind="ghost" size="sm" onClick={handleCancel}>
              Cancel
            </Button>
            <Button
              kind="tertiary"
              size="sm"
              renderIcon={Checkmark}
              onClick={handleApply}
            >
              Apply
            </Button>
          </div>
        )}

        {!isEditing ? (
          <div className={styles.serviceConfigContent}>
            {serviceFields.map((field) => {
              let value: string | undefined;

              // Handle version field
              if (field.key === "version") {
                value = selectedServiceConfig.version;
              }
              // Handle inference method (LLM provider)
              else if (field.isInferenceMethod) {
                value = selectedServiceConfig.components.llm?.providerId;
              }
              // Handle LLM model (stored in params.model)
              else if (field.key === "llm") {
                value = selectedServiceConfig.components.llm?.params?.model as
                  | string
                  | undefined;
                // Use first option as fallback if no value is set
                if (!value && field.options.length > 0) {
                  value = field.options[0].id;
                }
              }
              // Handle components with models (embedding, reranker) - show model from params
              else if (
                selectedServiceConfig.components[field.key]?.params?.model
              ) {
                value = selectedServiceConfig.components[field.key].params
                  .model as string;
              }
              // Handle components without models (vector_store) - show provider
              else if (selectedServiceConfig.components[field.key]) {
                value = selectedServiceConfig.components[field.key].providerId;
              }

              // For inference method display, use complete provider list instead of filtered options
              // to ensure provider ID can always be mapped to name
              // For LLM model display, use llmOptions to ensure model ID can be mapped to name
              const displayOptions = field.isInferenceMethod
                ? getComponentProviders("llm")
                : field.key === "llm"
                  ? llmOptions
                  : field.options;
              const displayValue = getDisplayName(value, displayOptions);

              return (
                <div key={field.key} className={styles.serviceConfigItem}>
                  <span className={styles.serviceConfigItemLabel}>
                    {field.label}
                  </span>
                  <span className={styles.serviceConfigItemValue}>
                    {displayValue}
                  </span>
                </div>
              );
            })}

            {/* Show cloud credentials dynamically based on provider schema */}
            {currentLlmProviderId && providerSchema && (
              <ProviderCredentialDisplay
                contextId={selectedServiceId}
                providerSchema={providerSchema}
                values={selectedServiceConfig.components.llm?.params || {}}
                className={styles.serviceConfigItem}
              />
            )}
          </div>
        ) : (
          <>
            {/* Service version - half width */}
            <div className={styles.serviceConfigFieldRow}>
              {serviceFields
                .filter((f) => f.key === "version")
                .map((field) => {
                  const fieldValue = currentConfig?.version;
                  const selectedItem =
                    field.options.find((opt) => opt.id === fieldValue) || null;

                  return (
                    <div
                      key={field.key}
                      className={styles.serviceConfigFieldHalf}
                    >
                      <Dropdown
                        id={`${selectedServiceId}-${field.key}`}
                        titleText={field.label}
                        label={`Select ${field.label.toLowerCase()}`}
                        invalid={!selectedItem}
                        invalidText={`Provide a valid ${field.label}`}
                        items={field.options}
                        itemToString={(item) => (item ? item.text : "")}
                        selectedItem={selectedItem}
                        onChange={({ selectedItem }) => {
                          updateTempConfig({
                            version: selectedItem?.id || "",
                          });
                        }}
                      />
                    </div>
                  );
                })}
            </div>

            {/* Embedding model and Vector store - side by side (read-only) */}
            <div className={styles.serviceConfigFieldRow}>
              {serviceFields
                .filter(
                  (f) => f.key === "embedding" || f.key === "vector_store",
                )
                .sort((a, b) => {
                  // Sort to ensure embedding comes before vector_store
                  if (a.key === "embedding") return -1;
                  if (b.key === "embedding") return 1;
                  return 0;
                })
                .map((field) => {
                  let fieldValue: string | undefined;

                  if (currentConfig?.components[field.key]?.params?.model) {
                    fieldValue = currentConfig.components[field.key].params
                      .model as string;
                  } else if (currentConfig?.components[field.key]) {
                    fieldValue = currentConfig.components[field.key].providerId;
                  }

                  const selectedItem =
                    field.options.find((opt) => opt.id === fieldValue) || null;

                  return (
                    <div
                      key={field.key}
                      className={styles.serviceConfigFieldHalf}
                    >
                      <TextInput
                        id={`${selectedServiceId}-${field.key}`}
                        labelText={field.label}
                        value={selectedItem?.text || ""}
                        readOnly
                      />
                    </div>
                  );
                })}
            </div>

            {/* Large language model and Inference backend - side by side */}
            <div className={styles.serviceConfigFieldRow}>
              {serviceFields
                .filter((f) => f.key === "llm" || f.isInferenceMethod)
                .map((field) => {
                  let fieldValue: string | undefined;

                  if (field.isInferenceMethod) {
                    fieldValue = currentConfig?.components.llm?.providerId;
                  } else if (field.key === "llm") {
                    fieldValue = currentConfig?.components.llm?.params
                      ?.model as string | undefined;
                    if (!fieldValue && field.options.length > 0) {
                      fieldValue = field.options[0].id;
                    }
                  }

                  const selectedItem =
                    field.options.find((opt) => opt.id === fieldValue) || null;

                  return (
                    <div
                      key={field.key}
                      className={styles.serviceConfigFieldHalf}
                    >
                      <Dropdown
                        id={`${selectedServiceId}-${field.key}`}
                        titleText={field.label}
                        label={`Select ${field.label.toLowerCase()}`}
                        invalid={!selectedItem}
                        invalidText={`Provide a valid ${field.label}`}
                        items={field.options}
                        itemToString={(item) => (item ? item.text : "")}
                        selectedItem={selectedItem}
                        onChange={({ selectedItem }) => {
                          if (field.isInferenceMethod) {
                            updateTempConfig({
                              components: {
                                ...currentConfig?.components,
                                llm: {
                                  providerId: selectedItem?.id || "",
                                  params: currentConfig?.components.llm?.params
                                    ?.model
                                    ? {
                                        model:
                                          currentConfig.components.llm.params
                                            .model,
                                      }
                                    : {},
                                },
                              },
                            });
                          } else if (field.key === "llm") {
                            const llmComponent = currentConfig?.components.llm;
                            const newModelId = selectedItem?.id || "";

                            const supportingProviders = llmModelsWithProviders
                              .filter((option) => option.id === newModelId)
                              .map((option) => option.providerId);

                            const currentProviderId =
                              llmComponent?.providerId || "";
                            const isCurrentProviderCompatible =
                              supportingProviders.includes(currentProviderId);

                            const newProviderId = isCurrentProviderCompatible
                              ? currentProviderId
                              : supportingProviders[0] || "";

                            updateTempConfig({
                              components: {
                                ...currentConfig?.components,
                                llm: {
                                  providerId: newProviderId,
                                  params: {
                                    model: newModelId,
                                  },
                                },
                              },
                            });
                          }
                        }}
                      />
                    </div>
                  );
                })}
            </div>

            {/* Cloud credentials section - dynamically rendered based on provider schema */}
            {currentLlmProviderId && providerSchema && (
              <div className={styles.cloudCredentialsSection}>
                {/* Show header for all providers */}
                <h4 className={styles.cloudCredentialsTitle}>
                  {currentLlmProviderId.toLowerCase().includes("watsonx")
                    ? "Cloud credentials"
                    : "Inference credentials"}
                </h4>
                <DynamicSchemaFields
                  componentType="llm"
                  providerId={currentLlmProviderId}
                  values={currentConfig?.components.llm?.params || {}}
                  onChange={(updates) => {
                    const llmComponent = currentConfig?.components.llm;
                    updateTempConfig({
                      components: {
                        ...currentConfig?.components,
                        llm: {
                          providerId: llmComponent?.providerId || "",
                          params: updates,
                        },
                      },
                    });
                  }}
                  providerParamsMap={{ [currentLlmProviderId]: providerSchema }}
                  hasValidationError={showValidationError}
                  fieldErrors={fieldErrors}
                />
              </div>
            )}

            {/* Model description section - show "What is this model good at?" at the bottom */}
            {currentConfig?.components.llm?.params?.model &&
              providerSchema &&
              (() => {
                const modelDescription = getModelDescription(
                  currentConfig.components.llm.params.model as string,
                );

                if (!modelDescription) return null;

                const sections = parseModelDescription(modelDescription);

                if (
                  !sections.mainDescription &&
                  !sections.strengths &&
                  !sections.languages
                ) {
                  return null;
                }

                return (
                  <div className={styles.modelDescriptionSection}>
                    <Accordion>
                      <AccordionItem title="What is this model good at?">
                        <div className={styles.modelDescriptionContent}>
                          {/* Main Description - Full width at top */}
                          {sections.mainDescription && (
                            <div className={styles.modelDescriptionFullWidth}>
                              <p className={styles.modelDescriptionText}>
                                {sections.mainDescription}
                              </p>
                            </div>
                          )}

                          {/* Strengths and Languages - Side by side */}
                          {(sections.strengths || sections.languages) && (
                            <div className={styles.modelDescriptionRow}>
                              {sections.strengths && (
                                <div className={styles.modelDescriptionHalf}>
                                  <h5 className={styles.modelDescriptionTitle}>
                                    Model strengths
                                  </h5>
                                  <p className={styles.modelDescriptionText}>
                                    {sections.strengths}
                                  </p>
                                </div>
                              )}

                              {sections.languages && (
                                <div className={styles.modelDescriptionHalf}>
                                  <h5 className={styles.modelDescriptionTitle}>
                                    Supported languages
                                  </h5>
                                  <p className={styles.modelDescriptionText}>
                                    {sections.languages}
                                  </p>
                                </div>
                              )}
                            </div>
                          )}
                        </div>
                      </AccordionItem>
                    </Accordion>
                  </div>
                );
              })()}
          </>
        )}
      </ProductiveCard>
    );
  };

  return (
    <>
      <div className={styles.stepHeader}>
        <h2 className={styles.stepTitle}>{title}</h2>
      </div>

      {/* Resource Requirements */}
      <ResourceRequirementsPanel
        calculatedResources={calculatedResources}
        resourceData={resources}
        resourcesLoading={resourcesLoading}
        resourcesError={resourcesError}
        onResourceStatusChange={onResourceStatusChange}
      />

      {/* Service Configuration - Dynamically rendered */}
      <div className={styles.formSection}>{renderServiceConfig()}</div>
    </>
  );
};
