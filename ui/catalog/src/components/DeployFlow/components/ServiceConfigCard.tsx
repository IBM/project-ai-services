import { Fragment, useMemo, useState } from "react";
import { Button, Dropdown, TextInput } from "@carbon/react";
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

interface ServiceConfigCardProps {
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
              .filter(([key]) => key !== "model")
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
              const componentType = llmComponent ? "llm" : "reranker";
              const paramsMap =
                providerParamsByType[componentType]?.paramsMap || {};
              const schema = paramsMap[config.inferenceBackend];

              if (!schema?.properties) return null;

              return Object.entries(config.params)
                .filter(([key]) => key !== "model")
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
                          onUpdateConfig({
                            inferenceBackend: selectedItem?.id || "",
                          });
                        }}
                      />
                    </div>
                    <div />
                    <div style={{ gridColumn: "1 / -1" }}>
                      <DynamicSchemaFields
                        componentType="llm"
                        providerId={fieldValue || ""}
                        values={currentConfig?.params || {}}
                        onChange={(params) => {
                          // Clear validation error when user makes changes
                          if (hasValidationError) {
                            setHasValidationError(false);
                          }
                          onUpdateConfig({
                            params: {
                              ...currentConfig?.params,
                              ...params,
                            },
                          });
                        }}
                        providerParamsMap={
                          (providerParamsByType["llm"]?.paramsMap ||
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
          </div>
        </>
      )}
    </ProductiveCard>
  );
};
