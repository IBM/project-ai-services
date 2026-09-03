import { useId } from "react";
import { Modal, TextInput, InlineNotification } from "@carbon/react";
import styles from "./DeleteConfirmNameModal.module.scss";

export interface DeleteConfirmNameModalProps {
  /** Controls modal visibility. */
  isOpen: boolean;
  /** True while the async delete call is in-flight. Disables all controls. */
  isDeleting: boolean;
  /** The exact name the user must type to unlock the Remove button. */
  itemName: string;
  /** Current value of the confirmation text input, controlled by parent. */
  confirmValue: string;
  /** Modal heading. @default "Remove data source" */
  heading?: string;
  /** Warning paragraph shown below the inline notification. */
  warningText: string;
  /** Title of the info inline notification. @default "This may take a while." */
  infoNotificationTitle?: string;
  /** Subtitle of the info inline notification. */
  infoNotificationSubtitle?: string;
  /** Error message from a failed delete attempt — shown as an error banner inside the modal. */
  errorMessage?: string;
  /** Title of the error inline notification. @default "Removal failed:" */
  errorNotificationTitle?: string;
  /** Called on every keystroke in the confirmation input. */
  onConfirmValueChange: (value: string) => void;
  /** Called when the primary (Remove) button is clicked. */
  onConfirm: () => void;
  /** Called when the modal is dismissed (X, Escape, or Cancel). */
  onClose: () => void;
}

const DeleteConfirmNameModal = ({
  isOpen,
  isDeleting,
  itemName,
  confirmValue,
  heading = "Remove data source",
  warningText,
  infoNotificationTitle = "This may take a while.",
  infoNotificationSubtitle = "Data will be removed from each connected vector store. You can continue working while this process completes.",
  errorMessage,
  errorNotificationTitle = "Removal failed:",
  onConfirmValueChange,
  onConfirm,
  onClose,
}: DeleteConfirmNameModalProps) => {
  const inputId = useId();
  const nameMatches = confirmValue === itemName;

  return (
    <Modal
      open={isOpen}
      size="sm"
      modalHeading={heading}
      primaryButtonText={isDeleting ? "Removing..." : "Remove"}
      secondaryButtonText="Cancel"
      danger
      primaryButtonDisabled={!nameMatches || isDeleting}
      onRequestClose={() => {
        if (!isDeleting) {
          onClose();
        }
      }}
      onSecondarySubmit={() => {
        if (!isDeleting) {
          onClose();
        }
      }}
      onRequestSubmit={onConfirm}
    >
      <div className={styles.modalBody}>
        {errorMessage && (
          <InlineNotification
            kind="error"
            title={errorNotificationTitle}
            subtitle={errorMessage}
            lowContrast
            hideCloseButton
            className={styles.errorNotification}
          />
        )}

        <InlineNotification
          kind="info"
          title={infoNotificationTitle}
          subtitle={infoNotificationSubtitle}
          lowContrast
          hideCloseButton
          className={styles.inlineNotification}
        />

        <p className={styles.warningText}>{warningText}</p>

        <TextInput
          id={inputId}
          labelText={`Type [${itemName}] to confirm`}
          value={confirmValue}
          onChange={(e) => onConfirmValueChange(e.target.value)}
          disabled={isDeleting}
          autoComplete="off"
        />
      </div>
    </Modal>
  );
};

export default DeleteConfirmNameModal;
