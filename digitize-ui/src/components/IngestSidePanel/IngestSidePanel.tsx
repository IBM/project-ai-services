import { useState, useEffect, useRef, useCallback } from 'react';
import {
  TextInput,
  RadioButtonGroup,
  RadioButton,
  FileUploader,
  InlineNotification,
  ToastNotification,
} from '@carbon/react';
import { SidePanel } from '@carbon/ibm-products';
import styles from './IngestSidePanel.module.scss';

interface IngestSidePanelProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (operation: string, outputFormat: string, files: File[], jobName: string) => Promise<void>;
}

const IngestSidePanel = ({ open, onClose, onSubmit }: IngestSidePanelProps) => {
  const [jobName, setJobName] = useState('');
  const [operation, setOperation] = useState('ingestion');
  const [outputFormat, setOutputFormat] = useState('json');
  const [files, setFiles] = useState<File[]>([]);
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

  const handleFileAdd = (event: any) => {
    const addedFiles = event.target.files;
    if (addedFiles && addedFiles.length > 0) {
      const newFiles = Array.from(addedFiles) as File[];
      setFiles((prevFiles) => [...prevFiles, ...newFiles]);
    }
  };

  const handleSubmit = async () => {
    setError(null);
    
    if (!jobName.trim()) {
      setError('Please enter a job name');
      return;
    }
    if (files.length === 0) {
      setError('Please upload at least one file');
      return;
    }
    
    setIsSubmitting(true);
    try {
      await onSubmit(operation, outputFormat, files, jobName);
      handleClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred during submission');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleClose = () => {
    if (isSubmitting) return; // Prevent closing while submitting
    setJobName('');
    setOperation('ingestion');
    setOutputFormat('json');
    setFiles([]);
    setError(null);
    setIsSubmitting(false);
    onClose();
  };

  return (
    <SidePanel
      open={open}
      onRequestClose={handleClose}
      title="Ingest"
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
          />
        </div>

        {/* Operation Type Radio Buttons */}
        <div className={styles.formGroup}>
          <RadioButtonGroup
            name="operation"
            valueSelected={operation}
            onChange={(value) => setOperation(value as string)}
            orientation="horizontal"
          >
            <RadioButton
              labelText="Ingestion"
              value="ingestion"
              id="operation-ingestion"
            />
            <RadioButton
              labelText="Digitization only"
              value="digitization"
              id="operation-digitization"
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
            >
              <RadioButton
                labelText="JSON"
                value="json"
                id="format-json"
              />
              <RadioButton
                labelText="Markdown"
                value="md"
                id="format-markdown"
              />
              <RadioButton
                labelText="Text"
                value="txt"
                id="format-text"
              />
            </RadioButtonGroup>
          </div>
        )}

        {/* Upload Files Section */}
        <div className={styles.formGroup}>
          <FileUploader
            labelTitle="Upload files"
            labelDescription={`Supported file types are .pdf only,

Supported languages are English, German, Italian and French.

Supported content are text, tables`}
            buttonLabel="Upload"
            buttonKind="tertiary"
            size="md"
            filenameStatus="edit"
            accept={['.pdf']}
            multiple
            onChange={handleFileAdd}
            iconDescription="Upload files"
            className={styles.fileUploader}
          />
        </div>
      </div>
    </SidePanel>
  );
};

export default IngestSidePanel;

// Made with Bob