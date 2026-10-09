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
  /** Small label rendered above the heading (e.g. "Delete my-assistant"). */
  modalLabel?: string;
  /** Modal heading. @default "Confirm delete" */
  modalHeading?: string;
  /** Label for the primary action button (e.g. "Delete" or "Remove"). */
  primaryButtonLabel: string;
  /** Label for the primary action button when in-flight (e.g. "Deleting..." or "Removing..."). */
  primaryButtonLoadingLabel: string;
  /** Warning paragraph shown in the modal body. */
  warningText: string;
  /** When true an info InlineNotification is shown. @default false */
  showInfoNotification?: boolean;
  /** Title of the info inline notification. @default "This may take a while." */
  infoNotificationTitle?: string;
  /** Subtitle of the info inline notification. */
  infoNotificationSubtitle?: string;
  /** Error message from a failed delete attempt — shown as an error banner inside the modal. */
  errorMessage?: string;
  /** Title of the error inline notification. @default "Deletion failed:" */
  errorNotificationTitle?: string;
  /** Called on every keystroke in the confirmation input. */
  onConfirmValueChange: (value: string) => void;
  /** Called when the primary button is clicked. */
  onConfirm: () => void;
  /** Called when the modal is dismissed (X, Escape, or Cancel). */
  onClose: () => void;
}

const DeleteConfirmNameModal = ({
  isOpen,
  isDeleting,
  itemName,
  confirmValue,
  modalLabel,
  modalHeading = "Confirm delete",
  primaryButtonLabel,
  primaryButtonLoadingLabel,
  warningText,
  showInfoNotification = false,
  infoNotificationTitle = "This may take a while.",
  infoNotificationSubtitle = "Data will be removed from each connected vector store. You can continue working while this process completes.",
  errorMessage,
  errorNotificationTitle = "Deletion failed:",
  onConfirmValueChange,
  onConfirm,
  onClose,
}: DeleteConfirmNameModalProps) => {
  const inputId = useId();
  const nameMatches = Boolean(itemName) && confirmValue === itemName;

  return (
    <Modal
      open={isOpen}
      size="sm"
      modalLabel={modalLabel}
      modalHeading={modalHeading}
      primaryButtonText={
        isDeleting ? primaryButtonLoadingLabel : primaryButtonLabel
      }
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

        {showInfoNotification && (
          <InlineNotification
            kind="info"
            title={infoNotificationTitle}
            subtitle={infoNotificationSubtitle}
            lowContrast
            hideCloseButton
            className={styles.inlineNotification}
          />
        )}

        <p className={styles.warningText}>{warningText}</p>

        <TextInput
          id={inputId}
          labelText={`Type ${itemName} to confirm`}
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
