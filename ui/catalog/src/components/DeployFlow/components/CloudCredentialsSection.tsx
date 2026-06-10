import {
  TextInput,
  Toggletip,
  ToggletipButton,
  ToggletipContent,
} from "@carbon/react";
import { Information } from "@carbon/icons-react";
import styles from "../DeployFlow.module.scss";
import type { ServiceConfig } from "../types";

interface CloudCredentialsSectionProps {
  serviceName: string;
  currentConfig: ServiceConfig | null;
  updateTempConfig: (updates: Partial<ServiceConfig>) => void;
  hasValidationError?: boolean;
}

export const CloudCredentialsSection: React.FC<
  CloudCredentialsSectionProps
> = ({
  serviceName,
  currentConfig,
  updateTempConfig,
  hasValidationError = false,
}) => {
  if (currentConfig?.inferenceMethod !== "watsonx") {
    return null;
  }

  return (
    <div className={styles.cloudCredentialsSection}>
      <h4 className={styles.cloudCredentialsTitle}>Cloud credentials</h4>
      <div className={styles.serviceConfigFieldRow}>
        <div>
          <div className={styles.labelWithInfo}>
            <label
              htmlFor={`${serviceName}-projectId`}
              className={styles.textInputLabel}
            >
              Project ID
            </label>
            <Toggletip align="bottom">
              <ToggletipButton label="Additional information">
                <Information />
              </ToggletipButton>
              <ToggletipContent>
                <p>
                  Find your ID on the watsonx cloud site when you create a
                  project
                </p>
              </ToggletipContent>
            </Toggletip>
          </div>
          <TextInput
            id={`${serviceName}-projectId`}
            labelText=""
            placeholder="Ex. (12364567-e89b-12d3-a456-426614174000)"
            value={currentConfig?.watsonxProjectId || ""}
            invalid={
              hasValidationError && !currentConfig?.watsonxProjectId?.trim()
            }
            invalidText="Project ID is required"
            onChange={(e) =>
              updateTempConfig({ watsonxProjectId: e.target.value })
            }
          />
        </div>
        <div /> {/* Empty spacer */}
        <TextInput
          id={`${serviceName}-apiEndpoint`}
          labelText="API endpoint"
          placeholder=""
          value={currentConfig?.watsonxApiEndpoint || ""}
          invalid={
            hasValidationError && !currentConfig?.watsonxApiEndpoint?.trim()
          }
          invalidText="API endpoint is required"
          onChange={(e) =>
            updateTempConfig({ watsonxApiEndpoint: e.target.value })
          }
        />
        <TextInput
          id={`${serviceName}-apiKey`}
          labelText="API key"
          placeholder=""
          type="password"
          value={currentConfig?.watsonxApiKey || ""}
          invalid={hasValidationError && !currentConfig?.watsonxApiKey?.trim()}
          invalidText="API key is required"
          onChange={(e) => updateTempConfig({ watsonxApiKey: e.target.value })}
        />
      </div>
    </div>
  );
};
