import { createPortal } from "react-dom";
import {
  Button,
  Column,
  Grid,
  IconButton,
  TextInput,
  ToastNotification,
} from "@carbon/react";
import { Edit } from "@carbon/icons-react";
import { useInlineEdit } from "@/hooks/useInlineEdit";
import styles from "./InlineEditableTitle.module.scss";

export interface InlineEditableTitleProps {
  /** Current persisted value */
  value: string;
  /** Persists the new value; reject/throw to surface an error toast */
  onSave: (newValue: string) => Promise<void>;
  /** Accessible label of the field, e.g. "Deployment name" */
  label: string;
  /** Unique id for the text input */
  id: string;
  /** Title of the success toast */
  successTitle?: string;
  /** Title of the error toast */
  errorTitle?: string;
}

// Title text with an inline edit mode (input + Save/Cancel) and success/error toasts
const InlineEditableTitle = ({
  value,
  onSave,
  label,
  id,
  successTitle = "Name updated successfully",
  errorTitle = "Failed to update name",
}: InlineEditableTitleProps) => {
  const {
    editedValue,
    isEditing,
    isSaving,
    error,
    saveSuccess,
    inputRef,
    startEditing,
    cancel,
    handleChange,
    save,
    clearError,
    clearSuccess,
  } = useInlineEdit({ value, onSave });

  const editLabel = `Edit ${label.toLowerCase()}`;

  return (
    <>
      {/* Portal: PageHeader's title row has a CSS transform, which breaks position: fixed */}
      {createPortal(
        <>
          {saveSuccess && (
            <ToastNotification
              kind="success"
              title={successTitle}
              timeout={5000}
              onClose={clearSuccess}
              className={styles.toastNotification}
            />
          )}
          {error && (
            <ToastNotification
              kind="error"
              title={errorTitle}
              subtitle={error}
              onClose={clearError}
              className={styles.toastNotification}
            />
          )}
        </>,
        document.body,
      )}
      <div>
        {isEditing ? (
          <Grid className={styles.inlineEditRow} narrow>
            <Column sm={4} md={4} lg={8}>
              <TextInput
                ref={inputRef}
                id={id}
                labelText={label}
                hideLabel
                size="md"
                value={editedValue}
                onChange={(e) => handleChange(e.target.value)}
                disabled={isSaving}
                onKeyDown={(e) => {
                  if (e.key === "Enter") save();
                  if (e.key === "Escape") cancel();
                }}
              />
            </Column>
            <Column sm={2} md={2} lg={2} className={styles.inlineEditButtonCol}>
              <Button
                kind="secondary"
                size="md"
                onClick={cancel}
                disabled={isSaving}
                className={styles.inlineEditButton}
              >
                Cancel
              </Button>
            </Column>
            <Column sm={2} md={2} lg={2} className={styles.inlineEditButtonCol}>
              <Button
                kind="tertiary"
                size="md"
                onClick={save}
                disabled={isSaving || !editedValue.trim()}
                className={styles.inlineEditButton}
              >
                {isSaving ? "Saving..." : "Save"}
              </Button>
            </Column>
          </Grid>
        ) : (
          <div className={styles.titleRow}>
            <span>{value}</span>
            <IconButton
              label={editLabel}
              kind="ghost"
              size="md"
              align="right"
              onClick={startEditing}
            >
              <Edit size={16} />
            </IconButton>
          </div>
        )}
      </div>
    </>
  );
};

export default InlineEditableTitle;
