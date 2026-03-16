import { useState, useEffect, useRef, useCallback } from 'react';
import {
  TextInput,
  RadioButtonGroup,
  RadioButton,
  FileUploader,
} from '@carbon/react';
import { SidePanel } from '@carbon/ibm-products';
import styles from './IngestSidePanel.module.scss';

interface IngestSidePanelProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (operation: string, outputFormat: string, files: File[]) => void;
}

const IngestSidePanel = ({ open, onClose, onSubmit }: IngestSidePanelProps) => {
  const [jobName, setJobName] = useState('');
  const [operation, setOperation] = useState('ingestion');
  const [outputFormat, setOutputFormat] = useState('json');
  const [files, setFiles] = useState<File[]>([]);
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

  const handleSubmit = () => {
    if (files.length === 0) {
      alert('Please upload at least one file');
      return;
    }
    onSubmit(operation, outputFormat, files);
    handleClose();
  };

  const handleClose = () => {
    setJobName('');
    setOperation('ingestion');
    setOutputFormat('json');
    setFiles([]);
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
        },
        {
          kind: 'primary',
          label: 'Ingest',
          onClick: handleSubmit,
        },
      ]}
      className={styles.ingestSidePanel}
      size="md"
    >
      <div className={styles.sidePanelContent}>
        {/* Job Name Input */}
        <div className={styles.formGroup}>
          <TextInput
            id="job-name"
            size="lg"
            labelText="Job name"
            placeholder="Enter job name"
            value={jobName}
            onChange={(e) => setJobName(e.target.value)}
            ref={setInputRef}
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
                value="text"
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