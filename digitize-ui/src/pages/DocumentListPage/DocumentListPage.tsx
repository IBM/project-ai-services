import { useReducer, useEffect } from 'react';
import { NoDataEmptyState } from '@carbon/ibm-products';
import {
  DataTable,
  DataTableSkeleton,
  Table,
  TableHead,
  TableRow,
  TableHeader,
  TableBody,
  TableCell,
  TableContainer,
  TableToolbar,
  TableToolbarContent,
  TableToolbarSearch,
  Pagination,
  Button,
  Modal,
  Theme,
  Link,
  Loading,
  Checkbox,
  CheckboxGroup,
  ActionableNotification,
  TextInput,
  InlineLoading,
} from '@carbon/react';
import { Renew, TrashCan, Download, CheckmarkFilled, ErrorFilled, InProgress } from '@carbon/icons-react';
import { useTheme } from '../../contexts/useTheme';
import { listDocuments, getDocumentContent, deleteDocument, Document } from '../../services/api';
import { exportToCSV, validateFilename } from '../../utils/csvExport';
import styles from './DocumentListPage.module.scss';

interface DocumentContentData {
  result: any;
  output_format: string;
}

export type ExportStatus = 'idle' | 'exporting' | 'success' | 'error';

interface DocumentListState {
  documents: Document[];
  loading: boolean;
  page: number;
  pageSize: number;
  totalItems: number;
  search: string;
  selectedDoc: Document | null;
  showContentModal: boolean;
  docContent: DocumentContentData | null;
  loadingContent: boolean;
  showDeleteModal: boolean;
  docToDelete: string | null;
  isConfirmed: boolean;
  toastOpen: boolean;
  errorMessage: string;
  errorDocName: string;
  isDeleting: boolean;
  isExportDialogOpen: boolean;
  csvFileName: string;
  exportStatus: ExportStatus;
  exportErrorMessage: string;
}

type DocumentListAction =
  | { type: 'SET_DOCUMENTS'; payload: { documents: Document[]; totalItems: number } }
  | { type: 'SET_LOADING'; payload: boolean }
  | { type: 'SET_PAGE'; payload: number }
  | { type: 'SET_PAGE_SIZE'; payload: number }
  | { type: 'SET_SEARCH'; payload: string }
  | { type: 'SET_SELECTED_DOC'; payload: Document | null }
  | { type: 'SET_SHOW_CONTENT_MODAL'; payload: boolean }
  | { type: 'SET_DOC_CONTENT'; payload: DocumentContentData | null }
  | { type: 'SET_LOADING_CONTENT'; payload: boolean }
  | { type: 'SET_SHOW_DELETE_MODAL'; payload: boolean }
  | { type: 'SET_DOC_TO_DELETE'; payload: string | null }
  | { type: 'OPEN_CONTENT_MODAL'; payload: { doc: Document; content: DocumentContentData } }
  | { type: 'CLOSE_CONTENT_MODAL' }
  | { type: 'OPEN_DELETE_MODAL'; payload: string }
  | { type: 'CLOSE_DELETE_MODAL' }
  | { type: 'SET_CONFIRMED'; payload: boolean }
  | { type: 'SHOW_ERROR'; payload: { message: string; docName?: string } }
  | { type: 'HIDE_ERROR' }
  | { type: 'SET_IS_DELETING'; payload: boolean }
  | { type: 'DELETE_DOCUMENT'; payload: string }
  | { type: 'OPEN_EXPORT_DIALOG' }
  | { type: 'CLOSE_EXPORT_DIALOG' }
  | { type: 'SET_CSV_FILENAME'; payload: string }
  | { type: 'SET_EXPORT_STATUS'; payload: ExportStatus }
  | { type: 'SET_EXPORT_ERROR'; payload: string }
  | { type: 'CLEAR_EXPORT_ERROR' };

const initialState: DocumentListState = {
  documents: [],
  loading: false,
  page: 1,
  pageSize: 10,
  totalItems: 0,
  search: '',
  selectedDoc: null,
  showContentModal: false,
  docContent: null,
  loadingContent: false,
  showDeleteModal: false,
  docToDelete: null,
  isConfirmed: false,
  toastOpen: false,
  errorMessage: '',
  errorDocName: '',
  isDeleting: false,
  isExportDialogOpen: false,
  csvFileName: '',
  exportStatus: 'idle',
  exportErrorMessage: '',
};

const documentListReducer = (
  state: DocumentListState,
  action: DocumentListAction
): DocumentListState => {
  switch (action.type) {
    case 'SET_DOCUMENTS':
      return {
        ...state,
        documents: action.payload.documents,
        totalItems: action.payload.totalItems,
      };
    case 'SET_LOADING':
      return {
        ...state,
        loading: action.payload,
      };
    case 'SET_PAGE':
      return {
        ...state,
        page: action.payload,
      };
    case 'SET_PAGE_SIZE':
      return {
        ...state,
        pageSize: action.payload,
      };
    case 'SET_SEARCH':
      return {
        ...state,
        search: action.payload,
      };
    case 'SET_SELECTED_DOC':
      return {
        ...state,
        selectedDoc: action.payload,
      };
    case 'SET_SHOW_CONTENT_MODAL':
      return {
        ...state,
        showContentModal: action.payload,
      };
    case 'SET_DOC_CONTENT':
      return {
        ...state,
        docContent: action.payload,
      };
    case 'SET_LOADING_CONTENT':
      return {
        ...state,
        loadingContent: action.payload,
      };
    case 'SET_SHOW_DELETE_MODAL':
      return {
        ...state,
        showDeleteModal: action.payload,
      };
    case 'SET_DOC_TO_DELETE':
      return {
        ...state,
        docToDelete: action.payload,
      };
    case 'OPEN_CONTENT_MODAL':
      return {
        ...state,
        docContent: action.payload.content,
        selectedDoc: action.payload.doc,
        showContentModal: true,
        loadingContent: false,
      };
    case 'CLOSE_CONTENT_MODAL':
      return {
        ...state,
        showContentModal: false,
        docContent: null,
        selectedDoc: null,
      };
    case 'OPEN_DELETE_MODAL':
      return {
        ...state,
        docToDelete: action.payload,
        showDeleteModal: true,
        toastOpen: false,
      };
    case 'CLOSE_DELETE_MODAL':
      return {
        ...state,
        showDeleteModal: false,
        isConfirmed: false,
        docToDelete: null,
      };
    case 'SET_CONFIRMED':
      return { ...state, isConfirmed: action.payload };
    case 'DELETE_DOCUMENT':
      return {
        ...state,
        documents: state.documents.filter((d) => d.id !== action.payload),
        showDeleteModal: false,
        isConfirmed: false,
      };
    case 'SHOW_ERROR':
      return {
        ...state,
        errorMessage: action.payload.message,
        errorDocName: action.payload.docName ?? '',
        toastOpen: true,
        isDeleting: false,
      };
    case 'HIDE_ERROR':
      return {
        ...state,
        toastOpen: false,
        docToDelete: null,
        errorDocName: '',
      };
    case 'SET_IS_DELETING':
      return { ...state, isDeleting: action.payload };
    case 'OPEN_EXPORT_DIALOG':
      return {
        ...state,
        isExportDialogOpen: true,
        csvFileName: '',
        exportErrorMessage: '',
        exportStatus: 'idle',
      };
    case 'CLOSE_EXPORT_DIALOG':
      return {
        ...state,
        isExportDialogOpen: false,
      };
    case 'SET_CSV_FILENAME':
      return { ...state, csvFileName: action.payload };
    case 'SET_EXPORT_STATUS':
      return { ...state, exportStatus: action.payload };
    case 'SET_EXPORT_ERROR':
      return {
        ...state,
        exportErrorMessage: action.payload,
      };
    case 'CLEAR_EXPORT_ERROR':
      return { ...state, exportErrorMessage: '' };
    default:
      return state;
  }
};

const headers = [
  { key: 'name', header: 'Document name' },
  { key: 'status', header: 'Status' },
  { key: 'created_at', header: 'Created' },
  { key: 'view_action', header: '' },
  { key: 'delete_action', header: '' },
];

const getStatusIcon = (status: string) => {
  switch (status) {
    case 'completed':
      return <CheckmarkFilled size={16} className={styles.statusIconSuccess} />;
    case 'failed':
      return <ErrorFilled size={16} className={styles.statusIconError} />;
    case 'processing':
      return <InProgress size={16} className={styles.statusIconProgress} />;
    default:
      return null;
  }
};

const DocumentListPage = () => {
  const { effectiveTheme } = useTheme();
  const [state, dispatch] = useReducer(documentListReducer, initialState);

  const fetchDocuments = async () => {
    dispatch({ type: 'SET_LOADING', payload: true });
    try {
      const offset = (state.page - 1) * state.pageSize;
      const response = await listDocuments({
        limit: state.pageSize,
        offset: offset,
        name: state.search || null,
      });
      
      dispatch({
        type: 'SET_DOCUMENTS',
        payload: {
          documents: response.data || [],
          totalItems: response.pagination?.total || 0,
        },
      });
    } catch (error) {
      console.error('Error fetching documents:', error);
    } finally {
      dispatch({ type: 'SET_LOADING', payload: false });
    }
  };

  useEffect(() => {
    fetchDocuments();
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.page, state.pageSize, state.search]);

  const handleViewContent = async (doc: Document) => {
    dispatch({ type: 'SET_LOADING_CONTENT', payload: true });
    dispatch({ type: 'SET_SHOW_CONTENT_MODAL', payload: true });
    dispatch({ type: 'SET_SELECTED_DOC', payload: doc });
    
    try {
      const content = await getDocumentContent(doc.id);
      dispatch({
        type: 'OPEN_CONTENT_MODAL',
        payload: { doc, content },
      });
    } catch (error) {
      console.error('Error fetching document content:', error);
      dispatch({ type: 'SET_LOADING_CONTENT', payload: false });
    }
  };

  const getFileExtensionAndMimeType = (outputFormat: string) => {
    // Backend supports: json, md, txt
    switch (outputFormat.toLowerCase()) {
      case 'json':
        return { extension: 'json', mimeType: 'application/json' };
      case 'md':
        return { extension: 'md', mimeType: 'text/markdown' };
      case 'txt':
        return { extension: 'txt', mimeType: 'text/plain' };
      default:
        return { extension: 'json', mimeType: 'application/json' };
    }
  };

  const handleDownloadContent = () => {
    if (!state.docContent || !state.selectedDoc) return;

    try {
      const outputFormat = state.docContent.output_format || 'json';
      const { extension, mimeType } = getFileExtensionAndMimeType(outputFormat);
      
      // Convert content to appropriate string format
      let contentStr: string;
      const contentResult = state.docContent.result;
      
      if (typeof contentResult === 'string') {
        // For md and text formats, content is already a string
        contentStr = contentResult;
      } else {
        // For JSON format, stringify with formatting
        contentStr = JSON.stringify(contentResult, null, 2);
      }
      
      // Create a Blob from the content
      const blob = new Blob([contentStr], { type: mimeType });
      
      // Create a download link
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      
      // Generate filename from document name
      const docName = state.selectedDoc.name || state.selectedDoc.filename || 'document';
      const baseFilename = docName.replace(/\.[^/.]+$/, '');
      link.download = `${baseFilename}_content.${extension}`;
      
      // Trigger download
      document.body.appendChild(link);
      link.click();
      
      // Cleanup
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
    } catch (error) {
      console.error('Error downloading content:', error);
    }
  };

  const renderContentPreview = () => {
    if (state.loadingContent) {
      return (
        <div className={styles.loadingContainer}>
          <Loading description="Loading content..." withOverlay={false} />
        </div>
      );
    }

    if (!state.docContent) {
      return <p>No content available</p>;
    }

    // Format the content for better readability based on type
    try {
      const contentResult = state.docContent.result;
      let displayContent: string;
      
      if (typeof contentResult === 'string') {
        // For md and text formats, display as-is
        displayContent = contentResult;
      } else {
        // For JSON format, stringify with formatting
        displayContent = JSON.stringify(contentResult, null, 2);
      }
      
      return (
        <div className={styles.contentPreview}>
          <pre className={styles.contentPre}>{displayContent}</pre>
        </div>
      );
    } catch (error) {
      return <p>Error displaying content</p>;
    }
  };

  const handleDeleteConfirm = async () => {
    if (!state.docToDelete) return;

    dispatch({ type: 'SET_IS_DELETING', payload: true });

    try {
      await deleteDocument(state.docToDelete);
      dispatch({ type: 'DELETE_DOCUMENT', payload: state.docToDelete });
      fetchDocuments();
    } catch (error: any) {
      const msg = error.response?.data?.detail || error.message || 'Failed deleting document';
      const name = state.documents.find((d) => d.id === state.docToDelete)?.name || '';
      dispatch({
        type: 'SHOW_ERROR',
        payload: { message: msg, docName: name },
      });
    } finally {
      dispatch({ type: 'SET_IS_DELETING', payload: false });
      dispatch({ type: 'CLOSE_DELETE_MODAL' });
    }
  };

  const handleExportCSV = async () => {
    const filename = state.csvFileName.trim();
    const validationError = validateFilename(filename);

    if (validationError) {
      dispatch({
        type: 'SET_EXPORT_ERROR',
        payload: validationError,
      });
      return;
    }

    if (state.documents.length === 0) {
      dispatch({
        type: 'SET_EXPORT_ERROR',
        payload: 'No data available to export',
      });
      return;
    }

    dispatch({
      type: 'SET_EXPORT_STATUS',
      payload: 'exporting',
    });

    try {
      // Create rows for export (excluding action columns)
      const exportRows = state.documents.map((doc) => ({
        name: doc.name || doc.filename || 'N/A',
        status: doc.status,
        created_at: doc.created_at
          ? new Date(doc.created_at).toLocaleString('en-US', {
              month: 'short',
              day: 'numeric',
              year: 'numeric',
              hour: 'numeric',
              minute: '2-digit',
              hour12: true,
            })
          : 'N/A',
      }));

      exportToCSV({
        filename,
        headers,
        rows: exportRows,
        excludeColumns: ['view_action', 'delete_action'],
      });

      dispatch({
        type: 'SET_EXPORT_STATUS',
        payload: 'success',
      });
    } catch (error: any) {
      dispatch({
        type: 'SET_EXPORT_STATUS',
        payload: 'error',
      });

      dispatch({
        type: 'SET_EXPORT_ERROR',
        payload: error.message || 'An error occurred while exporting the CSV file. Please try again.',
      });
    }
  };

  const rows = state.documents.map((doc) => ({
    id: doc.id,
    name: doc.name || doc.filename || 'N/A',
    status: (
      <div className={styles.statusCell}>
        {getStatusIcon(doc.status)}
        <span className={styles.statusText}>{doc.status}</span>
      </div>
    ),
    created_at: doc.created_at
      ? new Date(doc.created_at).toLocaleString('en-US', {
          month: 'short',
          day: 'numeric',
          year: 'numeric',
          hour: 'numeric',
          minute: '2-digit',
          hour12: true,
        })
      : 'N/A',
    view_action: (
      <Button
        kind="ghost"
        size="sm"
        onClick={() => handleViewContent(doc)}
      >
        View content
      </Button>
    ),
    delete_action: (
      <Button
        hasIconOnly
        kind="ghost"
        size="sm"
        renderIcon={TrashCan}
        iconDescription="Delete"
        onClick={() => dispatch({ type: 'OPEN_DELETE_MODAL', payload: doc.id })}
      />
    ),
  }));

  const noSearchResults = state.documents.length === 0 && state.search;

  return (
    <Theme theme={effectiveTheme}>
      <div className={styles.documentListPage}>
        {state.toastOpen && (
          <ActionableNotification
            actionButtonLabel="Try again"
            aria-label="close notification"
            kind="error"
            closeOnEscape
            title={`Delete document ${state.errorDocName} failed`}
            subtitle={state.errorMessage}
            onCloseButtonClick={() => {
              dispatch({ type: 'HIDE_ERROR' });
            }}
            style={{
              position: 'fixed',
              top: '4rem',
              right: '2rem',
              zIndex: 9999,
            }}
          />
        )}
        {/* Page Header */}
        <div className={styles.pageHeader}>
          <div className={styles.headerContent}>
            <h1 className={styles.pageTitle}>Documents</h1>
            <Link href="https://www.ibm.com/docs/en/aiservices/2025.12.0?topic=services-introduction" className={styles.learnMore} target="_blank" rel="noopener noreferrer">
              Learn more →
            </Link>
          </div>
        </div>

        {/* Data Table with Enhanced Toolbar */}
        <div className={styles.tableWrapper}>
          {state.loading && state.documents.length === 0 ? (
            <DataTableSkeleton
              headers={headers}
              aria-label="Loading documents"
              showHeader={false}
              showToolbar={false}
            />
          ) : (
            <DataTable rows={rows} headers={headers} size="lg">
              {({
                rows,
                headers,
                getHeaderProps,
                getRowProps,
                getTableProps,
                getTableContainerProps,
              }) => {
                return (
                  <TableContainer
                    {...getTableContainerProps()}
                    className={styles.tableContainer}
                  >
                    <TableToolbar>
                      <TableToolbarContent>
                        <TableToolbarSearch
                          persistent
                          placeholder="Search"
                          onChange={(_e: any, value?: string) => dispatch({ type: 'SET_SEARCH', payload: value || '' })}
                          value={state.search}
                        />
                        <Button
                          kind="ghost"
                          hasIconOnly
                          renderIcon={Download}
                          iconDescription="Download"
                          tooltipPosition="bottom"
                          onClick={() => dispatch({ type: 'OPEN_EXPORT_DIALOG' })}
                        />
                        <Button
                          kind="ghost"
                          hasIconOnly
                          renderIcon={Renew}
                          iconDescription="Refresh"
                          onClick={fetchDocuments}
                          disabled={state.loading}
                          tooltipPosition="bottom"
                        />
                      </TableToolbarContent>
                    </TableToolbar>
                    <Table {...getTableProps()} className={styles.table}>
                      <TableHead>
                        <TableRow>
                          {headers.map((header) => {
                            const { key, ...rest } = getHeaderProps({ header });
                            return (
                              <TableHeader key={key} {...rest}>
                                {header.header}
                              </TableHeader>
                            );
                          })}
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {rows.length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={headers.length} className={styles.emptyStateCell}>
                              <NoDataEmptyState
                                illustrationTheme="light"
                                size="lg"
                                title={noSearchResults ? "No data" : "No documents found"}
                                subtitle={noSearchResults ? "Try adjusting your search." : "Start ingesting the document to get started"}
                              />
                            </TableCell>
                          </TableRow>
                        ) : (
                          rows.map((row) => {
                            const { key: rowKey, ...rowProps } = getRowProps({ row });
                            return (
                              <TableRow key={rowKey} {...rowProps}>
                                {row.cells.map((cell) => (
                                  <TableCell key={cell.id}>{cell.value}</TableCell>
                                ))}
                              </TableRow>
                            );
                          })
                        )}
                      </TableBody>
                    </Table>
                    {rows.length > 0 && (
                      <Pagination
                        page={state.page}
                        pageSize={state.pageSize}
                        pageSizes={[10, 25, 50, 100]}
                        totalItems={state.totalItems}
                        onChange={({ page, pageSize }) => {
                          dispatch({ type: 'SET_PAGE', payload: page });
                          dispatch({ type: 'SET_PAGE_SIZE', payload: pageSize });
                        }}
                        itemsPerPageText="Items per page:"
                      />
                    )}
                  </TableContainer>
                );
              }}
            </DataTable>
          )}
        </div>

      {/* Content Modal */}
      <Modal
        open={state.showContentModal}
        onRequestClose={() => dispatch({ type: 'CLOSE_CONTENT_MODAL' })}
        modalHeading={`Document Content: ${state.selectedDoc?.name || state.selectedDoc?.filename || 'Document'}`}
        primaryButtonText="Download"
        primaryButtonDisabled={state.loadingContent || !state.docContent}
        secondaryButtonText="Close"
        onRequestSubmit={handleDownloadContent}
        onSecondarySubmit={() => dispatch({ type: 'CLOSE_CONTENT_MODAL' })}
        size="lg"
      >
        <div className={styles.modalContent}>
          {renderContentPreview()}
        </div>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        open={state.showDeleteModal}
        danger
        size="sm"
        modalLabel="Delete Document"
        modalHeading="Confirm delete"
        primaryButtonText="Delete"
        secondaryButtonText="Cancel"
        primaryButtonDisabled={!state.isConfirmed}
        onRequestClose={() => dispatch({ type: 'CLOSE_DELETE_MODAL' })}
        onRequestSubmit={handleDeleteConfirm}
      >
        <p>
          Deleting a document permanently removes it from the system. This action cannot be undone.
        </p>
        <div>
          <CheckboxGroup
            legendText="Confirm document to be deleted"
          >
            <Checkbox
              id="checkbox-delete-doc"
              labelText={
                <strong>
                  {state.docToDelete
                    ? state.documents.find((d) => d.id === state.docToDelete)?.name || 'Document'
                    : ''}
                </strong>
              }
              checked={state.isConfirmed}
              onChange={(_, { checked }) =>
                dispatch({
                  type: 'SET_CONFIRMED',
                  payload: checked,
                })
              }
            />
          </CheckboxGroup>
        </div>
      </Modal>

      {/* Export CSV Modal */}
      <Modal
        open={state.isExportDialogOpen}
        size="sm"
        modalHeading="Export as CSV"
        passiveModal={state.exportStatus !== 'idle'}
        preventCloseOnClickOutside
        {...(state.exportStatus === 'idle' && {
          primaryButtonText: 'Export',
          secondaryButtonText: 'Cancel',
          onRequestSubmit: handleExportCSV,
        })}
        onRequestClose={() => dispatch({ type: 'CLOSE_EXPORT_DIALOG' })}
      >
        {state.exportStatus === 'idle' && (
          <TextInput
            id="csv-file-name"
            labelText="File name"
            value={state.csvFileName}
            invalid={!!state.exportErrorMessage}
            invalidText={state.exportErrorMessage}
            onChange={(e) => {
              dispatch({
                type: 'SET_CSV_FILENAME',
                payload: e.target.value,
              });
              dispatch({ type: 'CLEAR_EXPORT_ERROR' });
            }}
          />
        )}

        {state.exportStatus === 'exporting' && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <InlineLoading status="active" description="Exporting..." />
          </div>
        )}

        {state.exportStatus === 'success' && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <InlineLoading
              status="finished"
              description="The file has been exported"
            />
          </div>
        )}

        {state.exportStatus === 'error' && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <InlineLoading
              status="error"
              description={state.exportErrorMessage}
            />
          </div>
        )}
      </Modal>
    </div>
    </Theme>
  );
};

export default DocumentListPage;

// Made with Bob
