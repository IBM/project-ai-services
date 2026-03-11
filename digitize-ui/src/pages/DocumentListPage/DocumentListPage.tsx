import { useReducer, useEffect } from 'react';
import { NoDataEmptyState } from '@carbon/ibm-products';
import {
  DataTable,
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
  TableToolbarMenu,
  TableToolbarAction,
  TableBatchActions,
  TableBatchAction,
  TableSelectAll,
  TableSelectRow,
  Pagination,
  Button,
  Tag,
  Modal,
  Theme,
  Link,
  Loading,
} from '@carbon/react';
import { Renew, TrashCan, View, Download, CheckmarkFilled, ErrorFilled, InProgress } from '@carbon/icons-react';
import { useTheme } from '../../contexts/ThemeContext';
import { listDocuments, getDocumentContent, deleteDocument, Document } from '../../services/api';
import styles from './DocumentListPage.module.scss';

interface DocumentContentData {
  result: any;
  output_format: string;
}

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
  | { type: 'CLOSE_DELETE_MODAL' };

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
      };
    case 'CLOSE_DELETE_MODAL':
      return {
        ...state,
        showDeleteModal: false,
        docToDelete: null,
      };
    default:
      return state;
  }
};

const headers = [
  { key: 'name', header: 'Document name' },
  { key: 'status', header: 'Status' },
  { key: 'created_at', header: 'Created' },
  { key: 'actions', header: '' },
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
    // Backend supports: json, md, text
    switch (outputFormat.toLowerCase()) {
      case 'json':
        return { extension: 'json', mimeType: 'application/json' };
      case 'md':
        return { extension: 'md', mimeType: 'text/markdown' };
      case 'text':
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

  const handleDeleteClick = (docId: string) => {
    dispatch({ type: 'OPEN_DELETE_MODAL', payload: docId });
  };

  const handleDeleteConfirm = async () => {
    if (!state.docToDelete) return;
    try {
      await deleteDocument(state.docToDelete);
      dispatch({ type: 'CLOSE_DELETE_MODAL' });
      fetchDocuments();
    } catch (error) {
      console.error('Error deleting document:', error);
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
    actions: (
      <Button
        kind="ghost"
        size="sm"
        onClick={() => handleViewContent(doc)}
      >
        View content
      </Button>
    ),
  }));

  const noDocuments = state.documents.length === 0 && !state.search;
  const noSearchResults = state.documents.length === 0 && state.search;

  const handleDeleteJobs = async (selectedRows: any[]) => {
    try {
      const docIds = selectedRows.map(row => row.id);
      
      for (const docId of docIds) {
        await deleteDocument(docId);
      }
      
      fetchDocuments();
    } catch (error) {
      console.error('Error deleting documents:', error);
    }
  };

  return (
    <Theme theme={effectiveTheme}>
      <div className={styles.documentListPage}>
        {/* Page Header */}
        <div className={styles.pageHeader}>
          <div className={styles.headerContent}>
            <h1 className={styles.pageTitle}>Documents</h1>
            <Link href="#" className={styles.learnMore}>
              Learn more →
            </Link>
          </div>
        </div>

        {/* Data Table with Enhanced Toolbar */}
        <div className={styles.tableWrapper}>
          <DataTable rows={rows} headers={headers} size="lg">
            {({
              rows,
              headers,
              getHeaderProps,
              getRowProps,
              getTableProps,
              getSelectionProps,
              getToolbarProps,
              getBatchActionProps,
              selectedRows,
              getTableContainerProps,
            }) => {
              const batchActionProps = getBatchActionProps();
              
              return (
                <TableContainer
                  {...getTableContainerProps()}
                  className={styles.tableContainer}
                >
                  <TableToolbar {...getToolbarProps()}>
                    <TableBatchActions {...batchActionProps}>
                      <TableBatchAction
                        tabIndex={batchActionProps.shouldShowBatchActions ? 0 : -1}
                        renderIcon={TrashCan}
                        onClick={() => handleDeleteJobs(selectedRows)}
                      >
                        Delete
                      </TableBatchAction>
                    </TableBatchActions>
                    <TableToolbarContent>
                      <TableToolbarSearch
                        persistent
                        placeholder="Search"
                        onChange={(e: any, value?: string) => dispatch({ type: 'SET_SEARCH', payload: value || '' })}
                        value={state.search}
                      />
                      <Button
                        kind="ghost"
                        hasIconOnly
                        renderIcon={Download}
                        iconDescription="Download"
                        tooltipPosition="bottom"
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
                        <TableSelectAll {...getSelectionProps()} />
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
                          <TableCell colSpan={headers.length + 1} className={styles.emptyStateCell}>
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
                              <TableSelectRow {...getSelectionProps({ row })} />
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
        onRequestClose={() => dispatch({ type: 'CLOSE_DELETE_MODAL' })}
        modalHeading="Delete Document"
        primaryButtonText="Delete"
        secondaryButtonText="Cancel"
        onRequestSubmit={handleDeleteConfirm}
        onSecondarySubmit={() => dispatch({ type: 'CLOSE_DELETE_MODAL' })}
      >
        <p>Are you sure you want to delete this document? This action cannot be undone.</p>
      </Modal>
    </div>
    </Theme>
  );
};

export default DocumentListPage;

// Made with Bob
