import { Fragment, useMemo, useState } from "react";
import {
  Button,
  Dropdown,
  TextInput,
  Accordion,
  AccordionItem,
} from "@carbon/react";
import { ProductiveCard } from "@carbon/ibm-products";
import { Checkmark, Edit, View, ViewOff } from "@carbon/icons-react";
import styles from "../DeployFlow.module.scss";
import type { ServiceConfig } from "../types";
import type { ServiceConfigField } from "../types/StepTwo.types";
import { getDisplayName } from "../utils/StepTwo.utils";
import type { useBatchProviderParams } from "@/hooks/useProviderParams";
import { DynamicSchemaFields } from "./DynamicSchemaFields";
import type { Component } from "@/types/digitalAssistants";
import { parseSchema, validateField } from "@/utils/schemaParser";
import { useServiceParams } from "@/hooks/useServiceParams";
import { shouldShowParam } from "@/utils/paramFilter";

interface ServiceConfigCardProps {
  serviceId: string;
  serviceName: string;
  config: ServiceConfig;
  description: string;
  fields: ServiceConfigField[];
  isEditing: boolean;
  currentConfig: ServiceConfig | null;
  providerParamsByType: Record<
    string,
    ReturnType<typeof useBatchProviderParams>
  >;
  llmComponent: Component | null;
  rerankerComponent: Component | null;
  onEdit: () => void;
  onApply: () => void;
  onCancel: () => void;
  onUpdateConfig: (updates: Partial<ServiceConfig>) => void;
}

export const ServiceConfigCard: React.FC<ServiceConfigCardProps> = ({
  serviceId,
  serviceName,
  config,
  description,
  fields,
  isEditing,
  currentConfig,
  providerParamsByType,
  llmComponent,
  rerankerComponent,
  onEdit,
  onApply,
  onCancel,
  onUpdateConfig,
}) => {
  const [showPasswords, setShowPasswords] = useState<Record<string, boolean>>(
    {},
  );
  const [hasValidationError, setHasValidationError] = useState(false);

  // Fetch service-level schema
  const { params: serviceSchema } = useServiceParams(serviceId);

  // Helper function to get model description from provider schema
  const getModelDescription = (
    componentType: string,
    providerId: string | undefined,
    modelId: string | undefined,
  ) => {
    if (!modelId || !providerId) {
      return null;
    }

    const paramsMap = providerParamsByType[componentType]?.paramsMap || {};
    const providerSchema = paramsMap[providerId];

    if (
      !providerSchema ||
      !providerSchema.properties ||
      typeof providerSchema.properties !== "object"
    ) {
      return null;
    }

    const properties = providerSchema.properties as Record<
      string,
      {
        oneOf?: Array<{
          const?: string;
          description?: string;
        }>;
      }
    >;

    if (!properties.model?.oneOf) {
      return null;
    }

    const modelOption = properties.model.oneOf.find(
      (option) => option.const === modelId,
    );

    return modelOption?.description || null;
  };

  // Helper function to parse model description into structured sections
  const parseModelDescription = (description: string) => {
    const sections: {
      introduction?: string;
      useCases?: string;
      languages?: string;
      strengths?: string;
    } = {};

    // Split by ** markers to find section titles
    const parts = description.split(/\*\*(.*?)\*\*/g);

    // First part (index 0) is the introduction text before any ** markers
    if (parts[0] && parts[0].trim()) {
      sections.introduction = parts[0].trim();
    }

    // Process the rest of the parts (section titles and content)
    for (let i = 1; i < parts.length; i += 2) {
      const title = parts[i].trim().replace(/:$/, ""); // Remove trailing colon
      let content = parts[i + 1]?.trim() || "";

      // Remove leading colon and whitespace from content
      content = content.replace(/^:\s*/, "");

      if (title && content) {
        // Map section titles to keys
        if (title.toLowerCase().includes("use case")) {
          sections.useCases = content;
        } else if (title.toLowerCase().includes("language")) {
          sections.languages = content;
        } else if (title.toLowerCase().includes("strength")) {
          sections.strengths = content;
        }
      }
    }

    return sections;
  };

  // Parse service-level schema fields
  const serviceFields = useMemo(() => {
    if (!serviceSchema) return [];
    return parseSchema(
      serviceSchema as import("@/utils/schemaParser").JSONSchema,
    );
  }, [serviceSchema]);

  const togglePasswordVisibility = (key: string) => {
    setShowPasswords((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
  };

  // Validate inference backend parameters
  const validateInferenceBackendParams = (): boolean => {
    if (!currentConfig?.inferenceBackend || !currentConfig?.params) {
      return true; // No params to validate
    }

    // TODO: [Next Release] Replace hardcoded "llm"/"reranker" with constants from a shared file
    const componentType = llmComponent ? "llm" : "reranker";
    const paramsMap = providerParamsByType[componentType]?.paramsMap || {};
    const schema = paramsMap[currentConfig.inferenceBackend];

    if (!schema || !schema.properties) {
      return true; // No schema to validate against
    }

    // Parse schema to get field definitions with validation rules
    const fields = parseSchema(
      schema as import("@/utils/schemaParser").JSONSchema,
    );

    // Validate each field
    for (const field of fields) {
      if (field.key === "model") continue; // Skip model field

      const value = currentConfig.params[field.key];
      const error = validateField(value, field);

      if (error) {
        return false; // Validation failed
      }
    }

    return true; // All validations passed
  };

  // Handle Apply with validation
  const handleApplyWithValidation = () => {
    const isValid = validateInferenceBackendParams();

    if (!isValid) {
      setHasValidationError(true);
      return;
    }

    setHasValidationError(false);
    onApply();
  };

  // Compute available inference backend options based on selected model compatibility
  const inferenceBackendField = useMemo(() => {
    const component = llmComponent || rerankerComponent;
    if (!component) return null;

    // TODO: [Next Release] Replace hardcoded "llm"/"reranker" with constants from a shared file
    const componentType = llmComponent ? "llm" : "reranker";
    const selectedModel =
      currentConfig?.components?.[componentType]?.params?.model;
    const paramsMap = providerParamsByType[componentType]?.paramsMap || {};

    // Filter providers compatible with the selected model
    const inferenceBackendOptions = component.providers
      .filter((provider) => {
        if (!selectedModel) return true;

        const providerSchema = paramsMap[provider.id];
        if (!providerSchema || !providerSchema.properties) return false;

        const properties = providerSchema.properties as Record<
          string,
          { default?: unknown }
        >;
        const providerDefaultModel = properties.model?.default;

        return providerDefaultModel === selectedModel;
      })
      .map((provider) => ({
        id: provider.id,
        text: provider.name,
      }));

    return {
      key: "inferenceBackend" as keyof ServiceConfig,
      label: "Inference backend",
      options: inferenceBackendOptions,
    };
  }, [
    llmComponent,
    rerankerComponent,
    currentConfig?.components,
    providerParamsByType,
  ]);
  return (
    <ProductiveCard
      title={serviceName}
      description={description}
      className={styles.serviceConfigCard}
    >
      {!isEditing && (
        <div className={styles.cardEditAction}>
          <Button
            kind="ghost"
            size="sm"
            renderIcon={Edit}
            iconDescription="Edit"
            onClick={onEdit}
          >
            Edit
          </Button>
        </div>
      )}
      {isEditing && (
        <div className={styles.cardEditAction}>
          <Button
            kind="ghost"
            size="sm"
            onClick={() => {
              setHasValidationError(false);
              onCancel();
            }}
          >
            Cancel
          </Button>
          <Button
            kind="tertiary"
            size="sm"
            onClick={handleApplyWithValidation}
            renderIcon={Checkmark}
          >
            Apply
          </Button>
        </div>
      )}

      {!isEditing ? (
        <div className={styles.serviceConfigContent}>
          {fields.map((field) => {
            let value: string | undefined;

            // Determine field value based on field type
            if (field.globalValue !== undefined) {
              value = field.globalValue;
            } else if (field.key === "version") {
              value = config.version;
            } else if (field.key === "inferenceBackend") {
              value = config.inferenceBackend;
            } else if (config.components && config.components[field.key]) {
              value = config.components[field.key].providerId;
            }

            const displayValue = getDisplayName(
              String(value || ""),
              field.options,
            );
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

          {/* Render component configuration parameters */}
          {fields.map((field) => {
            if (
              field.key === "version" ||
              field.readonly ||
              field.key === "inferenceBackend"
            )
              return null;

            const componentConfig = config.components?.[field.key];
            if (!componentConfig?.params) return null;

            const paramsMap = providerParamsByType[field.key]?.paramsMap || {};
            const schema = paramsMap[componentConfig.providerId];

            if (!schema?.properties) return null;

            return Object.entries(componentConfig.params)
              .filter(([key, value]) => shouldShowParam(key, value, schema))
              .map(([key, value]) => {
                const property = (
                  schema.properties as Record<
                    string,
                    { title?: string; format?: string }
                  >
                )[key];
                const label = property?.title || key;
                const isPassword = property?.format === "password";

                return (
                  <div
                    key={`${field.key}-${key}`}
                    className={styles.serviceConfigItem}
                  >
                    <span className={styles.serviceConfigItemLabel}>
                      {label}
                    </span>
                    <span className={styles.serviceConfigItemValue}>
                      {isPassword ? (
                        <>
                          <span className={styles.apiKeyValue}>
                            {showPasswords[`${field.key}-${key}`]
                              ? String(value)
                              : "•".repeat(20)}
                          </span>
                          <Button
                            kind="ghost"
                            size="sm"
                            hasIconOnly
                            renderIcon={
                              showPasswords[`${field.key}-${key}`]
                                ? ViewOff
                                : View
                            }
                            iconDescription={
                              showPasswords[`${field.key}-${key}`]
                                ? "Hide"
                                : "Show"
                            }
                            onClick={() =>
                              togglePasswordVisibility(`${field.key}-${key}`)
                            }
                            className={styles.apiKeyToggle}
                          />
                        </>
                      ) : (
                        String(value)
                      )}
                    </span>
                  </div>
                );
              });
          })}

          {/* Render inference backend service-level parameters */}
          {config.inferenceBackend &&
            config.params &&
            Object.keys(config.params).length > 0 &&
            (() => {
              // TODO: [Next Release] Replace hardcoded "llm"/"reranker" with constants from a shared file
              const componentType = llmComponent ? "llm" : "reranker";
              const paramsMap =
                providerParamsByType[componentType]?.paramsMap || {};
              const schema = paramsMap[config.inferenceBackend];

              if (!schema?.properties) return null;

              // Filter out params that are already shown in service-level schema fields
              const serviceFieldKeys = new Set(serviceFields.map((f) => f.key));

              const excludeKeys = new Set(["model", ...serviceFieldKeys]);
              return Object.entries(config.params)
                .filter(([key, value]) =>
                  shouldShowParam(key, value, schema, excludeKeys),
                )
                .map(([key, value]) => {
                  const property = (
                    schema.properties as Record<
                      string,
                      { title?: string; format?: string }
                    >
                  )[key];
                  const label = property?.title || key;
                  const isPassword = property?.format === "password";

                  return (
                    <div
                      key={`service-${key}`}
                      className={styles.serviceConfigItem}
                    >
                      <span className={styles.serviceConfigItemLabel}>
                        {label}
                      </span>
                      <span className={styles.serviceConfigItemValue}>
                        {isPassword ? (
                          <>
                            <span className={styles.apiKeyValue}>
                              {showPasswords[`service-${key}`]
                                ? String(value)
                                : "•".repeat(20)}
                            </span>
                            <Button
                              kind="ghost"
                              size="sm"
                              hasIconOnly
                              renderIcon={
                                showPasswords[`service-${key}`] ? ViewOff : View
                              }
                              iconDescription={
                                showPasswords[`service-${key}`]
                                  ? "Hide"
                                  : "Show"
                              }
                              onClick={() =>
                                togglePasswordVisibility(`service-${key}`)
                              }
                              className={styles.apiKeyToggle}
                            />
                          </>
                        ) : (
                          String(value)
                        )}
                      </span>
                    </div>
                  );
                });
            })()}

          {/* Render service-level schema fields (only non-UI-only fields with non-default values) */}
          {serviceFields
            .filter((field) => {
              // Skip UI-only fields in view mode
              if (field.uiOnly) return false;

              // Only show controlled fields if they differ from default
              if (field.controlledBy) {
                const currentValue = config.params?.[field.key];
                const hasValue =
                  currentValue !== undefined && currentValue !== null;
                const isDifferentFromDefault =
                  hasValue && currentValue !== field.defaultValue;
                return isDifferentFromDefault;
              }

              // Show other fields if they have a value
              return config.params?.[field.key] !== undefined;
            })
            .map((field) => {
              const value = config.params?.[field.key];
              const isPassword = field.type === "password";

              return (
                <div
                  key={`service-field-${field.key}`}
                  className={styles.serviceConfigItem}
                >
                  <span className={styles.serviceConfigItemLabel}>
                    {field.label}
                  </span>
                  <span className={styles.serviceConfigItemValue}>
                    {isPassword ? (
                      <>
                        <span className={styles.apiKeyValue}>
                          {showPasswords[`service-field-${field.key}`]
                            ? String(value)
                            : "•".repeat(20)}
                        </span>
                        <Button
                          kind="ghost"
                          size="sm"
                          hasIconOnly
                          renderIcon={
                            showPasswords[`service-field-${field.key}`]
                              ? ViewOff
                              : View
                          }
                          iconDescription={
                            showPasswords[`service-field-${field.key}`]
                              ? "Hide"
                              : "Show"
                          }
                          onClick={() =>
                            togglePasswordVisibility(
                              `service-field-${field.key}`,
                            )
                          }
                          className={styles.apiKeyToggle}
                        />
                      </>
                    ) : (
                      String(value)
                    )}
                  </span>
                </div>
              );
            })}
        </div>
      ) : (
        <>
          <div className={styles.serviceConfigFieldRow}>
            {fields
              .filter((f) => f.key !== "inferenceBackend")
              .map((field, index) => {
                let fieldValue: string | undefined;

                // Determine field value for editing mode
                if (field.globalValue !== undefined) {
                  fieldValue = field.globalValue;
                } else if (field.key === "version") {
                  fieldValue = currentConfig?.version;
                } else if (
                  currentConfig?.components &&
                  currentConfig.components[field.key]
                ) {
                  fieldValue = currentConfig.components[field.key].providerId;
                }

                const selectedItem =
                  field.options.find((opt) => opt.id === fieldValue) || null;

                return (
                  <Fragment key={`${field.key}-${index}`}>
                    <div className={field.readonly ? styles.readonlyField : ""}>
                      {field.readonly ? (
                        <TextInput
                          id={`${serviceName}-${field.key}`}
                          labelText={field.label}
                          value={selectedItem?.text || ""}
                          readOnly
                        />
                      ) : (
                        <Dropdown
                          id={`${serviceName}-${field.key}`}
                          titleText={field.label}
                          label={`Select ${field.label.toLowerCase()}`}
                          invalid={!selectedItem}
                          invalidText={`${field.label} is required`}
                          items={field.options}
                          itemToString={(item) => (item ? item.text : "")}
                          selectedItem={selectedItem}
                          onChange={({ selectedItem }) => {
                            if (field.key === "version") {
                              onUpdateConfig({
                                version: selectedItem?.id || "",
                              });
                            } else {
                              const providerId = selectedItem?.id || "";

                              // Extract default model from provider schema
                              const paramsMap =
                                providerParamsByType[field.key]?.paramsMap ||
                                {};
                              const cachedParams = paramsMap[providerId];
                              const modelParam: Record<string, unknown> = {};

                              if (
                                cachedParams &&
                                typeof cachedParams === "object" &&
                                "properties" in cachedParams &&
                                cachedParams.properties &&
                                typeof cachedParams.properties === "object"
                              ) {
                                const properties =
                                  cachedParams.properties as Record<
                                    string,
                                    { default?: unknown }
                                  >;
                                if (properties.model?.default) {
                                  modelParam.model = properties.model.default;
                                }
                              }

                              onUpdateConfig({
                                components: {
                                  ...currentConfig?.components,
                                  [field.key]: {
                                    providerId,
                                    params: modelParam,
                                  },
                                },
                              });
                            }
                          }}
                        />
                      )}
                    </div>
                    {index === 0 && <div />}
                  </Fragment>
                );
              })}

            {/* Render inference backend dropdown and parameters */}
            {inferenceBackendField &&
              (() => {
                const fieldValue = currentConfig?.inferenceBackend;
                const selectedItem =
                  inferenceBackendField.options.find(
                    (opt) => opt.id === fieldValue,
                  ) || null;

                // Dynamically determine component type based on which component exists
                const componentType = llmComponent ? "llm" : "reranker";

                return (
                  <Fragment key="inferenceBackend">
                    <div>
                      <Dropdown
                        id={`${serviceName}-inferenceBackend`}
                        titleText={inferenceBackendField.label}
                        label={`Select ${inferenceBackendField.label.toLowerCase()}`}
                        invalid={!selectedItem}
                        invalidText={`${inferenceBackendField.label} is required`}
                        items={inferenceBackendField.options}
                        itemToString={(item) => (item ? item.text : "")}
                        selectedItem={selectedItem}
                        onChange={({ selectedItem }) => {
                          // Get service-level param keys to preserve (dynamic, backend-driven)
                          const serviceFieldKeys = new Set(
                            serviceFields.map((f) => f.key),
                          );

                          // Preserve only service-level params, clear provider-specific params
                          const preservedParams: Record<string, unknown> = {};
                          if (currentConfig?.params) {
                            Object.entries(currentConfig.params).forEach(
                              ([key, value]) => {
                                if (serviceFieldKeys.has(key)) {
                                  preservedParams[key] = value;
                                }
                              },
                            );
                          }

                          onUpdateConfig({
                            inferenceBackend: selectedItem?.id || "",
                            params: preservedParams, // Keep service-level, clear provider params
                          });
                        }}
                      />
                    </div>
                    <div />
                    <div style={{ gridColumn: "1 / -1" }}>
                      <DynamicSchemaFields
                        componentType={componentType}
                        providerId={fieldValue || ""}
                        values={currentConfig?.params || {}}
                        onChange={(params) => {
                          // Clear validation error when user makes changes
                          if (hasValidationError) {
                            setHasValidationError(false);
                          }
                          // Get provider schema keys to know which params belong to provider
                          const providerSchema =
                            providerParamsByType[componentType]?.paramsMap?.[
                              fieldValue || ""
                            ];
                          const providerKeys = new Set(
                            providerSchema?.properties
                              ? Object.keys(providerSchema.properties)
                              : [],
                          );

                          // Preserve service-level params, update only provider params
                          const mergedParams: Record<string, unknown> = {};
                          Object.entries(currentConfig?.params || {}).forEach(
                            ([key, value]) => {
                              // Keep service-level params (not in provider schema)
                              if (!providerKeys.has(key)) {
                                mergedParams[key] = value;
                              }
                            },
                          );
                          // Add/update provider params from DynamicSchemaFields
                          Object.entries(params).forEach(([key, value]) => {
                            mergedParams[key] = value;
                          });

                          onUpdateConfig({
                            params: mergedParams,
                          });
                        }}
                        providerParamsMap={
                          (providerParamsByType[componentType]?.paramsMap ||
                            {}) as Record<
                            string,
                            import("@/utils/schemaParser").JSONSchema
                          >
                        }
                        hasValidationError={hasValidationError}
                      />
                    </div>
                  </Fragment>
                );
              })()}

            {/* Render service-level schema fields in edit mode */}
            {serviceFields.length > 0 && serviceSchema && (
              <div style={{ gridColumn: "1 / -1", width: "100%" }}>
                <DynamicSchemaFields
                  componentType={serviceId}
                  providerId={serviceId}
                  values={currentConfig?.params || {}}
                  onChange={(params) => {
                    // Clear validation error when user makes changes
                    if (hasValidationError) {
                      setHasValidationError(false);
                    }
                    // Get service schema keys to know which params belong to service
                    const serviceSchemaTyped =
                      serviceSchema as import("@/utils/schemaParser").JSONSchema;
                    const serviceKeys = new Set(
                      serviceSchemaTyped?.properties
                        ? Object.keys(serviceSchemaTyped.properties)
                        : [],
                    );

                    // Preserve provider params, update only service-level params
                    const mergedParams: Record<string, unknown> = {};
                    Object.entries(currentConfig?.params || {}).forEach(
                      ([key, value]) => {
                        // Keep provider params (not in service schema)
                        if (!serviceKeys.has(key)) {
                          mergedParams[key] = value;
                        }
                      },
                    );
                    // Add/update service params from DynamicSchemaFields
                    Object.entries(params).forEach(([key, value]) => {
                      mergedParams[key] = value;
                    });

                    onUpdateConfig({
                      params: mergedParams,
                    });
                  }}
                  providerParamsMap={{
                    [serviceId]:
                      serviceSchema as import("@/utils/schemaParser").JSONSchema,
                  }}
                  hasValidationError={hasValidationError}
                />
              </div>
            )}

            {/* Model Description Accordion - In edit mode */}
            {(llmComponent || rerankerComponent) &&
              (() => {
                // TODO: [Next Release] Replace hardcoded "llm"/"reranker" with constants from a shared file
                const componentType = llmComponent ? "llm" : "reranker";
                const providerId =
                  currentConfig?.components?.[componentType]?.providerId;
                const modelId = currentConfig?.components?.[componentType]
                  ?.params?.model as string | undefined;

                if (!modelId || !providerId) return null;

                const modelDescription = getModelDescription(
                  componentType,
                  providerId,
                  modelId,
                );

                if (!modelDescription) return null;

                const sections = parseModelDescription(modelDescription);

                if (
                  !sections.introduction &&
                  !sections.useCases &&
                  !sections.strengths &&
                  !sections.languages
                ) {
                  return null;
                }

                return (
                  <div
                    style={{ gridColumn: "1 / -1" }}
                    className={styles.modelDescriptionSection}
                  >
                    <Accordion>
                      <AccordionItem title="What is this model good at?">
                        <div className={styles.modelDescriptionContent}>
                          {/* Introduction - Full width at top */}
                          {sections.introduction && (
                            <div className={styles.modelDescriptionFullWidth}>
                              <p className={styles.modelDescriptionText}>
                                {sections.introduction}
                              </p>
                            </div>
                          )}

                          {/* Use Cases - Full width (if exists separately) */}
                          {sections.useCases && (
                            <div className={styles.modelDescriptionFullWidth}>
                              <p className={styles.modelDescriptionText}>
                                {sections.useCases}
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
          </div>
        </>
      )}
    </ProductiveCard>
  );
};
