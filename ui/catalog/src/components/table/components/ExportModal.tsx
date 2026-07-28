import { Modal, TextInput } from "@carbon/react";

interface ExportModalProps {
  isOpen: boolean;
  /** True while the async export is in-flight. Disables the primary button. */
  isExporting: boolean;
  /** The current value of the filename input. */
  csvFileName: string;
  /** Inline validation error shown below the input. Empty string = no error. */
  exportErrorMessage: string;
  /** Called when the primary (Export) button is clicked. */
  onConfirm: () => void;
  /** Called when the modal is dismissed (X, Escape, or Cancel). */
  onClose: () => void;
  /** Called on every keystroke in the filename input. */
  onFileNameChange: (value: string) => void;
  /** Called on every keystroke to clear any previous validation error. */
  onClearError: () => void;
}

const ExportModal = ({
  isOpen,
  isExporting,
  csvFileName,
  exportErrorMessage,
  onConfirm,
  onClose,
  onFileNameChange,
  onClearError,
}: ExportModalProps) => (
  <Modal
    open={isOpen}
    size="sm"
    modalHeading="Export as CSV"
    primaryButtonText={isExporting ? "Exporting..." : "Export"}
    primaryButtonDisabled={isExporting}
    secondaryButtonText="Cancel"
    onRequestSubmit={onConfirm}
    onRequestClose={() => {
      if (!isExporting) {
        onClose();
      }
    }}
  >
    <TextInput
      id="csv-file-name"
      labelText="File name"
      value={csvFileName}
      invalid={!!exportErrorMessage}
      invalidText={exportErrorMessage}
      onChange={(e) => {
        onFileNameChange(e.target.value);
        onClearError();
      }}
    />
  </Modal>
);

export default ExportModal;
