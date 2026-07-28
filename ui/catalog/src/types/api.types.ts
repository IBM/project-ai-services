export interface ArchitectureSummary {
  id: string;
  name: string;
  description: string;
  certified_by: string;
  services: string[];
}

export interface AboutSectionValue {
  title?: string;
  value?: string;
}

export interface AboutSectionItem {
  title?: string;
  value?: string;
  values?: string[];
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

export interface ServiceSummary {
  id: string;
  name: string;
  description: string;
  certified_by: string;
  architectures: string[];
}

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

export interface DeployOptionsComponent {
  type: string;
  name: string;
  providers: Provider[];
}

export interface DeployOptionsService {
  id: string;
  name: string;
  version: string;
  schema?: string;
  components: DeployOptionsComponent[];
  resources?: {
    cpu: number;
    memory: number;
    storage?: number;
    accelerators?: Record<string, number>;
  };
}

export interface DeployOptionsResponse {
  id: string;
  name: string;
  version: string;
  global_components: DeployOptionsComponent[];
  services: DeployOptionsService[];
}

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

// Available system resources (total and available capacity)
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

// Currently consumed resources
export interface UsedResourcesResponse {
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

export interface Service {
  id: string;
  name: string;
  description: string;
  certified_by?: string;
  architectures?: string[];
  standalone?: boolean;
  version?: string;
}

export interface DeployComponent {
  type: string;
  name?: string;
  description?: string;
  providers: Array<{
    id: string;
    name: string;
    description?: string;
    default?: boolean;
    schema?: string;
    version?: string;
    resources?: {
      cpu?: number;
      memory?: number;
      storage?: number;
      accelerators?: Record<string, number>;
    };
    [key: string]: unknown;
  }>;
}

export interface DeployOptions {
  version: string;
  global_components: DeployComponent[];
  services: DeployOptionsService[];
}

export interface ServiceDeployOptions {
  id: string;
  name: string;
  description?: string;
  version: string;
  components: DeployComponent[];
  resources?: {
    cpu: number;
    memory: number;
    storage?: number;
    accelerators?: Record<string, number>;
  };
}

export interface ProviderSchemaProperty {
  default?: string;
  description?: string;
  title?: string;
  type?: string;
  format?: string;
  oneOf?: Array<{
    const: string;
    description?: string;
    title?: string;
  }>;
}

export interface ProviderSchema {
  $schema?: string;
  properties: {
    model?: ProviderSchemaProperty;
    [key: string]: ProviderSchemaProperty | undefined;
  };
  required?: string[];
  type: string;
}

export interface LLMOption {
  id: string;
  text: string;
  providerId: string;
  providerName: string;
}

export interface DeploymentComponent {
  component_type: string;
  provider_id: string;
  version: string;
  params?: Record<string, unknown>;
}

export interface DeploymentService {
  catalog_id: string;
  version: string;
  components: DeploymentComponent[];
  params?: {
    backend?: Record<string, unknown>;
  };
}

export interface ArchitectureDeploymentPayload {
  name: string;
  catalog_id: string;
  version: string;
  services: DeploymentService[];
}

export interface ServiceDeploymentPayload {
  name: string;
  catalog_id: string;
  version: string;
  deployment_type: "service";
  services: DeploymentService[];
  global_components?: Record<string, string>;
}

export type DeploymentPayload =
  | ArchitectureDeploymentPayload
  | ServiceDeploymentPayload;
