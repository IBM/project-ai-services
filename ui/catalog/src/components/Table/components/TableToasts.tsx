import { ActionableNotification, ToastNotification } from "@carbon/react";
import sharedStyles from "@/components/Table/table.shared.module.scss";

interface TableToastsProps {
  // --- Delete error toast ---
  /** Whether the delete-error toast is visible. */
  toastOpen: boolean;
  /** Name of the row that failed to delete — shown in the toast title. */
  deleteErrorRowName: string;
  /** Error message detail shown as the toast subtitle. */
  deleteErrorMessage: string;
  /**
   * Short label for the entity type, e.g. "digital assistant" */
  entityLabel: string;
  /** Called when the close button on the delete-error toast is clicked. */
  onDeleteErrorClose: () => void;
  /** Called when the "Try again" action button is clicked. */
  onDeleteErrorRetry: () => Promise<void>;

  /** Whether the export toast is visible. */
  exportToastOpen: boolean;
  /** "success" | "error" — controls the Carbon notification kind. */
  exportToastKind: "success" | "error";
  /** Detail message shown as the toast subtitle. */
  exportToastMessage: string;
  /** Called when the close button on the export toast is clicked. */
  onExportToastClose: () => void;
}

const TableToasts = ({
  toastOpen,
  deleteErrorRowName,
  deleteErrorMessage,
  entityLabel,
  onDeleteErrorClose,
  onDeleteErrorRetry,
  exportToastOpen,
  exportToastKind,
  exportToastMessage,
  onExportToastClose,
}: TableToastsProps) => (
  <>
    {toastOpen && (
      <ActionableNotification
        actionButtonLabel="Try again"
        aria-label="close notification"
        kind="error"
        closeOnEscape
        title={`Delete ${entityLabel} ${deleteErrorRowName} failed`}
        subtitle={deleteErrorMessage}
        onCloseButtonClick={onDeleteErrorClose}
        onActionButtonClick={onDeleteErrorRetry}
        className={sharedStyles.customToast}
      />
    )}
    {exportToastOpen && (
      <ToastNotification
        aria-label="close notification"
        kind={exportToastKind}
        title={
          exportToastKind === "success" ? "Export successful" : "Export failed"
        }
        subtitle={exportToastMessage}
        onCloseButtonClick={onExportToastClose}
        className={sharedStyles.customToast}
        hideCloseButton={false}
      />
    )}
  </>
);

export default TableToasts;
