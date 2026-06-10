import { Fragment } from "react";
import { Button, Dropdown, TextInput } from "@carbon/react";
import { ProductiveCard } from "@carbon/ibm-products";
import { Edit, Checkmark, View, ViewOff } from "@carbon/icons-react";
import styles from "../DeployFlow.module.scss";
import type { ServiceConfig } from "../types";
import type { ServiceConfigField } from "../types/StepTwo.types";
import { getDisplayName } from "../utils/StepTwo.utils";
import { CloudCredentialsSection } from "./CloudCredentialsSection";
import { SystemPromptSection } from "./SystemPromptSection";

interface ServiceConfigCardProps {
  serviceName: string;
  config: ServiceConfig;
  description: string;
  fields: ServiceConfigField[];
  isEditing: boolean;
  currentConfig: ServiceConfig | null;
  showApiKey: boolean;
  hasWatsonxValidationError?: boolean;
  hasPromptValidationError?: boolean;
  onEdit: () => void;
  onApply: () => void;
  onCancel: () => void;
  onUpdateConfig: (updates: Partial<ServiceConfig>) => void;
  onToggleApiKey: () => void;
}

export const ServiceConfigCard: React.FC<ServiceConfigCardProps> = ({
  serviceName,
  config,
  description,
  fields,
  isEditing,
  currentConfig,
  showApiKey,
  hasWatsonxValidationError = false,
  hasPromptValidationError = false,
  onEdit,
  onApply,
  onCancel,
  onUpdateConfig,
  onToggleApiKey,
}) => {
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
        <div className={styles.cardActions}>
          <Button kind="ghost" size="sm" onClick={onCancel}>
            Cancel
          </Button>
          <Button
            kind="primary"
            size="sm"
            renderIcon={Checkmark}
            onClick={onApply}
          >
            Apply
          </Button>
        </div>
      )}

      {!isEditing ? (
        <div className={styles.serviceConfigContent}>
          {fields.map((field) => {
            const value =
              field.globalValue !== undefined
                ? field.globalValue
                : config[field.key];
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
          {/* Show cloud credentials if watsonx is selected */}
          {config.inferenceMethod === "watsonx" && (
            <>
              {config.watsonxProjectId && (
                <div className={styles.serviceConfigItem}>
                  <span className={styles.serviceConfigItemLabel}>
                    Project ID
                  </span>
                  <span className={styles.serviceConfigItemValue}>
                    {config.watsonxProjectId}
                  </span>
                </div>
              )}
              {config.watsonxApiEndpoint && (
                <div className={styles.serviceConfigItem}>
                  <span className={styles.serviceConfigItemLabel}>
                    API endpoint
                  </span>
                  <span className={styles.serviceConfigItemValue}>
                    {config.watsonxApiEndpoint}
                  </span>
                </div>
              )}
              {config.watsonxApiKey && (
                <div className={styles.serviceConfigItem}>
                  <span className={styles.serviceConfigItemLabel}>API key</span>
                  <span className={styles.serviceConfigItemValue}>
                    <span className={styles.apiKeyValue}>
                      {showApiKey ? config.watsonxApiKey : "•".repeat(20)}
                    </span>
                    <Button
                      kind="ghost"
                      size="sm"
                      hasIconOnly
                      renderIcon={showApiKey ? ViewOff : View}
                      iconDescription={
                        showApiKey ? "Hide API key" : "Show API key"
                      }
                      onClick={onToggleApiKey}
                      className={styles.apiKeyToggle}
                    />
                  </span>
                </div>
              )}
            </>
          )}
          {/* Show system prompt if enabled for Question and answer service */}
          {serviceName === "Question and answer" &&
            config.editSystemPrompt &&
            config.systemPromptText && (
              <div className={styles.serviceConfigItem}>
                <span className={styles.serviceConfigItemLabel}>
                  System prompt
                </span>
                <span className={styles.serviceConfigItemValue}>
                  {config.systemPromptText}
                </span>
              </div>
            )}
        </div>
      ) : (
        <>
          <div className={styles.serviceConfigFieldRow}>
            {fields.map((field, index) => {
              const fieldValue =
                field.globalValue !== undefined
                  ? field.globalValue
                  : currentConfig?.[field.key];

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
                        onChange={({ selectedItem }) =>
                          onUpdateConfig({
                            [field.key]: selectedItem?.id || "",
                          })
                        }
                      />
                    )}
                  </div>
                  {/* Add empty space after service version (first field) */}
                  {index === 0 && <div />}

                  {/* System prompt section - only for Question and answer service after service version */}
                  {index === 0 && (
                    <SystemPromptSection
                      serviceName={serviceName}
                      currentConfig={currentConfig}
                      updateTempConfig={onUpdateConfig}
                      hasValidationError={hasPromptValidationError}
                    />
                  )}
                </Fragment>
              );
            })}
          </div>

          {/* Cloud credentials section - show when watsonx is selected */}
          <CloudCredentialsSection
            serviceName={serviceName}
            currentConfig={currentConfig}
            updateTempConfig={onUpdateConfig}
            hasValidationError={hasWatsonxValidationError}
          />
        </>
      )}
    </ProductiveCard>
  );
};
