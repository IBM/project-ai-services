// Architecture Types - Used for listing available architectures
export interface ArchitectureSummary {
  id: string;
  name: string;
  description: string;
  certified_by: string;
  services: string[];
}

// Architecture Details Types - Used for fetching full architecture information
export interface AboutSectionValue {
  title?: string;
  value?: string;
}

export interface AboutSectionItem {
  title?: string;
  value?: string; // Single value for resource allocation items (e.g., "15 - 20")
  values?: string[]; // Multiple values for use case domain items (e.g., ["Assistant 1", "Assistant 2"])
  url?: string;
  ctaLabel?: string;
  description?: string;
  image?: {
    source: string;
  };
}

export interface AboutSection {
  title: string;
  values?: (string | AboutSectionValue)[];
  sections?: AboutSectionItem[];
}

export interface ArchitectureDetailsResponse {
  id: string;
  name: string;
  description: string;
  version: string;
  type: string;
  certified_by: string;
  runtimes: string[];
  global_components: Array<{ type: string }>;
  services: Array<{
    id: string;
    version: string;
    optional?: boolean;
  }>;
  about: AboutSection[];
}

// Service Summary Types - Used for listing available services
export interface ServiceSummary {
  id: string;
  name: string;
  description: string;
  certified_by: string;
  architectures: string[];
}

// Deploy Options Types - Used for fetching deployment configuration
export interface Provider {
  id: string;
  name: string;
  description: string;
  version: string;
  default?: boolean;
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
  resources?: Resources;
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
  id: string;
  type: string;
  provider: string;
  metadata?: {
    model?: string;
    [key: string]: unknown;
  };
}

export interface ApplicationService {
  id: string;
  type: string;
  version: string;
  status?: string;
  message?: string;
  created_at: string;
  updated_at: string;
  components: ServiceComponent[];
  endpoints: Array<{
    type: string;
    url: string;
  }>;
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
    total_cpu: number;
    available_cpu: number;
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

// DeploymentDetails Types - Used for displaying application deployment details
export interface ResourceAllocation {
  name: string;
  used: number;
  allocated: number;
  unit: string;
}

export interface AcceleratorCards {
  id: string;
  label: string;
}

export interface DeploymentDetails {
  id: string;
  name: string;
  status: string;
  type: string;
  resources: ResourceAllocation[];
  acceleratorCards?: AcceleratorCards[];
}

export interface DeploymentServiceData {
  id: string;
  title: string;
  description: string;
  serviceVersion: string;
  largeLanguageModel?: string;
  inferenceBackend: string;
  embeddingModel?: string;
  vectorStore?: string;
  rankerModel?: string;
}

export interface DeployIntegrationEndpoints {
  id: string;
  title: string;
  description: string;
  baseURL: string;
  apiDocumentaion: string;
  interactiveAPIs: string[];
}

export interface ResourcesApiResponse {
  cpu: {
    used_cpu: number;
    total_cpu: number;
  };
  memory: {
    used_bytes: number;
    total_bytes: number;
  };
  accelerators: Record<string, { used: number; total: number }>;
}

export interface ApplicationDetailsApiResponse {
  id: string;
  name: string;
  type: string;
  status: string;
  services: Array<{
    id: string;
    type: string;
    catalog_id: string;
    version: string;
    components: Array<{
      type: string;
      provider: {
        id: string;
        name: string;
      };
      metadata?: { model?: string };
    }>;
    endpoints: Array<{
      type: string;
      url: string;
    }>;
  }>;
}
