import { useMemo } from "react";
import {
  Modal,
  TextInput,
  Button,
  InlineLoading,
  InlineNotification,
  CodeSnippet,
  Theme,
} from "@carbon/react";
import styles from "./RegisterWorkerModal.module.scss";
import type { RegisterPhase } from "./types";

export interface RegisterWorkerModalProps {
  isOpen: boolean;
  phase: RegisterPhase;
  workerName: string;
  token: string;
  errorMessage: string;
  onWorkerNameChange: (value: string) => void;
  onGenerateToken: () => void;
  onClose: () => void;
}

const RegisterWorkerModal = ({
  isOpen,
  phase,
  workerName,
  token,
  errorMessage,
  onWorkerNameChange,
  onGenerateToken,
  onClose,
}: RegisterWorkerModalProps) => {
  const isLoading = phase === "loading";
  const isSuccess = phase === "success";

  const runCommand = useMemo(() => {
    if (!isSuccess) return "";
    return [
      "ai-services agent start \\",
      `  --server <host>:${workerName} \\`,
      `  --name ${workerName} \\`,
      `  --token ${token}`,
    ].join("\n");
  }, [isSuccess, workerName, token]);

  return (
    <Modal
      open={isOpen}
      size="sm"
      modalHeading="Register worker resource"
      passiveModal
      onRequestClose={() => {
        if (!isLoading) onClose();
      }}
    >
      <div className={styles.modalBody}>
        <p className={styles.description}>
          Registering a worker resource requires a one-time bootstrap token.
          Generate the token, then run the provided command on the partition,
          virtual machine, or cluster you want to register. Once registered, the
          worker resource will appear in the Workers list and be ready to run
          services.
        </p>

        <TextInput
          id="register-worker-name"
          labelText="Worker resource name"
          value={workerName}
          disabled={isLoading}
          readOnly={isSuccess}
          invalid={phase === "invalid"}
          invalidText="Enter a valid worker resource name"
          onChange={(e) => onWorkerNameChange(e.target.value)}
        />

        {!isSuccess && (
          <div className={styles.generateRow}>
            {isLoading ? (
              <InlineLoading description="Generating token..." />
            ) : (
              <Button kind="tertiary" size="md" onClick={onGenerateToken}>
                Generate token
              </Button>
            )}
            {phase === "error" && (
              <p className={styles.errorText}>{errorMessage}</p>
            )}
          </div>
        )}

        {isSuccess && (
          <div className={styles.successContent}>
            <InlineNotification
              kind="success"
              title=""
              lowContrast
              hideCloseButton
            >
              <div className={styles.notificationContent}>
                <p className={styles.notificationTitle}>Token issued</p>
                <p className={styles.notificationSubtitle}>
                  The bootstrap token will expire after 24-hours.
                </p>
              </div>
            </InlineNotification>
            <p className={styles.runLabel}>Run command</p>
            <Theme theme="g100">
              <CodeSnippet type="multi" feedback="Copied to clipboard">
                {runCommand}
              </CodeSnippet>
            </Theme>
          </div>
        )}
      </div>
    </Modal>
  );
};

export default RegisterWorkerModal;
