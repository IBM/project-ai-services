import { Modal, InlineNotification, CodeSnippet, Theme } from "@carbon/react";
import styles from "./DeregisterWorkerModal.module.scss";

export interface DeregisterWorkerModalProps {
  isOpen: boolean;
  isDeregistering: boolean;
  workerName: string;
  onConfirm: () => void;
  onClose: () => void;
}

const buildCleanupCommand = (workerName: string) =>
  `ai-services catalog worker deregister "${workerName}"`;

const DeregisterWorkerModal = ({
  isOpen,
  isDeregistering,
  workerName,
  onConfirm,
  onClose,
}: DeregisterWorkerModalProps) => (
  <Modal
    open={isOpen}
    size="sm"
    modalLabel={`Deregister ${workerName}`}
    modalHeading="Deregister worker resource"
    primaryButtonText={isDeregistering ? "Deregistering..." : "Deregister"}
    secondaryButtonText="Cancel"
    primaryButtonDisabled={isDeregistering}
    preventCloseOnClickOutside
    onRequestSubmit={onConfirm}
    onRequestClose={onClose}
  >
    <div className={styles.modalBody}>
      <InlineNotification
        kind="warning"
        title="Ensure no services are actively running on this node before deregistering"
        lowContrast
        hideCloseButton
      />

      <p className={styles.description}>
        Deregistering a worker resource will remove the node from AI Launchpad.
        The resource itself will not be deleted and can be re-registered at any
        time.
      </p>

      <p className={styles.description}>
        To clean up AI Launchpad configuration and dependencies on the resource,
        run the provided script after deregistering.
      </p>

      <p className={styles.runLabel}>Run command</p>
      <Theme theme="g100">
        <CodeSnippet
          type="multi"
          feedback="Copied to clipboard"
          copyButtonDescription="Copy cleanup command"
        >
          {buildCleanupCommand(workerName)}
        </CodeSnippet>
      </Theme>
    </div>
  </Modal>
);

export default DeregisterWorkerModal;
