import { TextInput } from "@carbon/react";
import styles from "../DeployFlow.module.scss";
import type { ServiceConfig } from "../types";

interface VllmCredentialsSectionProps {
  serviceName: string;
  currentConfig: ServiceConfig | null;
  updateTempConfig: (updates: Partial<ServiceConfig>) => void;
}

export const VllmCredentialsSection: React.FC<VllmCredentialsSectionProps> = ({
  serviceName,
  currentConfig,
  updateTempConfig,
}) => {
  // Check which VLLM providers are selected
  const hasVllmCpu =
    currentConfig?.inferenceMethod?.includes("vllm-cpu") ||
    currentConfig?.rerankerModel?.includes("vllm-cpu") ||
    currentConfig?.embeddingModel?.includes("vllm-cpu");

  const hasVllmSpyre =
    currentConfig?.inferenceMethod?.includes("vllm-spyre") ||
    currentConfig?.rerankerModel?.includes("vllm-spyre") ||
    currentConfig?.embeddingModel?.includes("vllm-spyre");

  // Don't show if no VLLM providers are selected
  if (!hasVllmCpu && !hasVllmSpyre) {
    return null;
  }

  return (
    <div className={styles.cloudCredentialsSection}>
      <h4 className={styles.cloudCredentialsTitle}>
        VLLM Authentication (Optional)
      </h4>
      <div className={styles.serviceConfigFieldRow}>
        {hasVllmCpu && (
          <TextInput
            id={`${serviceName}-vllm-cpu-apiKey`}
            labelText="API key for vLLM CPU"
            placeholder="Leave empty to disable authentication"
            type="password"
            value={currentConfig?.vllmCpuApiKey || ""}
            onChange={(e) =>
              updateTempConfig({ vllmCpuApiKey: e.target.value })
            }
          />
        )}
        {hasVllmSpyre && (
          <TextInput
            id={`${serviceName}-vllm-spyre-apiKey`}
            labelText="API key for vLLM Spyre"
            placeholder="Leave empty to disable authentication"
            type="password"
            value={currentConfig?.vllmSpyreApiKey || ""}
            onChange={(e) =>
              updateTempConfig({ vllmSpyreApiKey: e.target.value })
            }
          />
        )}
      </div>
    </div>
  );
};

// Made with Bob
