import { useEffect, useMemo } from "react";
import styles from "./RemoteModelConfig.module.scss";
import { Dropdown, InlineLoading, InlineNotification } from "@carbon/react";
import { DynamicSchemaFields } from "../../Shared/components/DynamicSchemaFields";
import {
  useModelDeployOptions,
  useProviderParamsSchema,
} from "../hooks/useModelDeployOptions";
import {
  buildDefaultConfig,
  buildProviderConfig,
  getParamErrors,
  isConfigResolved,
} from "../utils/modelFlowOptions";
import type { ModelConfig } from "../types";

interface RemoteModelConfigProps {
  modelId: string;
  config: ModelConfig;
  onApply: (config: ModelConfig) => void;
  onValidityChange: (isValid: boolean) => void;
  showErrors: boolean;
}

// Provider dropdown and provider fields for a remote model
export const RemoteModelConfig: React.FC<RemoteModelConfigProps> = ({
  modelId,
  config,
  onApply,
  onValidityChange,
  showErrors,
}) => {
  const {
    data: deployOptions,
    isLoading: optionsLoading,
    error: optionsError,
  } = useModelDeployOptions(modelId);

  const providers = useMemo(
    () =>
      (deployOptions?.providers ?? []).filter(
        (p) => p.deployment_type === "remote",
      ),
    [deployOptions],
  );

  // Resolve the default provider
  useEffect(() => {
    if (providers.length === 0 || isConfigResolved(config, providers)) return;
    onApply(buildDefaultConfig(providers));
  }, [providers, config, onApply]);

  const selectedProvider = providers.find(
    (p) => p.id === config.inferenceBackendId,
  );

  const {
    data: paramsSchema,
    isLoading: schemaLoading,
    error: schemaError,
  } = useProviderParamsSchema(selectedProvider?.schema);

  const paramErrors = useMemo(
    () => getParamErrors(paramsSchema, config.params),
    [paramsSchema, config.params],
  );

  const isValid =
    !!selectedProvider &&
    !schemaLoading &&
    !schemaError &&
    Object.keys(paramErrors).length === 0;
  useEffect(() => {
    onValidityChange(isValid);
  }, [isValid, onValidityChange]);

  if (optionsLoading) {
    return <InlineLoading description="Loading providers..." />;
  }

  if (optionsError) {
    return (
      <InlineNotification
        kind="error"
        role="alert"
        title="Failed to load deploy options."
        subtitle="Cancel and reopen to try again."
        lowContrast
        hideCloseButton
      />
    );
  }

  if (providers.length === 0) {
    return (
      <InlineNotification
        kind="warning"
        title="No remote providers available"
        subtitle="This model cannot be registered as a remote endpoint."
        lowContrast
        hideCloseButton
      />
    );
  }

  return (
    <div className={styles.remoteConfig}>
      <div className={styles.providerField}>
        <Dropdown
          id="remote-model-provider"
          titleText="Provider"
          label="Select provider"
          items={providers}
          itemToString={(item) => (item ? item.name : "")}
          selectedItem={selectedProvider ?? null}
          onChange={({ selectedItem }) => {
            if (selectedItem) onApply(buildProviderConfig(selectedItem));
          }}
        />
      </div>

      {schemaLoading && <InlineLoading description="Loading settings..." />}

      {schemaError && !schemaLoading && (
        <InlineNotification
          kind="error"
          role="alert"
          title="Failed to load provider settings."
          subtitle="Cancel and reopen to try again."
          lowContrast
          hideCloseButton
        />
      )}

      {selectedProvider && paramsSchema && (
        <div className={styles.schemaFields}>
          <DynamicSchemaFields
            componentType="model"
            providerId={selectedProvider.id}
            values={config.params}
            onChange={(params) => onApply({ ...config, params })}
            providerParamsMap={{ [selectedProvider.id]: paramsSchema }}
            hasValidationError={showErrors}
            fieldErrors={paramErrors}
          />
        </div>
      )}
    </div>
  );
};
