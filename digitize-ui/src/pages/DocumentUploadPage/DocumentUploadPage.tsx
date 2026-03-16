import { useReducer } from 'react';
import { useNavigate } from 'react-router-dom';
import { PageHeader } from '@carbon/ibm-products';
import {
  Grid,
  Column,
  FileUploader,
  Button,
  RadioButtonGroup,
  RadioButton,
  InlineNotification,
  Loading,
  Tile,
  ProgressIndicator,
  ProgressStep,
  Accordion,
  AccordionItem,
  Theme,
  TextInput,
} from '@carbon/react';
import { Upload, DocumentPdf, Close, Checkmark, Renew, View } from '@carbon/icons-react';
import { useTheme } from '../../contexts/useTheme';
import { uploadDocuments } from '../../services/api';
import styles from './DocumentUploadPage.module.scss';

interface DocumentUploadState {
  files: File[];
  jobName: string;
  operation: string;
  outputFormat: string;
  loading: boolean;
  error: string | null;
  success: string | null;
  currentStep: number;
  fileUploaderKey: number;
  isFileListExpanded: boolean;
  isCompleted: boolean;
  jobId: string | null;
}

type DocumentUploadAction =
  | { type: 'SET_FILES'; payload: File[] }
  | { type: 'SET_JOB_NAME'; payload: string }
  | { type: 'SET_OPERATION'; payload: string }
  | { type: 'SET_OUTPUT_FORMAT'; payload: string }
  | { type: 'SET_LOADING'; payload: boolean }
  | { type: 'SET_ERROR'; payload: string | null }
  | { type: 'SET_SUCCESS'; payload: string | null }
  | { type: 'SET_CURRENT_STEP'; payload: number }
  | { type: 'INCREMENT_FILE_UPLOADER_KEY' }
  | { type: 'TOGGLE_FILE_LIST_EXPANDED' }
  | { type: 'SET_IS_COMPLETED'; payload: boolean }
  | { type: 'SET_JOB_ID'; payload: string | null }
  | { type: 'UPLOAD_START' }
  | { type: 'UPLOAD_SUCCESS'; payload: { jobId: string; message: string } }
  | { type: 'UPLOAD_ERROR'; payload: string }
  | { type: 'RESET_FORM' }
  | { type: 'CLEAR_FILES' };

const initialState: DocumentUploadState = {
  files: [],
  jobName: '',
  operation: 'ingestion',
  outputFormat: 'json',
  loading: false,
  error: null,
  success: null,
  currentStep: 0,
  fileUploaderKey: 0,
  isFileListExpanded: true,
  isCompleted: false,
  jobId: null,
};

const documentUploadReducer = (
  state: DocumentUploadState,
  action: DocumentUploadAction
): DocumentUploadState => {
  switch (action.type) {
    case 'SET_FILES':
      return {
        ...state,
        files: action.payload,
        error: null,
        success: null,
        currentStep: action.payload.length > 0 ? 3 : state.currentStep,
      };
    case 'SET_JOB_NAME':
      return {
        ...state,
        jobName: action.payload,
        error: null,
        success: null,
      };
    case 'SET_OPERATION':
      return {
        ...state,
        operation: action.payload,
        currentStep: 1,
      };
    case 'SET_OUTPUT_FORMAT':
      return {
        ...state,
        outputFormat: action.payload,
      };
    case 'SET_LOADING':
      return {
        ...state,
        loading: action.payload,
      };
    case 'SET_ERROR':
      return {
        ...state,
        error: action.payload,
      };
    case 'SET_SUCCESS':
      return {
        ...state,
        success: action.payload,
      };
    case 'SET_CURRENT_STEP':
      return {
        ...state,
        currentStep: action.payload,
      };
    case 'INCREMENT_FILE_UPLOADER_KEY':
      return {
        ...state,
        fileUploaderKey: state.fileUploaderKey + 1,
      };
    case 'TOGGLE_FILE_LIST_EXPANDED':
      return {
        ...state,
        isFileListExpanded: !state.isFileListExpanded,
      };
    case 'SET_IS_COMPLETED':
      return {
        ...state,
        isCompleted: action.payload,
      };
    case 'SET_JOB_ID':
      return {
        ...state,
        jobId: action.payload,
      };
    case 'UPLOAD_START':
      return {
        ...state,
        loading: true,
        error: null,
        success: null,
      };
    case 'UPLOAD_SUCCESS':
      return {
        ...state,
        loading: false,
        jobId: action.payload.jobId,
        success: action.payload.message,
        files: [],
        jobName: '',
        currentStep: 4,
        isCompleted: true,
      };
    case 'UPLOAD_ERROR':
      return {
        ...state,
        loading: false,
        error: action.payload,
      };
    case 'RESET_FORM':
      return {
        ...initialState,
        fileUploaderKey: state.fileUploaderKey + 1,
      };
    case 'CLEAR_FILES':
      return {
        ...state,
        files: [],
        currentStep: 2,
        fileUploaderKey: state.fileUploaderKey + 1,
      };
    default:
      return state;
  }
};

const DocumentUploadPage = () => {
  const navigate = useNavigate();
  const { effectiveTheme } = useTheme();
  const [state, dispatch] = useReducer(documentUploadReducer, initialState);

  const handleFileChange = (event: any) => {
    const selectedFiles = Array.from((event.target?.files || []) as FileList);
    dispatch({ type: 'SET_FILES', payload: selectedFiles });
  };

  const handleOperationChange = (value: string | number | undefined) => {
    if (value !== undefined) {
      dispatch({ type: 'SET_OPERATION', payload: String(value) });
    }
  };

  const handleUpload = async () => {
    if (!state.jobName.trim()) {
      dispatch({ type: 'SET_ERROR', payload: 'Please enter a job name' });
      return;
    }

    if (state.files.length === 0) {
      dispatch({ type: 'SET_ERROR', payload: 'Please select at least one file to upload' });
      return;
    }

    if (state.operation === 'digitization' && state.files.length > 1) {
      dispatch({ type: 'SET_ERROR', payload: 'Only 1 file allowed for digitization operation' });
      return;
    }

    dispatch({ type: 'UPLOAD_START' });

    try {
      const result = await uploadDocuments(state.files, state.operation, state.outputFormat, state.jobName);
      dispatch({
        type: 'UPLOAD_SUCCESS',
        payload: {
          jobId: result.job_id,
          message: `Upload successful! Job ID: ${result.job_id}`,
        },
      });
    } catch (err) {
      const error = err as any;
      const errorMessage = error.response?.data?.detail || error.message || 'Upload failed';
      dispatch({ type: 'UPLOAD_ERROR', payload: errorMessage });
    }
  };

  const handleUploadMore = () => {
    dispatch({ type: 'RESET_FORM' });
  };

  const handleViewJobs = () => {
    navigate('/jobs');
  };

  return (
    <Theme theme={effectiveTheme}>
      <PageHeader
        title={{ text: 'Upload Documents' }}
        subtitle="Upload PDF documents for processing and digitization"
      />

      <Grid fullWidth className={styles.mainGrid}>
        {/* Left Column - Form */}
        <Column lg={10} md={6} sm={4}>
          <div className={styles.uploadContent}>
            {/* Progress Indicator */}
            <div className={styles.progressSection}>
              <ProgressIndicator currentIndex={state.currentStep} spaceEqually>
                <ProgressStep
                  label="Select operation"
                  description="Choose processing type"
                />
                <ProgressStep
                  label="Enter job name"
                  description="Provide a name for this job"
                />
                <ProgressStep
                  label="Upload files"
                  description="Select PDF documents"
                />
                <ProgressStep
                  label="Process"
                  description="Submit for processing"
                />
                <ProgressStep
                  label="Complete"
                  description="View results"
                />
              </ProgressIndicator>
            </div>

            {/* Completion State */}
            {state.isCompleted ? (
              <div className={styles.completionContainer}>
                <Tile className={styles.completionTile}>
                  <div className={styles.completionContent}>
                    <div className={styles.completionIcon}>
                      <Checkmark size={48} />
                    </div>
                    <h3 className={styles.completionTitle}>Upload Complete!</h3>
                    <p className={styles.completionMessage}>
                      Your document{state.files.length > 1 ? 's have' : ' has'} been successfully uploaded and processing has started.
                    </p>
                    {state.jobId && (
                      <div className={styles.jobIdContainer}>
                        <span className={styles.jobIdLabel}>Job ID:</span>
                        <code className={styles.jobId}>{state.jobId}</code>
                      </div>
                    )}
                  </div>
                  
                  <div className={styles.completionActions}>
                    <Button
                      kind="primary"
                      renderIcon={Renew}
                      onClick={handleUploadMore}
                      size="lg"
                    >
                      Upload More Documents
                    </Button>
                    <Button
                      kind="secondary"
                      renderIcon={View}
                      onClick={handleViewJobs}
                      size="lg"
                    >
                      View Running Jobs
                    </Button>
                  </div>
                </Tile>
              </div>
            ) : (
              <>
                {/* Operation Type */}
                <Tile className={styles.formTile}>
                  <div className={styles.tileHeader}>
                    <h4>Step 1: Select Operation Type</h4>
                    <p className={styles.tileDescription}>
                      Choose how you want to process your documents
                    </p>
                  </div>
                  <RadioButtonGroup
                    legendText=""
                    name="operation"
                    valueSelected={state.operation}
                    onChange={handleOperationChange}
                    orientation="vertical"
                  >
                    <RadioButton
                      labelText="Ingestion"
                      value="ingestion"
                      id="operation-ingestion"
                    />
                    <RadioButton
                      labelText="Digitization"
                      value="digitization"
                      id="operation-digitization"
                    />
                  </RadioButtonGroup>
                </Tile>

                {/* Job Name Input */}
                <Tile className={styles.formTile}>
                  <div className={styles.tileHeader}>
                    <h4>Step 2: Enter Job Name</h4>
                    <p className={styles.tileDescription}>
                      Provide a descriptive name for this processing job
                    </p>
                  </div>
                  <TextInput
                    id="job-name-input"
                    labelText="Job Name"
                    placeholder="e.g., Q4 Financial Reports"
                    value={state.jobName}
                    onChange={(e) => {
                      dispatch({ type: 'SET_JOB_NAME', payload: e.target.value });
                      if (e.target.value.trim()) {
                        dispatch({ type: 'SET_CURRENT_STEP', payload: 2 });
                      }
                    }}
                    invalid={state.error?.includes('job name')}
                    invalidText={state.error?.includes('job name') ? state.error : ''}
                    size="lg"
                  />
                </Tile>

                {/* Output Format (only for digitization) */}
                {state.operation === 'digitization' && (
                  <Tile className={styles.formTile}>
                    <div className={styles.tileHeader}>
                      <h4>Output Format</h4>
                      <p className={styles.tileDescription}>
                        Select the desired output format for digitized content
                      </p>
                    </div>
                    <RadioButtonGroup
                      legendText=""
                      name="outputFormat"
                      valueSelected={state.outputFormat}
                      onChange={(value) => value !== undefined && dispatch({ type: 'SET_OUTPUT_FORMAT', payload: String(value) })}
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
                        id="format-md"
                      />
                      <RadioButton
                        labelText="Text"
                        value="text"
                        id="format-text"
                      />
                    </RadioButtonGroup>
                  </Tile>
                )}

                {/* File Upload */}
                <Tile className={styles.formTile}>
                  <div className={styles.tileHeader}>
                    <h4>Step 3: Upload Files</h4>
                    <p className={styles.tileDescription}>
                      {state.operation === 'ingestion'
                        ? 'Upload one or more PDF files (max 500MB each)'
                        : 'Upload a single PDF file (max 500MB)'}
                    </p>
                  </div>
                  <FileUploader
                    key={state.fileUploaderKey}
                    labelTitle=""
                    labelDescription="Drag and drop files here or click to browse"
                    buttonLabel="Select files"
                    filenameStatus="edit"
                    accept={['.pdf']}
                    multiple={state.operation === 'ingestion'}
                    onChange={handleFileChange}
                    size="lg"
                    className={styles.fileUploader}
                  />
                </Tile>

                {/* Selected Files Display */}
                {state.files.length > 0 && (
                  <Tile className={styles.fileListTile}>
                    <div
                      className={styles.fileListHeader}
                      onClick={() => dispatch({ type: 'TOGGLE_FILE_LIST_EXPANDED' })}
                      role="button"
                      tabIndex={0}
                    >
                      <DocumentPdf size={24} />
                      <h4>Selected Files ({state.files.length})</h4>
                      <span className={`${styles.expandIcon} ${state.isFileListExpanded ? styles.expanded : ''}`}>
                        ▼
                      </span>
                    </div>
                    {state.isFileListExpanded && (
                      <ul className={styles.fileList}>
                        {state.files.map((file, index) => (
                          <li key={index} className={styles.fileItem}>
                            <DocumentPdf size={20} className={styles.fileIcon} />
                            <span className={styles.fileName}>{file.name}</span>
                            <span className={styles.fileSize}>
                              ({(file.size / 1024 / 1024).toFixed(2)} MB)
                            </span>
                            <button
                              className={styles.removeButton}
                              onClick={(e) => {
                                e.stopPropagation();
                                const newFiles = state.files.filter((_, i) => i !== index);
                                dispatch({ type: 'SET_FILES', payload: newFiles });
                                if (newFiles.length === 0) {
                                  dispatch({ type: 'SET_CURRENT_STEP', payload: 1 });
                                  dispatch({ type: 'INCREMENT_FILE_UPLOADER_KEY' });
                                }
                              }}
                              aria-label="Remove file"
                              title="Remove file"
                            >
                              <Close size={16} />
                            </button>
                          </li>
                        ))}
                      </ul>
                    )}
                  </Tile>
                )}

                {/* Notifications */}
                {state.error && (
                  <InlineNotification
                    kind="error"
                    title="Upload Error"
                    subtitle={state.error}
                    onCloseButtonClick={() => dispatch({ type: 'SET_ERROR', payload: null })}
                    className={styles.notification}
                    lowContrast
                  />
                )}

                {state.success && !state.isCompleted && (
                  <InlineNotification
                    kind="success"
                    title="Upload Successful"
                    subtitle={state.success}
                    onCloseButtonClick={() => dispatch({ type: 'SET_SUCCESS', payload: null })}
                    className={styles.notification}
                    lowContrast
                  />
                )}

                {/* Action Buttons */}
                <div className={styles.actionButtons}>
                  <Button
                    kind="primary"
                    renderIcon={Upload}
                    onClick={handleUpload}
                    disabled={state.loading || state.files.length === 0 || !state.jobName.trim()}
                    size="lg"
                  >
                    {state.loading ? 'Processing...' : 'Upload and Process'}
                  </Button>
                  {state.files.length > 0 && !state.loading && (
                    <Button
                      kind="secondary"
                      onClick={() => dispatch({ type: 'CLEAR_FILES' })}
                      size="lg"
                    >
                      Clear Selection
                    </Button>
                  )}
                </div>

                {state.loading && (
                  <div className={styles.loadingContainer}>
                    <Loading description="Uploading and processing documents..." withOverlay={false} />
                  </div>
                )}
              </>
            )}
          </div>
        </Column>

        {/* Right Column - Info Panel */}
        <Column lg={6} md={2} sm={4}>
          <div className={styles.infoPanel}>
            <Accordion>
              <AccordionItem title="About Document Processing" open={false}>
                <div className={styles.accordionContent}>
                  <div className={styles.infoSection}>
                    <h5>Ingestion</h5>
                    <p>
                      Processes documents using advanced AI to extract text, tables, and structure.
                      Stores embeddings in a vector database for semantic search and RAG applications.
                    </p>
                  </div>
                  <div className={styles.infoSection}>
                    <h5>Digitization</h5>
                    <p>
                      Converts PDF documents into structured formats (JSON, Markdown, or Text)
                      while preserving document structure, tables, and formatting.
                    </p>
                  </div>
                </div>
              </AccordionItem>

              <AccordionItem title="Supported Features" open={false}>
                <div className={styles.accordionContent}>
                  <ul className={styles.featureList}>
                    <li>✓ Text extraction with layout preservation</li>
                    <li>✓ Table detection and extraction</li>
                    <li>✓ Multi-column document support</li>
                    <li>✓ Header and footer detection</li>
                    <li>✓ Image and figure recognition</li>
                    <li>✓ Batch processing support</li>
                  </ul>
                </div>
              </AccordionItem>

              <AccordionItem title="File Requirements" open={false}>
                <div className={styles.accordionContent}>
                  <ul className={styles.requirementsList}>
                    <li><strong>Format:</strong> PDF only</li>
                    <li><strong>Max Size:</strong> 500MB per file</li>
                    <li><strong>Ingestion:</strong> Multiple files allowed</li>
                    <li><strong>Digitization:</strong> Single file only</li>
                  </ul>
                </div>
              </AccordionItem>
            </Accordion>
          </div>
        </Column>
      </Grid>
    </Theme>
  );
};

export default DocumentUploadPage;

// Made with Bob
