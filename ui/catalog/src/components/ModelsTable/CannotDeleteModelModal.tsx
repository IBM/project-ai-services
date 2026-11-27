import { useEffect, useState } from "react";
import {
  Modal,
  InlineLoading,
  InlineNotification,
  UnorderedList,
  ListItem,
} from "@carbon/react";
import { fetchModelDeployment } from "@/api/models.api";
import type { ModelApplication } from "@/types/api.types";
import styles from "./CannotDeleteModelModal.module.scss";

export interface CannotDeleteModelModalProps {
  isOpen: boolean;
  modelId: string | null;
  servicesCount: number;
  onClose: () => void;
}

const CannotDeleteModelModal = ({
  isOpen,
  modelId,
  servicesCount,
  onClose,
}: CannotDeleteModelModalProps) => {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [applications, setApplications] = useState<ModelApplication[]>([]);

  useEffect(() => {
    if (!isOpen || !modelId) {
      return;
    }

    let isMounted = true;

    const loadDetails = async () => {
      setIsLoading(true);
      setError(null);
      try {
        const detail = await fetchModelDeployment(modelId);
        if (isMounted) {
          setApplications(detail.applications ?? []);
        }
      } catch (err) {
        if (isMounted) {
          const msg =
            err instanceof Error
              ? err.message
              : "Failed to load connected services details";
          setError(msg);
        }
      } finally {
        if (isMounted) {
          setIsLoading(false);
        }
      }
    };

    void loadDetails();

    return () => {
      isMounted = false;
    };
  }, [isOpen, modelId]);

  const count = applications.length > 0 ? applications.length : servicesCount;
  const isMultiple = count > 1;

  return (
    <Modal
      open={isOpen}
      size="sm"
      passiveModal
      modalHeading="Cannot delete model"
      onRequestClose={onClose}
    >
      <div className={styles.modalBody}>
        <p className={styles.description}>
          {isMultiple
            ? "This model is in use by the following services. Delete them and then try again."
            : "This model is in use by the following service. Delete the service and then try again."}
        </p>

        {isLoading && (
          <div className={styles.loadingWrapper}>
            <InlineLoading description="Loading connected services..." />
          </div>
        )}

        {error && (
          <InlineNotification
            kind="error"
            lowContrast
            hideCloseButton
            title="Error"
            subtitle={error}
            className={styles.errorNotification}
          />
        )}

        {!isLoading && applications.length > 0 && (
          <UnorderedList className={styles.servicesList}>
            {applications.map((app) => (
              <ListItem key={app.id}>{app.name}</ListItem>
            ))}
          </UnorderedList>
        )}
      </div>
    </Modal>
  );
};

export default CannotDeleteModelModal;
