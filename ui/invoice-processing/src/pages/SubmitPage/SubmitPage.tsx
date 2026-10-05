import React, { useState } from 'react';
import { useNavigate } from 'react-router';
import {
  Breadcrumb,
  BreadcrumbItem,
  Button,
  Column,
  FileUploaderDropContainer,
  FileUploaderItem,
  Form,
  FormGroup,
  Grid,
  Heading,
  InlineNotification,
  Loading,
  ProgressIndicator,
  ProgressStep,
  Stack,
  Tag,
  Tile,
} from '@carbon/react';
import { Upload } from '@carbon/icons-react';
import { submitInvoice } from '../../services/api';

const ACCEPTED_EXTENSIONS = ['.pdf', '.png', '.jpg', '.jpeg', '.tiff', '.tif', '.webp', '.bmp'];
const ACCEPTED_MIME_TYPES = [
  'application/pdf',
  'image/png',
  'image/jpeg',
  'image/tiff',
  'image/webp',
  'image/bmp',
];

const PIPELINE_STEPS = [
  'Document Upload',
  'OCR Extraction',
  'Staging',
  'Human Review',
  'Database Load',
];

interface NotificationState {
  kind: 'error' | 'success' | 'info' | 'warning';
  title: string;
  subtitle?: string;
}

const SubmitPage: React.FC = () => {
  const navigate = useNavigate();
  const [file, setFile] = useState<File | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [notification, setNotification] = useState<NotificationState | null>(null);

  const handleAddFiles = (_: React.SyntheticEvent, { addedFiles }: { addedFiles: File[] }) => {
    if (addedFiles && addedFiles.length > 0) {
      const selected = addedFiles[0];
      const ext = `.${selected.name.split('.').pop()?.toLowerCase()}`;
      if (!ACCEPTED_EXTENSIONS.includes(ext) && !ACCEPTED_MIME_TYPES.includes(selected.type)) {
        setNotification({
          kind: 'error',
          title: 'Unsupported file format',
          subtitle: `Please upload a supported file: ${ACCEPTED_EXTENSIONS.join(', ')}`,
        });
        return;
      }
      setFile(selected);
      setNotification(null);
    }
  };

  const handleRemoveFile = () => {
    setFile(null);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) {
      setNotification({ kind: 'error', title: 'No file selected.' });
      return;
    }

    setSubmitting(true);
    setNotification(null);

    try {
      const res = await submitInvoice(file);
      setNotification({
        kind: 'success',
        title: 'Invoice submitted successfully.',
        subtitle: `Job ID: ${res.job_id || 'Queued'}. Redirecting to jobs...`,
      });
      setTimeout(() => {
        navigate('/jobs');
      }, 1500);
    } catch (err: unknown) {
      const errorMessage =
        (err as { response?: { data?: { detail?: string } }; message?: string })?.response?.data?.detail ||
        (err as Error)?.message ||
        'Failed to submit invoice.';
      setNotification({
        kind: 'error',
        title: 'Submission failed.',
        subtitle: errorMessage,
      });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Grid className="cds--css-grid--full-width" style={{ padding: '2rem' }}>
      <Column lg={16} md={8} sm={4}>
        <Breadcrumb style={{ marginBottom: '1rem' }}>
          <BreadcrumbItem href="#" onClick={(e) => { e.preventDefault(); navigate('/jobs'); }}>
            Pipeline Jobs
          </BreadcrumbItem>
          <BreadcrumbItem href="#" isCurrentPage>
            Submit Invoice
          </BreadcrumbItem>
        </Breadcrumb>

        <Heading style={{ fontSize: '1.75rem', fontWeight: 600, marginBottom: '0.5rem' }}>
          Invoice Processing — Entry Point
        </Heading>
        <p style={{ color: 'var(--cds-text-secondary)', marginBottom: '1.5rem' }}>
          Upload an invoice document to start the automated end-to-end processing pipeline.
        </p>

        {/* Pipeline progress indicator */}
        <Tile style={{ marginBottom: '1.5rem', padding: '1.25rem' }}>
          <ProgressIndicator currentIndex={0} spaceEqually>
            {PIPELINE_STEPS.map((label, i) => (
              <ProgressStep
                key={label}
                label={label}
                complete={false}
                current={i === 0}
              />
            ))}
          </ProgressIndicator>
        </Tile>

        {/* Form Container */}
        <Tile style={{ padding: '2rem' }}>
          <p style={{ fontSize: '0.875rem', color: 'var(--cds-text-secondary)', marginBottom: '1.5rem' }}>
            Upload an invoice document. Runs all pipeline stages end-to-end.
          </p>

          <Form onSubmit={handleSubmit}>
            <Stack gap={6}>
              {notification && (
                <InlineNotification
                  kind={notification.kind}
                  title={notification.title}
                  subtitle={notification.subtitle}
                  onCloseButtonClick={() => setNotification(null)}
                  style={{ marginBottom: '1rem', maxWidth: '100%' }}
                />
              )}

              <FormGroup legendText="Invoice Document">
                <p style={{ fontSize: '0.75rem', color: 'var(--cds-text-secondary)', marginBottom: '0.75rem' }}>
                  Accepted: PDF, PNG, JPG/JPEG, TIFF, WEBP, BMP
                </p>
                <FileUploaderDropContainer
                  labelText="Drag and drop a file here, or click to upload"
                  accept={ACCEPTED_MIME_TYPES}
                  onAddFiles={handleAddFiles}
                  multiple={false}
                  disabled={submitting}
                />
                {file && (
                  <div style={{ marginTop: '0.75rem' }}>
                    <FileUploaderItem
                      name={file.name}
                      status="edit"
                      onDelete={handleRemoveFile}
                    />
                    <Tag type="blue" size="sm" style={{ marginTop: '0.5rem' }}>
                      {(file.size / 1024).toFixed(1)} KB
                    </Tag>
                  </div>
                )}
              </FormGroup>

              <div>
                <Button
                  type="submit"
                  renderIcon={Upload}
                  disabled={submitting || !file}
                >
                  {submitting ? 'Submitting…' : 'Start Full Pipeline'}
                </Button>
              </div>

              {submitting && <Loading description="Starting pipeline…" withOverlay />}
            </Stack>
          </Form>
        </Tile>
      </Column>
    </Grid>
  );
};

export default SubmitPage;
