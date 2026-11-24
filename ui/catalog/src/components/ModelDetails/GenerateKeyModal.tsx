import { useState } from "react";
import {
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
  onConfirm: () => void;
  onRetry?: () => void;
  onClose: () => void;
}

// Confirms key revocation before generating, then displays the newly created API key
const GenerateKeyModal = ({
  isOpen,
  isGenerating,
  apiKey,
  error,
  onConfirm,
  onRetry = onConfirm,
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

  const isKeyReady = Boolean(apiKey);

  return (
    <Modal
      open={isOpen}
      passiveModal={isKeyReady}
      size="xs"
      modalHeading={isKeyReady ? "API key generated" : "Generate API key"}
      primaryButtonText={
        isGenerating ? "Generating..." : error ? "Retry" : "Generate key"
      }
      secondaryButtonText="Cancel"
      primaryButtonDisabled={isGenerating}
      onRequestSubmit={error ? onRetry : onConfirm}
      onRequestClose={handleClose}
    >
      <div className={styles.modalBody}>
        {error ? (
          <InlineNotification
            kind="error"
            title="Key generation failed:"
            subtitle={error}
            lowContrast
            hideCloseButton
          />
        ) : isKeyReady && apiKey ? (
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
                  iconDescription="Copy"
                  feedback="Copied"
                  onClick={() => handleCopy(apiKey)}
                />
              )}
            </div>
          </>
        ) : isGenerating ? (
          <SkeletonText paragraph lineCount={3} />
        ) : (
          <>
            <InlineNotification
              kind="warning"
              title="Generating a new API key will revoke the existing key."
              lowContrast
              hideCloseButton
              className={styles.warning}
            />
            <p>
              Are you sure you want to generate a new API key for this model?
            </p>
          </>
        )}
      </div>
    </Modal>
  );
};

export default GenerateKeyModal;
