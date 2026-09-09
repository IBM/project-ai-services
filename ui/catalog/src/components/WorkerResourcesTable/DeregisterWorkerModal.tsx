import { Modal, InlineNotification, CodeSnippet, Theme } from "@carbon/react";
import styles from "./DeregisterWorkerModal.module.scss";

export interface DeregisterWorkerModalProps {
  isOpen: boolean;
  isDeregistering: boolean;
  workerName: string;
  workerStatus: string;
  runtimeType: string;
  onConfirm: () => void;
  onClose: () => void;
}

const buildCleanupCommand = (runtimeType: string) =>
  `ai-services worker uninstall --runtime ${!runtimeType || runtimeType === "unknown" ? "<podman|openshift>" : runtimeType} --yes`;

const DeregisterWorkerModal = ({
  isOpen,
  isDeregistering,
  workerName,
  workerStatus,
  runtimeType,
  onConfirm,
  onClose,
}: DeregisterWorkerModalProps) => {
  const isPending = workerStatus === "pending";

  return (
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
        {isPending ? (
          <InlineNotification
            kind="info"
            title="This worker never completed registration and has no components to clean up on the node"
            lowContrast
            hideCloseButton
          />
        ) : (
          <InlineNotification
            kind="warning"
            title="Ensure no services are actively running on this node before deregistering"
            lowContrast
            hideCloseButton
          />
        )}

        <p className={styles.description}>
          Deregistering a worker resource will remove the node from AI
          Launchpad. The resource itself will not be deleted and can be
          re-registered at any time.
        </p>

        {!isPending && (
          <>
            <p className={styles.description}>
              To clean up AI Launchpad configuration and dependencies on the
              resource, run the provided script after deregistering.
            </p>

            <p className={styles.runLabel}>Run command</p>
            <Theme theme="g100">
              <CodeSnippet
                type="multi"
                feedback="Copied to clipboard"
                copyButtonDescription="Copy cleanup command"
              >
                {buildCleanupCommand(runtimeType)}
              </CodeSnippet>
            </Theme>
          </>
        )}
      </div>
    </Modal>
  );
};

export default DeregisterWorkerModal;
