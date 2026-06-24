package types

// ApplicationListResponse represents the response for listing applications.
type ApplicationListResponse struct {
	Data       []Application      `json:"data"`
	Pagination PaginationMetadata `json:"pagination"`
}

// Application represents an application in the list/get response.
type Application struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	CatalogID      string               `json:"catalog_id"`
	DeploymentType string               `json:"deployment_type"`
	Type           string               `json:"type"`
	Status         string               `json:"status"`
	Message        string               `json:"message"`
	Version        string               `json:"version"`
	Services       []ApplicationService `json:"services,omitempty"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
}

// ApplicationService represents an application service in the list/get response.
type ApplicationService struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	CatalogID string                 `json:"catalog_id,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Message   string                 `json:"message"`
	Endpoints []map[string]any       `json:"endpoints,omitempty"`
	Version   string                 `json:"version,omitempty"`
	Component []ServiceComponentResp `json:"components,omitempty"`
	CreatedAt string                 `json:"created_at,omitempty"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
}

// ServiceComponentResp represents a service component in the get response.
type ServiceComponentResp struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Provider ProviderInfo   `json:"provider"`
	Status   string         `json:"status,omitempty"`
	Message  string         `json:"message,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ProviderInfo represents provider information with ID and name.
type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PaginationMetadata represents pagination information in the response.
type PaginationMetadata struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	TotalItems int  `json:"total_items"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

// ApplicationResourcesResponse represents the resource usage response for an application.
type ApplicationResourcesResponse struct {
	CPU          ApplicationCPUInfo  `json:"cpu"`
	Memory       ApplicationMemInfo  `json:"memory"`
	Accelerators map[string][]string `json:"accelerators"`
}

// ApplicationCPUInfo represents CPU allocation and usage for an application.
type ApplicationCPUInfo struct {
	TotalCores float64 `json:"total_cores"` // Total allocated CPU cores
	UsedCores  float64 `json:"used_cores"`  // Actually used CPU cores
}

// ApplicationMemInfo represents memory allocation and usage for an application.
type ApplicationMemInfo struct {
	TotalBytes int64 `json:"total_bytes"` // Total allocated memory in bytes
	UsedBytes  int64 `json:"used_bytes"`  // Actually used memory in bytes
}

// ApplicationPSResponse represents the response for pod/container status.
type ApplicationPSResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Services   []Pod  `json:"services"`
	Components []Pod  `json:"components"`
}

type Status string

const (
	Waiting    Status = "waiting"
	Running    Status = "running"
	Terminated Status = "terminated"
	Created    Status = "created"
	Paused     Status = "paused"
	Restarting Status = "restarting"
	Exited     Status = "exited"
	Removing   Status = "removing"
	Dead       Status = "dead"
)

// Pod represents the details of a pod.
type Pod struct {
	PodID      string         `json:"pod_id"`
	PodName    string         `json:"pod_name"`
	Status     Status         `json:"status"`
	Created    string         `json:"created"`
	Healthy    bool           `json:"healthy"`
	Containers []PodContainer `json:"containers"`
}

// PodContainer represents a container in a pod.
type PodContainer struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Healthy bool   `json:"healthy"`
}

// Made with Bob
