import { useState, useEffect, useRef, useCallback } from 'react';
import {
  TextInput,
  RadioButtonGroup,
  RadioButton,
  FileUploaderButton,
  FileUploaderItem,
  InlineNotification,
  ToastNotification,
} from '@carbon/react';
import { SidePanel } from '@carbon/ibm-products';
import styles from './IngestSidePanel.module.scss';

interface IngestSidePanelProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (operation: string, outputFormat: string, files: File[], jobName: string) => Promise<void>;
  onSubmittingChange?: (isSubmitting: boolean) => void;
}

interface FileItem {
  uuid: string;
  name: string;
  filesize: number;
  status: 'edit' | 'complete' | 'uploading';
  iconDescription: string;
  invalid: boolean;
  file: File;
}

let lastId = 0;
function uid(prefix = 'file') {
  lastId++;
  return `${prefix}-${lastId}`;
}

const IngestSidePanel = ({ open, onClose, onSubmit, onSubmittingChange }: IngestSidePanelProps) => {
  const [jobName, setJobName] = useState('');
  const [operation, setOperation] = useState('ingestion');
  const [outputFormat, setOutputFormat] = useState('json');
  const [fileItems, setFileItems] = useState<FileItem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);

  // Callback ref to capture the actual input element
  const setInputRef = useCallback((node: HTMLInputElement | null) => {
    inputRef.current = node;
  }, []);

  useEffect(() => {
    if (open) {
      // Delay to allow SidePanel animation to complete
      const timer = setTimeout(() => {
        if (inputRef.current) {
          inputRef.current.focus();
        }
      }, 200);
      
      return () => clearTimeout(timer);
    }
  }, [open]);

  const handleFileAdd = useCallback((event: any) => {
    const addedFiles = event.target.files;
    if (addedFiles && addedFiles.length > 0) {
      const newFileItems = Array.from(addedFiles).map((file: any) => ({
        uuid: uid(),
        name: file.name,
        filesize: file.size,
        status: 'edit' as const,
        iconDescription: 'Delete file',
        invalid: false,
        file: file,
      }));
      setFileItems((prev) => [...prev, ...newFileItems]);
    }
  }, []);

  const handleFileDelete = useCallback((_event: any, { uuid }: { uuid: string }) => {
    if (isSubmitting) return; // Prevent deletion during submission
    setFileItems((prev) => prev.filter((item) => item.uuid !== uuid));
  }, [isSubmitting]);

  const handleSubmit = async () => {
    setError(null);
    
    if (!jobName.trim()) {
      setError('Please enter a job name');
      return;
    }
    if (fileItems.length === 0) {
      setError('Please upload at least one file');
      return;
    }
    
    setIsSubmitting(true);
    onSubmittingChange?.(true);
    try {
      const files = fileItems.map((item) => item.file);
      await onSubmit(operation, outputFormat, files, jobName);
      handleClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred during submission');
    } finally {
      setIsSubmitting(false);
      onSubmittingChange?.(false);
    }
  };

  const handleClose = () => {
    if (isSubmitting) return; // Prevent closing while submitting
    setJobName('');
    setOperation('ingestion');
    setOutputFormat('json');
    setFileItems([]);
    setError(null);
    setIsSubmitting(false);
    onClose();
  };

  return (
    <SidePanel
      open={open}
      onRequestClose={handleClose}
      title="Trigger a job"
      actions={[
        {
          kind: 'secondary',
          label: 'Cancel',
          onClick: handleClose,
          disabled: isSubmitting,
        },
        {
          kind: 'primary',
          label: isSubmitting ? 'Submitting...' : 'Submit',
          onClick: handleSubmit,
          disabled: isSubmitting,
        },
      ]}
      className={styles.ingestSidePanel}
      size="md"
    >
      <div className={styles.sidePanelContent}>
        {/* Submitting Toast Notification */}
        {isSubmitting && (
          <ToastNotification
            kind="info"
            title="Processing request"
            subtitle="Your job is being submitted. Please wait..."
            timeout={0}
            lowContrast
            hideCloseButton
            style={{ marginBottom: '1rem' }}
          />
        )}

        {/* Error Notification */}
        {error && (
          <InlineNotification
            kind="error"
            title="Validation Error"
            subtitle={error}
            onCloseButtonClick={() => setError(null)}
            lowContrast
            hideCloseButton={false}
            style={{ marginBottom: '1rem' }}
          />
        )}

        {/* Job Name Input */}
        <div className={styles.formGroup}>
          <TextInput
            id="job-name"
            size="lg"
            labelText="Job name *"
            placeholder="Enter job name (required)"
            value={jobName}
            onChange={(e) => {
              setJobName(e.target.value);
              if (error) setError(null);
            }}
            ref={setInputRef}
            required
            invalid={error?.includes('job name')}
            invalidText={error?.includes('job name') ? error : ''}
            disabled={isSubmitting}
          />
        </div>

        {/* Operation Type Radio Buttons */}
        <div className={styles.formGroup}>
          <RadioButtonGroup
            name="operation"
            valueSelected={operation}
            onChange={(value) => setOperation(value as string)}
            orientation="horizontal"
            disabled={isSubmitting}
          >
            <RadioButton
              labelText="Ingestion"
              value="ingestion"
              id="operation-ingestion"
              disabled={isSubmitting}
            />
            <RadioButton
              labelText="Digitization only"
              value="digitization"
              id="operation-digitization"
              disabled={isSubmitting}
            />
          </RadioButtonGroup>
        </div>

        {/* Output Format Radio Buttons - Only show for Digitization only */}
        {operation === 'digitization' && (
          <div className={styles.formGroup}>
            <label className={styles.formLabel}>Output format</label>
            <RadioButtonGroup
              name="output-format"
              valueSelected={outputFormat}
              onChange={(value) => setOutputFormat(value as string)}
              orientation="horizontal"
              disabled={isSubmitting}
            >
              <RadioButton
                labelText="JSON"
                value="json"
                id="format-json"
                disabled={isSubmitting}
              />
              <RadioButton
                labelText="Markdown"
                value="md"
                id="format-markdown"
                disabled={isSubmitting}
              />
              <RadioButton
                labelText="Text"
                value="txt"
                id="format-text"
                disabled={isSubmitting}
              />
            </RadioButtonGroup>
          </div>
        )}

        {/* Upload Files Section */}
        <div className={styles.formGroup}>
          <strong className={styles.fileUploaderLabel}>Upload files</strong>
          <p className={styles.fileUploaderDescription}>
            Supported file type is .pdf only.
            <br /><br />
            Supported languages are English and German.
            <br /><br />
            Supported contents are text and tables.
          </p>
          <FileUploaderButton
            labelText="Upload"
            buttonKind="tertiary"
            size="md"
            accept={['.pdf']}
            multiple
            onChange={handleFileAdd}
            disableLabelChanges
            disabled={isSubmitting}
          />
          <div className={styles.fileContainer}>
            {fileItems.map((item) => (
              <FileUploaderItem
                key={item.uuid}
                uuid={item.uuid}
                name={item.name}
                size="md"
                status={item.status}
                iconDescription={item.iconDescription}
                invalid={item.invalid}
                onDelete={handleFileDelete}
              />
            ))}
          </div>
          {fileItems.length > 0 && (
            <div style={{ marginTop: '0.5rem', fontSize: '0.875rem', color: 'var(--cds-text-secondary)' }}>
              {fileItems.length} file{fileItems.length !== 1 ? 's' : ''} selected
            </div>
          )}
        </div>
      </div>
    </SidePanel>
  );
};

export default IngestSidePanel;

// Made with Bob