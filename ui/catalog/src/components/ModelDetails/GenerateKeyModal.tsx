import { useState } from "react";
import {
  ActionableNotification,
  CopyButton,
  InlineNotification,
  Modal,
  SkeletonText,
  TextArea,
} from "@carbon/react";
import styles from "./GenerateKeyModal.module.scss";

export interface GenerateKeyModalProps {
  isOpen: boolean;
  isGenerating: boolean;
  apiKey: string | null;
  error: string | null;
  onRetry: () => void;
  onClose: () => void;
}

// Shows a freshly generated model API key once, with a copy action
const GenerateKeyModal = ({
  isOpen,
  isGenerating,
  apiKey,
  error,
  onRetry,
  onClose,
}: GenerateKeyModalProps) => {
  const [copyError, setCopyError] = useState<string | null>(null);
  // navigator.clipboard is undefined on plain HTTP pages, so hide the copy button there
  const canCopyToClipboard = Boolean(navigator.clipboard);

  const handleCopy = async (key: string) => {
    try {
      await navigator.clipboard.writeText(key);
      setCopyError(null);
    } catch {
      setCopyError("Could not copy the API key. Please copy it manually.");
    }
  };

  // The modal stays mounted between keys, so clear any copy error when it closes
  const handleClose = () => {
    setCopyError(null);
    onClose();
  };

  return (
    <Modal
      open={isOpen}
      passiveModal
      size="xs"
      modalHeading="Generate API key"
      onRequestClose={handleClose}
    >
      <div className={styles.modalBody}>
        {error ? (
          <ActionableNotification
            kind="error"
            title="Key generation failed:"
            subtitle={error}
            lowContrast
            hideCloseButton
            actionButtonLabel="Retry"
            onActionButtonClick={onRetry}
          />
        ) : (
          <>
            <InlineNotification
              kind="warning"
              title="Copy and store your API key in a secure location."
              subtitle="Once you close this dialog, the key will no longer be visible, so save it before sharing with your team."
              lowContrast
              hideCloseButton
              className={styles.warning}
            />
            {copyError && (
              <InlineNotification
                kind="error"
                role="alert"
                title="Failed to copy API key:"
                subtitle={copyError}
                lowContrast
                onClose={() => setCopyError(null)}
                className={styles.warning}
              />
            )}
            {isGenerating || !apiKey ? (
              <SkeletonText paragraph lineCount={2} />
            ) : (
              <div className={styles.keyRow}>
                <TextArea
                  id="model-api-key"
                  labelText="API key"
                  value={apiKey}
                  rows={2}
                  readOnly
                  className={styles.keyField}
                />
                {canCopyToClipboard && (
                  <CopyButton
                    align="left"
                    size="xs"
                    iconDescription="copy"
                    feedback="Copied"
                    onClick={() => handleCopy(apiKey)}
                  />
                )}
              </div>
            )}
          </>
        )}
      </div>
    </Modal>
  );
};

export default GenerateKeyModal;
