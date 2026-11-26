import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Dropdown,
  InlineLoading,
  InlineNotification,
  Link,
  Toggletip,
  ToggletipActions,
  ToggletipButton,
  ToggletipContent,
} from "@carbon/react";
import { ProductiveCard } from "@carbon/ibm-products";
import { Edit, Information, View, ViewOff } from "@carbon/icons-react";
import type {
  ModelDeployProfile,
  ModelDeployProvider,
} from "@/types/api.types";
import { useResources } from "../../Shared/hooks/useResources";
import { ResourceRequirementsPanel } from "../../Shared/components/ResourceRequirementsPanel";
import { DynamicSchemaFields } from "../../Shared/components/DynamicSchemaFields";
import type { DeploymentRuntimeType } from "../../Shared/types";
import {
  useModelDeployOptions,
  useProviderParamsSchema,
} from "../hooks/useModelDeployOptions";
import {
  buildDefaultConfig,
  buildProviderConfig,
  getParamErrors,
  getParamFields,
  isConfigResolved,
  profileLabel,
  profileToCalculatedResources,
} from "../utils/modelFlowOptions";
import type { ModelConfig } from "../types";
import sharedStyles from "../../Shared/DeployFlow.shared.module.scss";
import styles from "../ModelsDeployFlow.module.scss";

// TODO: Replace with the real sizing reference URL once it is available
const SIZING_REFERENCE_URL = "https://example.com/sizing-reference";

interface ModelsStepTwoProps {
  title: string;
  modelId: string;
  modelName: string;
  modelDescription: string;
  componentType: string;
  deploymentType: DeploymentRuntimeType;
  workerName: string;
  config: ModelConfig;
  onApply: (config: ModelConfig) => void;
  onEditingChange: (isEditing: boolean) => void;
  onResourceStatusChange: (hasInsufficientResources: boolean) => void;
  onValidityChange: (isValid: boolean) => void;
}

interface LocalResourceRequirementsProps {
  workerName: string;
  profile: ModelDeployProfile | undefined;
  onResourceStatusChange: (hasInsufficientResources: boolean) => void;
}

// Shows required vs available resources for the applied profile
const LocalResourceRequirements: React.FC<LocalResourceRequirementsProps> = ({
  workerName,
  profile,
  onResourceStatusChange,
}) => {
  const { resources, resourcesLoading, resourcesError } =
    useResources(workerName);
  const requiredResources = useMemo(
    () => profileToCalculatedResources(profile),
    [profile],
  );

  return (
    <ResourceRequirementsPanel
      calculatedResources={requiredResources}
      resourceData={resources}
      resourcesLoading={resourcesLoading}
      resourcesError={resourcesError}
      onResourceStatusChange={onResourceStatusChange}
    />
  );
};

export const ModelsStepTwo: React.FC<ModelsStepTwoProps> = ({
  title,
  modelId,
  modelName,
  modelDescription,
  componentType,
  deploymentType,
  workerName,
  config,
  onApply,
  onEditingChange,
  onResourceStatusChange,
  onValidityChange,
}) => {
  const {
    data: deployOptions,
    isLoading: optionsLoading,
    error: optionsError,
  } = useModelDeployOptions(modelId, deploymentType);

  // Local backends for the chosen runtime
  const providers = useMemo(
    () =>
      (deployOptions?.providers ?? []).filter(
        (p) => p.deployment_type === "local",
      ),
    [deployOptions],
  );

  // Resolve the default backend and profile
  useEffect(() => {
    if (providers.length === 0 || isConfigResolved(config, providers)) return;
    onApply(buildDefaultConfig(providers));
  }, [providers, config, onApply]);

  // Working copy while editing; null in read view
  const [draft, setDraft] = useState<ModelConfig | null>(null);
  const [showErrors, setShowErrors] = useState(false);
  const [showPasswords, setShowPasswords] = useState<Record<string, boolean>>(
    {},
  );
  const isEditing = draft !== null;
  const shownConfig = draft ?? config;

  const appliedProvider = providers.find(
    (p) => p.id === config.inferenceBackendId,
  );
  const appliedProfile = appliedProvider?.profiles?.find(
    (p) => p.id === config.resourceProfileId,
  );
  const shownProvider = providers.find(
    (p) => p.id === shownConfig.inferenceBackendId,
  );
  const shownProfile = shownProvider?.profiles?.find(
    (p) => p.id === shownConfig.resourceProfileId,
  );

  const {
    data: paramsSchema,
    isLoading: schemaLoading,
    error: schemaError,
  } = useProviderParamsSchema(shownProvider?.schema);

  // Fields requested by the provider schema
  const paramFields = useMemo(
    () => (paramsSchema ? getParamFields(paramsSchema) : []),
    [paramsSchema],
  );

  const paramErrors = useMemo(
    () => getParamErrors(paramsSchema, shownConfig.params),
    [paramsSchema, shownConfig.params],
  );
  const hasParamErrors = Object.keys(paramErrors).length > 0;

  // Block Register while editing
  useEffect(() => {
    onEditingChange(isEditing);
  }, [isEditing, onEditingChange]);

  // Register needs a saved, valid config
  const isValid =
    !isEditing &&
    !!appliedProvider &&
    !schemaLoading &&
    !schemaError &&
    !hasParamErrors;
  useEffect(() => {
    onValidityChange(isValid);
  }, [isValid, onValidityChange]);

  const handleEdit = () => {
    setShowErrors(false);
    setShowPasswords({});
    setDraft(config);
  };

  // Toggles a saved secret between masked and plain
  const togglePasswordVisibility = (key: string) => {
    setShowPasswords((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const handleBackendChange = useCallback(
    (provider: ModelDeployProvider | null) => {
      if (!provider) return;
      setDraft(buildProviderConfig(provider));
    },
    [],
  );

  const handleProfileChange = (profile: ModelDeployProfile | null) => {
    if (!profile) return;
    setDraft({ ...shownConfig, resourceProfileId: profile.id });
  };

  const handleParamsChange = (params: Record<string, unknown>) => {
    setDraft({ ...shownConfig, params });
  };

  const handleCancel = () => {
    setShowErrors(false);
    setShowPasswords({});
    setDraft(null);
  };

  // Saves the draft, or shows errors if invalid
  const handleSave = () => {
    if (!draft) return;
    if (hasParamErrors) {
      setShowErrors(true);
      return;
    }
    onApply(draft);
    setShowErrors(false);
    setShowPasswords({});
    setDraft(null);
  };

  const hasProfiles = (shownProvider?.profiles?.length ?? 0) > 0;
  const backendLabel = `${componentType} inference backend`;

  // Renders one saved param value
  const renderParamValue = (
    key: string,
    label: string,
    isPassword: boolean,
  ) => {
    const value = config.params[key];
    const isEmpty =
      value === undefined || value === null || String(value).trim() === "";
    if (isEmpty) {
      return (
        <span className={sharedStyles.requiredMessage}>
          Required — Click Edit to add
        </span>
      );
    }
    if (!isPassword) return String(value);

    const isVisible = !!showPasswords[key];
    return (
      <>
        <span className={sharedStyles.apiKeyValue}>
          {isVisible ? String(value) : "•".repeat(20)}
        </span>
        <Button
          kind="ghost"
          size="sm"
          hasIconOnly
          renderIcon={isVisible ? ViewOff : View}
          iconDescription={`${isVisible ? "Hide" : "Show"} ${label}`}
          onClick={() => togglePasswordVisibility(key)}
          className={sharedStyles.apiKeyToggle}
        />
      </>
    );
  };

  // Read view
  const renderReadView = () => (
    <div className={sharedStyles.serviceConfigContent}>
      <div className={sharedStyles.serviceConfigItem}>
        <span className={sharedStyles.serviceConfigItemLabel}>
          {backendLabel}
        </span>
        <span className={sharedStyles.serviceConfigItemValue}>
          {appliedProvider?.name ?? ""}
        </span>
      </div>
      {appliedProfile && (
        <div className={sharedStyles.serviceConfigItem}>
          <span className={sharedStyles.serviceConfigItemLabel}>
            Resource profile
          </span>
          <span className={sharedStyles.serviceConfigItemValue}>
            {profileLabel(appliedProfile)}
          </span>
        </div>
      )}
      {paramFields
        .filter(
          (field) =>
            field.validation?.required ||
            String(config.params[field.key] ?? "").trim() !== "",
        )
        .map((field) => (
          <div key={field.key} className={sharedStyles.serviceConfigItem}>
            <span className={sharedStyles.serviceConfigItemLabel}>
              {field.label}
            </span>
            <span className={sharedStyles.serviceConfigItemValue}>
              {renderParamValue(
                field.key,
                field.label,
                field.type === "password",
              )}
            </span>
          </div>
        ))}
    </div>
  );

  // Edit view
  const renderEditView = () => (
    <div className={sharedStyles.serviceConfigFieldRow}>
      <div>
        <div className={styles.fieldLabel}>
          <span>{backendLabel}</span>
        </div>
        <Dropdown
          id="model-inference-backend"
          titleText={backendLabel}
          hideLabel
          label="Select inference backend"
          items={providers}
          itemToString={(item) => (item ? item.name : "")}
          selectedItem={shownProvider ?? null}
          onChange={({ selectedItem }) => handleBackendChange(selectedItem)}
        />
      </div>

      {hasProfiles && (
        <div>
          {/* Outside the label to avoid interactive content inside it */}
          <div className={styles.fieldLabel}>
            <span>Resource profile</span>
            <Toggletip align="top">
              <ToggletipButton label="Additional information">
                <Information />
              </ToggletipButton>
              <ToggletipContent>
                <p>
                  Defines the infrastructure resources allocated to this model
                  instance, including acceleration cards, RAM allocation, and
                  context window. See sizing reference for all parameters.
                </p>
                <ToggletipActions>
                  <Link
                    href={SIZING_REFERENCE_URL}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    Sizing reference
                  </Link>
                </ToggletipActions>
              </ToggletipContent>
            </Toggletip>
          </div>
          <Dropdown
            id="model-resource-profile"
            titleText="Resource profile"
            hideLabel
            label="Select resource profile"
            items={shownProvider?.profiles ?? []}
            itemToString={(item) => (item ? profileLabel(item) : "")}
            selectedItem={shownProfile ?? null}
            onChange={({ selectedItem }) => handleProfileChange(selectedItem)}
          />
        </div>
      )}

      {schemaLoading && (
        <div className={sharedStyles.fullWidth}>
          <InlineLoading description="Loading settings..." />
        </div>
      )}

      {schemaError && !schemaLoading && (
        <div className={sharedStyles.fullWidth}>
          <InlineNotification
            kind="error"
            role="alert"
            title="Failed to load backend settings."
            subtitle="Cancel and reopen to try again."
            lowContrast
            hideCloseButton
          />
        </div>
      )}

      {shownProvider && paramsSchema && paramFields.length > 0 && (
        <div className={sharedStyles.fullWidth}>
          <DynamicSchemaFields
            componentType={componentType}
            providerId={shownProvider.id}
            values={shownConfig.params}
            onChange={handleParamsChange}
            providerParamsMap={{ [shownProvider.id]: paramsSchema }}
            hasValidationError={showErrors}
            fieldErrors={paramErrors}
          />
        </div>
      )}
    </div>
  );

  return (
    <>
      <div className={sharedStyles.stepHeader}>
        <h2 className={sharedStyles.stepTitle}>{title}</h2>
      </div>

      <LocalResourceRequirements
        workerName={workerName}
        profile={appliedProfile}
        onResourceStatusChange={onResourceStatusChange}
      />

      <ProductiveCard
        title={modelName}
        description={modelDescription}
        className={sharedStyles.serviceConfigCard}
      >
        {providers.length > 0 && !isEditing && (
          <div className={sharedStyles.cardEditAction}>
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
          <div className={sharedStyles.cardEditAction}>
            <Button kind="secondary" size="sm" onClick={handleCancel}>
              Cancel
            </Button>
            <Button
              kind="primary"
              size="sm"
              onClick={handleSave}
              disabled={schemaLoading}
            >
              Save
            </Button>
          </div>
        )}

        {optionsLoading && (
          <InlineLoading description="Loading inference backends..." />
        )}

        {optionsError && !optionsLoading && (
          <InlineNotification
            kind="error"
            role="alert"
            title="Failed to load deploy options."
            subtitle="Cancel and reopen to try again."
            lowContrast
            hideCloseButton
          />
        )}

        {!optionsLoading && !optionsError && providers.length === 0 && (
          <InlineNotification
            kind="warning"
            title="No inference backends available"
            subtitle="This model cannot be registered with the selected deployment type."
            lowContrast
            hideCloseButton
          />
        )}

        {providers.length > 0 &&
          (isEditing ? renderEditView() : renderReadView())}
      </ProductiveCard>
    </>
  );
};
