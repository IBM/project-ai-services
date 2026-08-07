import { useId } from "react";
import { Modal, Checkbox, CheckboxGroup } from "@carbon/react";
import sharedStyles from "@/components/Table/table.shared.module.scss";

interface DeleteModalProps {
  isOpen: boolean;
  /** True while the async delete call is in-flight. Disables close + button. */
  isDeleting: boolean;
  /** Whether the user has ticked the confirmation checkbox. */
  isConfirmed: boolean;
  /** The name of the item being deleted — shown in the checkbox label. */
  itemName: string;
  /** Carbon `modalLabel` (small text above the heading). */
  modalLabel: string;
  /** Legend text for the confirmation checkbox group. */
  confirmLegend: string;
  /** Warning paragraph shown in the modal body. */
  warningText: string;
  /** Called when the primary (Delete) button is clicked. */
  onConfirm: () => void;
  /** Called when the modal is dismissed (X, Escape, or Cancel). */
  onClose: () => void;
  /** Called when the confirmation checkbox changes. */
  onCheckboxChange: (checked: boolean) => void;
}

const DeleteModal = ({
  isOpen,
  isDeleting,
  isConfirmed,
  itemName,
  modalLabel,
  confirmLegend,
  warningText,
  onConfirm,
  onClose,
  onCheckboxChange,
}: DeleteModalProps) => {
  const checkboxId = useId();
  return (
    <Modal
      open={isOpen}
      size="sm"
      modalLabel={modalLabel}
      modalHeading="Confirm delete"
      primaryButtonText={isDeleting ? "Deleting..." : "Delete"}
      secondaryButtonText="Cancel"
      danger
      primaryButtonDisabled={!isConfirmed || isDeleting}
      onRequestClose={() => {
        // Prevent closing the modal while deletion is in progress
        if (!isDeleting) {
          onClose();
        }
      }}
      onRequestSubmit={onConfirm}
    >
      <p>{warningText}</p>
      <div>
        <CheckboxGroup
          className={sharedStyles.deleteConfirmation}
          legendText={confirmLegend}
        >
          <Checkbox
            id={checkboxId}
            labelText={<strong>{itemName}</strong>}
            checked={isConfirmed}
            onChange={(_, { checked }) => onCheckboxChange(checked)}
          />
        </CheckboxGroup>
      </div>
    </Modal>
  );
};

export default DeleteModal;
