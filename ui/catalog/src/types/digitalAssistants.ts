// Deploy Options Types - Used for fetching deployment configuration
export interface Provider {
  id: string;
  name: string;
  description: string;
  version: string;
  schema?: string;
  resources?: {
    cpu: number;
    memory: number;
    storage?: number;
    accelerators?: Record<string, number>;
  };
}

export interface Component {
  type: string;
  name: string;
  providers: Provider[];
}

export interface Service {
  id: string;
  name: string;
  version: string;
  schema?: string;
  components: Component[];
}

export interface DeployOptionsResponse {
  id: string;
  name: string;
  version: string;
  global_components: Component[];
  services: Service[];
}

// Application Types - Used for managing deployed digital assistants
export interface ServiceComponent {
  type: string;
  provider: string;
  metadata: Record<string, unknown>;
}

export interface ApplicationService {
  id: string;
  type: string;
  version: string;
  status: string;
  created_at: string;
  updated_at: string;
  components: ServiceComponent[];
  endpoints: object[];
}

export interface Application {
  id: string;
  name: string;
  type: string;
  deployment_type: string;
  status: string;
  message: string;
  created_at: string;
  updated_at: string;
  services: ApplicationService[];
}

export interface PaginationMetadata {
  page: number;
  page_size: number;
  total_items: number;
  total_pages: number;
  has_next: boolean;
  has_prev: boolean;
}

export interface ApplicationListResponse {
  data: Application[];
  pagination: PaginationMetadata;
}

// API Request/Response Types
export interface FetchApplicationsParams {
  page?: number;
  page_size?: number;
  deployment_type?: "architectures" | "services";
  catalog_id?: string;
}

export interface DeleteApplicationResponse {
  id: string;
  message: string;
  status: string;
}

export interface DeployApplicationResponse {
  id: string;
}

// Resources API Types - Used for fetching system resource availability
export interface ResourcesResponse {
  cpu: {
    total_cores: number;
    available_cores: number;
  };
  memory: {
    total_bytes: number;
    available_bytes: number;
  };
  accelerators: {
    [key: string]: {
      total: number;
      available: number;
    };
  };
}
