import { Fragment, useMemo, useState } from "react";
import {
  Button,
  Dropdown,
  TextInput,
  Accordion,
  AccordionItem,
} from "@carbon/react";
import { ProductiveCard } from "@carbon/ibm-products";
import { Edit, View, ViewOff } from "@carbon/icons-react";
import styles from "../DeployFlow.shared.module.scss";
import type { ServiceConfig, ServiceConfigField } from "../types";
import { getDisplayName } from "../utils/displayHelpers";
import { DynamicSchemaFields } from "./DynamicSchemaFields";
import type {
  DeployOptionsComponent as Component,
  ProviderSchema,
  JSONSchema,
  LLMOption,
} from "@/types/api.types";
import { parseSchema, validateField } from "@/utils/schemaParser";
import { shouldShowParam } from "../utils/paramFilter";
import type { ComponentType } from "@/constants";

export interface ServiceConfigCardProps {
  serviceId: string;
  serviceName: string;
  config: ServiceConfig;
  description: string;
  fields: ServiceConfigField[];
  isEditing: boolean;
  currentConfig: ServiceConfig | null;
  providerParamsByType: Record<string, Record<string, ProviderSchema>>;
  /** The LLM or reranker component for this service. Null when the service has neither. */
  inferenceComponent: Component | null;
  /** Service-level schema passed from the parent — no store access inside this component. */
  serviceSchema: JSONSchema | null;
  /**
   * Model→provider mapping used to auto-select a compatible inference backend
   * when the user picks an LLM model. Both flows supply this; DA derives it from
   * providerParamsByType, Services fetches it via useServiceDeployOptions.
   */
  llmModelsWithProviders: LLMOption[];
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
  inferenceComponent,
  serviceSchema,
  llmModelsWithProviders,
  onEdit,
  onApply,
  onCancel,
  onUpdateConfig,
}) => {
  // Which component type is the inference component for this service.
  // Null when the service has no LLM or reranker.
  const inferenceComponentType: ComponentType | null = inferenceComponent
    ? (inferenceComponent.type as ComponentType)
    : null;

  const [showPasswords, setShowPasswords] = useState<Record<string, boolean>>(
    {},
  );
  const [hasValidationError, setHasValidationError] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  // Helper function to get model description from provider schema
  const getModelDescription = (
    componentType: string,
    providerId: string | undefined,
    modelId: string | undefined,
  ) => {
    if (!modelId || !providerId) {
      return null;
    }

    const paramsMap = providerParamsByType[componentType] || {};
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

    const parts = description.split(/\*\*(.*?)\*\*/g);

    if (parts[0] && parts[0].trim()) {
      sections.introduction = parts[0].trim();
    }

    for (let i = 1; i < parts.length; i += 2) {
      const title = parts[i].trim().replace(/:$/, "");
      let content = parts[i + 1]?.trim() || "";
      content = content.replace(/^:\s*/, "");

      if (title && content) {
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
    return parseSchema(serviceSchema);
  }, [serviceSchema]);

  const togglePasswordVisibility = (key: string) => {
    setShowPasswords((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
  };

  const buildFieldErrors = (): Record<string, string> => {
    if (!currentConfig?.params) {
      return {};
    }

    const errors: Record<string, string> = {};

    // Validate provider credential fields when an inference component is selected.
    const currentInferenceProviderId = inferenceComponentType
      ? currentConfig.components?.[inferenceComponentType]?.providerId
      : undefined;
    if (currentInferenceProviderId && inferenceComponentType) {
      const paramsMap = providerParamsByType[inferenceComponentType] || {};
      const providerSchema = paramsMap[currentInferenceProviderId];

      if (providerSchema?.properties) {
        const providerFields = parseSchema(providerSchema);
        for (const field of providerFields) {
          if (field.key === "model") continue;
          const error = validateField(currentConfig.params[field.key], field);
          if (error) {
            errors[field.key] = error;
          }
        }
      }
    }

    // Validate service-level schema fields (always, when schema is available)
    if (serviceSchema?.properties) {
      const svcFields = parseSchema(serviceSchema);
      for (const field of svcFields) {
        const error = validateField(currentConfig.params[field.key], field);
        if (error) {
          errors[field.key] = error;
        }
      }
    }

    // Validate credential fields for custom component providers.
    // Credentials are stored in componentConfig.params (not serviceConfig.params).
    for (const field of fields) {
      if (!field.isCustomComponent) continue;
      const componentType = String(field.key);
      const selectedProviderId =
        currentConfig.components?.[componentType]?.providerId ?? "";
      if (!selectedProviderId) continue;
      const customSchema =
        providerParamsByType[componentType]?.[selectedProviderId];
      if (!customSchema?.properties) continue;
      const customFields = parseSchema(customSchema);
      for (const f of customFields) {
        if (f.key === "model") continue;
        const value =
          currentConfig.components?.[componentType]?.params?.[f.key];
        const error = validateField(value, f);
        if (error) {
          errors[f.key] = error;
        }
      }
    }

    return errors;
  };

  const handleApplyWithValidation = () => {
    const errors = buildFieldErrors();

    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      setHasValidationError(true);
      return;
    }

    setFieldErrors({});
    setHasValidationError(false);
    setShowPasswords({});
    onApply();
  };

  // Compute available inference backend options based on selected model compatibility.
  // Uses llmModelsWithProviders (the model→provider map) to find which providers
  // support the currently selected model — consistent with handleLlmModelChange.
  const inferenceBackendField = useMemo(() => {
    if (!inferenceComponent || !inferenceComponentType) return null;

    const selectedModel =
      currentConfig?.components?.[inferenceComponentType]?.params?.model;

    // When no model is selected show all providers; otherwise filter to only
    // those that carry the selected model in llmModelsWithProviders.
    const supportingProviderIds = selectedModel
      ? new Set(
          llmModelsWithProviders
            .filter((opt) => opt.id === selectedModel)
            .map((opt) => opt.providerId),
        )
      : null;

    const inferenceBackendOptions = inferenceComponent.providers
      .filter((provider) =>
        supportingProviderIds ? supportingProviderIds.has(provider.id) : true,
      )
      .map((provider) => ({
        id: provider.id,
        text: provider.name,
      }));

    return {
      label: "LLM inference backend",
      options: inferenceBackendOptions,
    };
  }, [
    inferenceComponent,
    inferenceComponentType,
    currentConfig?.components,
    llmModelsWithProviders,
  ]);

  /**
   * Handle LLM model selection — auto-select a compatible inference backend.
   * Keeps the current backend if it supports the newly selected model;
   * otherwise switches to the first compatible backend. Matches the pattern
   * already in Services/StepTwo (lines 701–728).
   */
  // Only called for isModelFirst LLM fields (Services flow).
  // Auto-selects a compatible inference backend provider for the chosen model.
  const handleLlmModelChange = (fieldKey: string, newModelId: string) => {
    const supportingProviders = llmModelsWithProviders
      .filter((option) => option.id === newModelId)
      .map((option) => option.providerId);

    const currentProviderId =
      currentConfig?.components?.[fieldKey]?.providerId || "";
    const isCurrentProviderCompatible =
      supportingProviders.includes(currentProviderId);

    const newProviderId = isCurrentProviderCompatible
      ? currentProviderId
      : (supportingProviders[0] ?? "");

    onUpdateConfig({
      components: {
        ...currentConfig?.components,
        [fieldKey]: {
          providerId: newProviderId,
          params: { model: newModelId },
        },
      },
    });
  };

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
            kind="secondary"
            size="sm"
            onClick={() => {
              setHasValidationError(false);
              setShowPasswords({});
              onCancel();
            }}
          >
            Cancel
          </Button>
          <Button kind="primary" size="sm" onClick={handleApplyWithValidation}>
            Save
          </Button>
        </div>
      )}

      {!isEditing ? (
        <div className={styles.serviceConfigContent}>
          {fields.map((field) => {
            // Custom component: model row first, then backend row (matches LLM pattern)
            if (field.isCustomComponent) {
              const modelId = config.components?.[field.key]?.params?.model as
                | string
                | undefined;
              const modelName =
                field.options.find((m) => m.id === modelId)?.text ??
                modelId ??
                "";
              const providerId = config.components?.[field.key]?.providerId;
              const providerName =
                field.providerOptions?.find((p) => p.id === providerId)?.text ??
                providerId ??
                "";
              const componentParams =
                config.components?.[field.key]?.params ?? {};
              const customComponentSchemas =
                providerParamsByType[String(field.key)] ?? {};
              const customProviderSchema = providerId
                ? customComponentSchemas[providerId]
                : undefined;

              // No model options and no schema → show only the backend/provider row
              // (same pattern as vector store).
              const hasModelOptions = field.options.length > 0;
              return (
                <Fragment key={field.key}>
                  {hasModelOptions && (
                    <div className={styles.serviceConfigItem}>
                      <span className={styles.serviceConfigItemLabel}>
                        {field.label}
                      </span>
                      <span className={styles.serviceConfigItemValue}>
                        {modelName}
                      </span>
                    </div>
                  )}
                  <div className={styles.serviceConfigItem}>
                    <span className={styles.serviceConfigItemLabel}>
                      {hasModelOptions ? `${field.label} Backend` : field.label}
                    </span>
                    <span className={styles.serviceConfigItemValue}>
                      {providerName}
                    </span>
                  </div>
                  {/* Credential params for the selected custom component backend (read-only).
                      Iterate schema properties (not componentParams) so required fields
                      that are still empty show "Required — Click Edit to add" instead of
                      being silently invisible. */}
                  {customProviderSchema?.properties &&
                    Object.entries(
                      customProviderSchema.properties as Record<
                        string,
                        { title?: string; format?: string; default?: unknown }
                      >,
                    )
                      .filter(([key]) => key !== "model")
                      .map(([key, property]) => {
                        const value = componentParams[key];
                        const label = property?.title || key;
                        const isPassword = property?.format === "password";
                        const isValueEmpty =
                          value === undefined ||
                          value === null ||
                          String(value).trim() === "";
                        const isRequired = Array.isArray(
                          customProviderSchema.required,
                        )
                          ? customProviderSchema.required.includes(key)
                          : false;
                        if (
                          !isValueEmpty &&
                          property?.default !== undefined &&
                          value === property.default
                        ) {
                          return null;
                        }
                        if (isValueEmpty && !isRequired) return null;

                        return (
                          <div
                            key={`${String(field.key)}-cred-${key}`}
                            className={styles.serviceConfigItem}
                          >
                            <span className={styles.serviceConfigItemLabel}>
                              {label}
                            </span>
                            <span className={styles.serviceConfigItemValue}>
                              {isValueEmpty ? (
                                <span className={styles.requiredMessage}>
                                  Required — Click Edit to add
                                </span>
                              ) : isPassword ? (
                                <>
                                  <span className={styles.apiKeyValue}>
                                    {showPasswords[
                                      `${String(field.key)}-cred-${key}`
                                    ]
                                      ? String(value)
                                      : "•".repeat(20)}
                                  </span>
                                  <Button
                                    kind="ghost"
                                    size="sm"
                                    hasIconOnly
                                    renderIcon={
                                      showPasswords[
                                        `${String(field.key)}-cred-${key}`
                                      ]
                                        ? ViewOff
                                        : View
                                    }
                                    iconDescription={
                                      showPasswords[
                                        `${String(field.key)}-cred-${key}`
                                      ]
                                        ? "Hide"
                                        : "Show"
                                    }
                                    onClick={() =>
                                      togglePasswordVisibility(
                                        `${String(field.key)}-cred-${key}`,
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
                </Fragment>
              );
            }

            let value: string | undefined;

            if (field.globalValue !== undefined) {
              value = field.globalValue;
            } else if (field.key === "version") {
              value = config.version;
            } else if (field.isModelFirst) {
              value = config.components?.[field.key]?.params?.model as
                | string
                | undefined;
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

          {/* Inference backend row (read-only view) */}
          {inferenceComponent &&
            inferenceComponentType &&
            (() => {
              const providerId =
                config.components?.[inferenceComponentType]?.providerId;
              if (!providerId) return null;
              const provider = inferenceComponent.providers.find(
                (p) => p.id === providerId,
              );
              if (!provider) return null;
              return (
                <div
                  key="inferenceBackend-readonly"
                  className={styles.serviceConfigItem}
                >
                  <span className={styles.serviceConfigItemLabel}>
                    LLM inference backend
                  </span>
                  <span className={styles.serviceConfigItemValue}>
                    {provider.name}
                  </span>
                </div>
              );
            })()}

          {/* Render component configuration parameters */}
          {fields.map((field) => {
            if (field.key === "version" || field.readonly) return null;
            // Custom components render their own credential rows inside the isCustomComponent
            // Fragment above — skip them here to avoid showing params twice.
            if (field.isCustomComponent) return null;

            const componentConfig = config.components?.[field.key];
            if (!componentConfig?.params) return null;

            const paramsMap = providerParamsByType[field.key] || {};
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
          {inferenceComponentType &&
            config.components?.[inferenceComponentType]?.providerId &&
            config.params &&
            Object.keys(config.params).length > 0 &&
            (() => {
              const inferenceProviderId =
                config.components![inferenceComponentType].providerId;
              const paramsMap =
                providerParamsByType[inferenceComponentType] || {};
              const schema = paramsMap[inferenceProviderId];

              if (!schema?.properties) return null;

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

          {/* Render service-level schema fields (non-UI-only fields with non-default values,
              plus any required fields that are missing so they can show an error message) */}
          {serviceFields
            .filter((field) => {
              if (field.uiOnly) return false;
              if (field.controlledBy) {
                const currentValue = config.params?.[field.key];
                const hasValue =
                  currentValue !== undefined && currentValue !== null;
                const isDifferentFromDefault =
                  hasValue && currentValue !== field.defaultValue;
                return isDifferentFromDefault;
              }
              // Always show required fields so the "Required — Click Edit to add"
              // message is visible even when the value is absent.
              if (field.validation?.required) return true;
              return config.params?.[field.key] !== undefined;
            })
            .map((field) => {
              const value = config.params?.[field.key];
              const isPassword = field.type === "password";

              const isValueEmpty =
                value === undefined ||
                value === null ||
                String(value).trim() === "";
              const isRequiredMissing =
                field.validation?.required && isValueEmpty;

              return (
                <div
                  key={`service-field-${field.key}`}
                  className={styles.serviceConfigItem}
                >
                  <span className={styles.serviceConfigItemLabel}>
                    {field.label}
                  </span>
                  <span className={styles.serviceConfigItemValue}>
                    {isRequiredMissing ? (
                      <span className={styles.requiredMessage}>
                        Required — Click Edit to add
                      </span>
                    ) : isPassword ? (
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
            {fields.map((field, index) => {
              // Custom component: model-first then backend — exactly mirrors LLM flow.
              // field.options = flat model list across all providers (LLMOption[]).
              // field.providerOptions = all providers for the backend dropdown.
              if (field.isCustomComponent) {
                const selectedModelId = currentConfig?.components?.[field.key]
                  ?.params?.model as string | undefined;
                const selectedModelItem =
                  field.options.find((m) => m.id === selectedModelId) ?? null;

                const selectedProviderId =
                  currentConfig?.components?.[field.key]?.providerId ?? "";

                // Find providers that carry the selected model by checking each
                // provider's schema in providerParamsByType. This correctly handles
                // the case where the same model const appears in multiple providers —
                // unlike field.options which is deduplicated by fetchComponentModelsWithSchemas.
                const componentSchemas =
                  providerParamsByType[String(field.key)] ?? {};
                const supportingProviderIds = selectedModelId
                  ? new Set(
                      (field.providerOptions ?? [])
                        .filter((p) => {
                          const schema = componentSchemas[p.id];
                          return schema?.properties?.model?.oneOf?.some(
                            (entry) => entry.const === selectedModelId,
                          );
                        })
                        .map((p) => p.id),
                    )
                  : null;

                const backendOptions = (field.providerOptions ?? []).filter(
                  (p) =>
                    supportingProviderIds
                      ? supportingProviderIds.has(p.id)
                      : true,
                );

                const selectedBackendItem =
                  backendOptions.find((p) => p.id === selectedProviderId) ??
                  null;

                // Helper: find supporting providers for a given model using schemas.
                const getSupportingProviders = (modelId: string): string[] =>
                  (field.providerOptions ?? [])
                    .filter((p) => {
                      const schema = componentSchemas[p.id];
                      return schema?.properties?.model?.oneOf?.some(
                        (entry) => entry.const === modelId,
                      );
                    })
                    .map((p) => p.id);

                // No model options → show only the backend/provider dropdown
                // (same single-dropdown pattern as vector store).
                const hasModelOptions = field.options.length > 0;
                return (
                  <Fragment key={`${String(field.key)}-custom`}>
                    {/* Model dropdown — only when model options exist */}
                    {hasModelOptions && (
                      <div>
                        <Dropdown
                          id={`${serviceName}-${String(field.key)}-model`}
                          titleText={field.label}
                          label="Choose an option"
                          invalid={!selectedModelItem}
                          invalidText={`Provide a valid ${field.label}`}
                          items={field.options}
                          itemToString={(item) => (item ? item.text : "")}
                          selectedItem={selectedModelItem}
                          onChange={({ selectedItem }) => {
                            const newModelId = selectedItem?.id ?? "";
                            const supporting =
                              getSupportingProviders(newModelId);
                            // Keep current provider if compatible, else switch to first
                            const newProviderId = supporting.includes(
                              selectedProviderId,
                            )
                              ? selectedProviderId
                              : (supporting[0] ?? "");
                            onUpdateConfig({
                              components: {
                                ...currentConfig?.components,
                                [field.key]: {
                                  providerId: newProviderId,
                                  params: { model: newModelId },
                                },
                              },
                            });
                          }}
                        />
                      </div>
                    )}
                    {/* Backend/provider dropdown — always shown; label adjusts when no model */}
                    <div>
                      <Dropdown
                        id={`${serviceName}-${String(field.key)}-backend`}
                        titleText={
                          hasModelOptions
                            ? `${field.label} Backend`
                            : field.label
                        }
                        label="Choose an option"
                        invalid={!selectedBackendItem}
                        invalidText={`Provide a valid ${field.label} Backend`}
                        items={backendOptions}
                        itemToString={(item) => (item ? item.text : "")}
                        selectedItem={selectedBackendItem}
                        onChange={({ selectedItem }) => {
                          onUpdateConfig({
                            components: {
                              ...currentConfig?.components,
                              [field.key]: {
                                providerId: selectedItem?.id ?? "",
                                params: {
                                  model: selectedModelId ?? "",
                                },
                              },
                            },
                          });
                        }}
                      />
                    </div>
                    {/* Credential fields for the selected custom component backend */}
                    {(() => {
                      const customProviderSchema =
                        componentSchemas[selectedProviderId];
                      const credentialKeys = customProviderSchema?.properties
                        ? Object.keys(customProviderSchema.properties).filter(
                            (key) => key !== "model",
                          )
                        : [];

                      return credentialKeys.length > 0 ? (
                        <>
                          <div />
                          <div className={styles.fullWidth}>
                            <h4 className={styles.cloudCredentialsTitle}>
                              {selectedBackendItem?.text
                                ? `${selectedBackendItem.text} credentials`
                                : "Backend credentials"}
                            </h4>
                            <DynamicSchemaFields
                              componentType={String(field.key)}
                              providerId={selectedProviderId}
                              values={
                                currentConfig?.components?.[field.key]
                                  ?.params ?? {}
                              }
                              onChange={(params) => {
                                setHasValidationError(false);
                                setFieldErrors({});
                                const customKeys = new Set(
                                  customProviderSchema?.properties
                                    ? Object.keys(
                                        customProviderSchema.properties,
                                      )
                                    : [],
                                );
                                // Keep model and any non-schema params; overwrite
                                // only keys belonging to this provider's schema.
                                const mergedParams: Record<string, unknown> =
                                  {};
                                Object.entries(
                                  currentConfig?.components?.[field.key]
                                    ?.params ?? {},
                                ).forEach(([key, value]) => {
                                  if (!customKeys.has(key)) {
                                    mergedParams[key] = value;
                                  }
                                });
                                Object.entries(params).forEach(
                                  ([key, value]) => {
                                    mergedParams[key] = value;
                                  },
                                );
                                onUpdateConfig({
                                  components: {
                                    ...currentConfig?.components,
                                    [field.key]: {
                                      providerId: selectedProviderId,
                                      params: mergedParams,
                                    },
                                  },
                                });
                              }}
                              providerParamsMap={
                                componentSchemas as Record<string, JSONSchema>
                              }
                              hasValidationError={hasValidationError}
                              fieldErrors={fieldErrors}
                            />
                          </div>
                        </>
                      ) : null;
                    })()}
                  </Fragment>
                );
              }

              let fieldValue: string | undefined;

              if (field.globalValue !== undefined) {
                fieldValue = field.globalValue;
              } else if (field.key === "version") {
                fieldValue = currentConfig?.version;
              } else if (field.isModelFirst) {
                fieldValue = currentConfig?.components?.[field.key]?.params
                  ?.model as string | undefined;
              } else if (
                currentConfig?.components &&
                currentConfig.components[field.key]
              ) {
                fieldValue = currentConfig.components[field.key].providerId;
              }

              const selectedItem =
                field.options.find(
                  (opt: { id: string; text: string }) => opt.id === fieldValue,
                ) || null;

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
                        invalidText={`Provide a valid ${field.label}`}
                        items={field.options}
                        itemToString={(item) => (item ? item.text : "")}
                        selectedItem={selectedItem}
                        onChange={({ selectedItem }) => {
                          if (field.key === "version") {
                            onUpdateConfig({
                              version: selectedItem?.id || "",
                            });
                          } else if (
                            field.isModelFirst &&
                            inferenceComponentType &&
                            String(field.key) === inferenceComponentType
                          ) {
                            // Inference component (LLM or reranker): model-first, auto-resolve provider
                            handleLlmModelChange(
                              field.key,
                              selectedItem?.id || "",
                            );
                          } else if (field.isModelFirst) {
                            // Other model-first fields: write model directly into params
                            onUpdateConfig({
                              components: {
                                ...currentConfig?.components,
                                [field.key]: {
                                  providerId:
                                    currentConfig?.components?.[field.key]
                                      ?.providerId ?? "",
                                  params: {
                                    ...currentConfig?.components?.[field.key]
                                      ?.params,
                                    model: selectedItem?.id || "",
                                  },
                                },
                              },
                            });
                          } else {
                            // DA and provider-first fields: write providerId directly
                            onUpdateConfig({
                              components: {
                                ...currentConfig?.components,
                                [field.key]: {
                                  providerId: selectedItem?.id || "",
                                  params:
                                    currentConfig?.components?.[field.key]
                                      ?.params ?? {},
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
              inferenceComponentType &&
              (() => {
                const fieldValue = inferenceComponentType
                  ? currentConfig?.components?.[inferenceComponentType]
                      ?.providerId
                  : undefined;
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
                        label="Choose an option"
                        invalid={!selectedItem}
                        invalidText={`Provide a valid ${inferenceBackendField.label}`}
                        items={inferenceBackendField.options}
                        itemToString={(item) => (item ? item.text : "")}
                        selectedItem={selectedItem}
                        onChange={({ selectedItem }) => {
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
                            components: {
                              ...currentConfig?.components,
                              [inferenceComponentType!]: {
                                providerId: selectedItem?.id || "",
                                params:
                                  currentConfig?.components?.[
                                    inferenceComponentType!
                                  ]?.params ?? {},
                              },
                            },
                            params: preservedParams,
                          });
                        }}
                      />
                    </div>
                    {(() => {
                      const providerSchema =
                        providerParamsByType[inferenceComponentType!]?.[
                          fieldValue || ""
                        ];
                      const hasCredentialFields =
                        providerSchema?.properties &&
                        Object.keys(providerSchema.properties).filter(
                          (key) => key !== "model",
                        ).length > 0;

                      return hasCredentialFields ? (
                        <>
                          <div />
                          <div className={styles.fullWidth}>
                            <h4 className={styles.cloudCredentialsTitle}>
                              {(fieldValue || "")
                                .toLowerCase()
                                .includes("watsonx")
                                ? "Cloud credentials"
                                : "Inference credentials"}
                            </h4>
                            <DynamicSchemaFields
                              componentType={inferenceComponentType!}
                              providerId={fieldValue || ""}
                              values={currentConfig?.params || {}}
                              onChange={(params) => {
                                setHasValidationError(false);
                                setFieldErrors({});
                                const providerSchema =
                                  providerParamsByType[
                                    inferenceComponentType!
                                  ]?.[fieldValue || ""];
                                const providerKeys = new Set(
                                  providerSchema?.properties
                                    ? Object.keys(providerSchema.properties)
                                    : [],
                                );

                                const mergedParams: Record<string, unknown> =
                                  {};
                                Object.entries(
                                  currentConfig?.params || {},
                                ).forEach(([key, value]) => {
                                  if (!providerKeys.has(key)) {
                                    mergedParams[key] = value;
                                  }
                                });
                                Object.entries(params).forEach(
                                  ([key, value]) => {
                                    mergedParams[key] = value;
                                  },
                                );

                                onUpdateConfig({ params: mergedParams });
                              }}
                              providerParamsMap={
                                (providerParamsByType[
                                  inferenceComponentType!
                                ] || {}) as Record<string, JSONSchema>
                              }
                              hasValidationError={hasValidationError}
                              fieldErrors={fieldErrors}
                            />
                          </div>
                        </>
                      ) : null;
                    })()}
                  </Fragment>
                );
              })()}

            {/* Render service-level schema fields in edit mode */}
            {serviceFields.length > 0 && serviceSchema && (
              <div className={styles.fullWidth}>
                <DynamicSchemaFields
                  componentType={serviceId}
                  providerId={serviceId}
                  values={currentConfig?.params || {}}
                  onChange={(params) => {
                    setHasValidationError(false);
                    setFieldErrors({});
                    const serviceKeys = new Set(
                      serviceSchema?.properties
                        ? Object.keys(serviceSchema.properties)
                        : [],
                    );

                    const mergedParams: Record<string, unknown> = {};
                    Object.entries(currentConfig?.params || {}).forEach(
                      ([key, value]) => {
                        if (!serviceKeys.has(key)) {
                          mergedParams[key] = value;
                        }
                      },
                    );
                    Object.entries(params).forEach(([key, value]) => {
                      mergedParams[key] = value;
                    });

                    onUpdateConfig({ params: mergedParams });
                  }}
                  providerParamsMap={{ [serviceId]: serviceSchema }}
                  hasValidationError={hasValidationError}
                  fieldErrors={fieldErrors}
                />
              </div>
            )}

            {/* Model Description Accordion - In edit mode */}
            {inferenceComponent &&
              inferenceComponentType &&
              (() => {
                const providerId =
                  currentConfig?.components?.[inferenceComponentType]
                    ?.providerId;
                const modelId = currentConfig?.components?.[
                  inferenceComponentType
                ]?.params?.model as string | undefined;

                if (!modelId || !providerId) return null;

                const modelDescription = getModelDescription(
                  inferenceComponentType,
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
                    className={`${styles.modelDescriptionSection} ${styles.fullWidth}`}
                  >
                    <Accordion>
                      <AccordionItem title="What is this LLM good at?">
                        <div className={styles.modelDescriptionContent}>
                          {sections.introduction && (
                            <div className={styles.modelDescriptionFullWidth}>
                              <p className={styles.modelDescriptionText}>
                                {sections.introduction}
                              </p>
                            </div>
                          )}

                          {sections.useCases && (
                            <div className={styles.modelDescriptionFullWidth}>
                              <p className={styles.modelDescriptionText}>
                                {sections.useCases}
                              </p>
                            </div>
                          )}

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
