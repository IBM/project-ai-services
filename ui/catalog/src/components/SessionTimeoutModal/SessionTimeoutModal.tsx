import { Modal } from "@carbon/react";
import styles from "./SessionTimeoutModal.module.scss";

interface SessionTimeoutModalProps {
  open: boolean;
  timeRemaining: string;
  onExtendSession: () => void;
}

const SessionTimeoutModal = ({
  open,
  timeRemaining,
  onExtendSession,
}: SessionTimeoutModalProps) => {
  return (
    <Modal
      open={open}
      modalHeading="Session Timeout Warning"
      primaryButtonText="Stay Logged In"
      onRequestSubmit={onExtendSession}
      onRequestClose={onExtendSession}
      preventCloseOnClickOutside
      size="sm"
      danger={false}
    >
      <div className={styles.modalContent}>
        <p className={styles.message}>
          Your session is about to expire due to inactivity.
        </p>
        <div className={styles.countdown}>
          <p className={styles.countdownLabel}>Time remaining:</p>
          <p className={styles.countdownTime}>{timeRemaining}</p>
        </div>
        <p className={styles.instruction}>
          Click &ldquo;Stay Logged In&rdquo; to continue your session, or you
          will be automatically logged out.
        </p>
      </div>
    </Modal>
  );
};

export default SessionTimeoutModal;
