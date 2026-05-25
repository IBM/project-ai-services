import axios, { AxiosResponse } from 'axios';

// Use relative URL to leverage Vite's proxy configuration
// This avoids CORS issues by routing through the dev server
const API_BASE_URL = '/v1';

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Types
export interface Document {
  id: string;
  name: string;
  filename?: string;
  status: string;
  submitted_at?: string;
  output_format?: string;
}

export interface JobStats {
  total_documents: number;
  completed: number;
  failed: number;
  in_progress: number;
}

export interface Job {
  job_id: string;
  job_name?: string;
  operation: string;
  status: string;
  submitted_at?: string;
  completed_at?: string;
  error?: string | null;
  documents?: Document[];
  stats?: JobStats;
}

export interface PaginationInfo {
  total: number;
  limit: number;
  offset: number;
}

export interface JobsResponse {
  data: Job[];
  pagination?: PaginationInfo;
}

export interface DocumentsResponse {
  data: Document[];
  pagination?: PaginationInfo;
}

export interface UploadResponse {
  job_id: string;
  message?: string;
}

export interface GetJobsParams {
  latest?: boolean;
  limit?: number;
  offset?: number;
  status?: string | null;
  operation?: string | null;
}

export interface ListDocumentsParams {
  limit?: number;
  offset?: number;
  status?: string | null;
  name?: string | null;
}

// Document Upload and Processing
export const uploadDocuments = async (
  files: File[],
  operation: string = 'ingestion',
  outputFormat: string = 'json',
  jobName?: string
): Promise<UploadResponse> => {
  const formData = new FormData();
  files.forEach(file => {
    formData.append('files', file);
  });

  let url = `/jobs?operation=${operation}`;
  
  // Only include output_format for digitization operations
  if (operation === 'digitization') {
    url += `&output_format=${outputFormat}`;
  }
  
  if (jobName) {
    url += `&job_name=${encodeURIComponent(jobName)}`;
  }

  const response: AxiosResponse<UploadResponse> = await api.post(
    url,
    formData,
    {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    }
  );
  return response.data;
};

// Job Management
export const getAllJobs = async (params: GetJobsParams = {}): Promise<JobsResponse> => {
  const { latest = false, limit = 20, offset = 0, status = null, operation = null } = params;
  const queryParams = new URLSearchParams({
    latest: latest.toString(),
    limit: limit.toString(),
    offset: offset.toString(),
  });
  
  if (status) {
    queryParams.append('status', status);
  }
  if (operation) {
    queryParams.append('operation', operation);
  }

  const response: AxiosResponse<JobsResponse> = await api.get(`/jobs?${queryParams}`);
  return response.data;
};

export const getJobById = async (jobId: string): Promise<Job> => {
  const response: AxiosResponse<Job> = await api.get(`/jobs/${jobId}`);
  return response.data;
};

// Document Management
export const listDocuments = async (params: ListDocumentsParams = {}): Promise<DocumentsResponse> => {
  const { limit = 20, offset = 0, status = null, name = null } = params;
  const queryParams = new URLSearchParams({
    limit: limit.toString(),
    offset: offset.toString(),
  });
  
  if (status) {
    queryParams.append('status', status);
  }
  if (name) {
    queryParams.append('name', name);
  }

  const response: AxiosResponse<DocumentsResponse> = await api.get(`/documents?${queryParams}`);
  return response.data;
};

export const getDocumentMetadata = async (docId: string, details: boolean = false): Promise<Document> => {
  const response: AxiosResponse<Document> = await api.get(`/documents/${docId}?details=${details}`);
  return response.data;
};

export const getDocumentContent = async (docId: string): Promise<any> => {
  const response: AxiosResponse<any> = await api.get(`/documents/${docId}/content`);
  return response.data;
};

export const deleteDocument = async (docId: string): Promise<{ message: string }> => {
  const response: AxiosResponse<{ message: string }> = await api.delete(`/documents/${docId}`);
  return response.data;
};

export const bulkDeleteDocuments = async (): Promise<{ message: string }> => {
  const response: AxiosResponse<{ message: string }> = await api.delete('/documents?confirm=true');
  return response.data;
};

export const deleteJob = async (jobId: string): Promise<{ message: string }> => {
  const response: AxiosResponse<{ message: string }> = await api.delete(`/jobs/${jobId}`);
  return response.data;
};

export const bulkDeleteJobs = async (jobIds: string[]): Promise<{ message: string }> => {
  const response: AxiosResponse<{ message: string }> = await api.post('/jobs/bulk-delete', {
    job_ids: jobIds,
  });
  return response.data;
};

export default api;

// Made with Bob