import axios from 'axios';

const apiClient = axios.create({
  baseURL: '/v1',
  headers: {
    'Content-Type': 'application/json',
  },
});

export interface PaginationInfo {
  page: number;
  page_size: number;
  total_items: number;
  total_pages: number;
}

export interface InvoiceJob {
  job_id: string;
  filename: string;
  input_type: 'pdf' | 'image';
  pipeline_path: 'pdf_path' | 'oneshot' | null;
  status: string;
  submitted_at: string;
  completed_at: string | null;
  updated_at: string;
  error: string | null;
  digitize_job_id: string | null;
  extract_job_id: string | null;
  staged_header: Record<string, unknown> | null;
  staged_lines: Record<string, unknown>[] | null;
  interface_ref: string | null;
  job_metadata: Record<string, unknown> | null;
}

export interface SubmitResponse {
  job_id: string;
  input_type: string;
  status: string;
  message: string;
}

export interface InvoiceJobsResponse {
  data: InvoiceJob[];
  pagination: PaginationInfo;
}

export interface ListJobsParams {
  page?: number;
  page_size?: number;
  status?: string;
}

// Stubs — implementation details will be filled in feature PRs
export const submitInvoice = async (file: File): Promise<SubmitResponse> => {
  const formData = new FormData();
  formData.append('file', file);
  const response = await apiClient.post<SubmitResponse>('/invoices', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
  return response.data;
};

export const getJob = async (jobId: string): Promise<InvoiceJob> => {
  const response = await apiClient.get<InvoiceJob>(`/invoices/${jobId}`);
  return response.data;
};

export const listJobs = async (params?: ListJobsParams): Promise<InvoiceJobsResponse> => {
  const response = await apiClient.get<InvoiceJobsResponse>('/invoices', { params });
  return response.data;
};

export const approveJob = async (jobId: string): Promise<void> => {
  await apiClient.post(`/invoices/${jobId}/approve`);
};

export const rejectJob = async (jobId: string): Promise<void> => {
  await apiClient.post(`/invoices/${jobId}/reject`);
};

export default apiClient;
