import { Checkbox, TextArea } from "@carbon/react";
import styles from "../DeployFlow.module.scss";
import type { ServiceConfig } from "../types";

interface SystemPromptSectionProps {
  serviceName: string;
  currentConfig: ServiceConfig | null;
  updateTempConfig: (updates: Partial<ServiceConfig>) => void;
  hasValidationError?: boolean;
}

export const SystemPromptSection: React.FC<SystemPromptSectionProps> = ({
  serviceName,
  currentConfig,
  updateTempConfig,
  hasValidationError = false,
}) => {
  if (serviceName !== "Question and answer") {
    return null;
  }

  return (
    <div className={styles.systemPromptSection}>
      <Checkbox
        id={`${serviceName}-editSystemPrompt`}
        labelText="Edit system prompt for queries"
        checked={currentConfig?.editSystemPrompt || false}
        onChange={(e) =>
          updateTempConfig({
            editSystemPrompt: e.target.checked,
          })
        }
      />
      {currentConfig?.editSystemPrompt && (
        <div className={styles.systemPromptTextArea}>
          <TextArea
            id={`${serviceName}-systemPromptText`}
            labelText="Prompt text (English only)"
            value={
              currentConfig?.systemPromptText ||
              "You are a helpful, conversational AI assistant. Engage naturally with users across multiple turns of conversation. Provide clear, accurate, and contextually relevant responses. Reference previous exchanges when appropriate to maintain conversation flow."
            }
            invalid={
              hasValidationError && !currentConfig?.systemPromptText?.trim()
            }
            invalidText="Prompt text is required"
            onChange={(e) =>
              updateTempConfig({
                systemPromptText: e.target.value,
              })
            }
            rows={4}
            maxCount={2500}
            enableCounter
          />
        </div>
      )}
    </div>
  );
};
