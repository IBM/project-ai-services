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
	DeploymentType string               `json:"deployment_type"`
	Type           string               `json:"type"`
	Status         string               `json:"status"`
	Message        string               `json:"message"`
	Services       []ApplicationService `json:"services,omitempty"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
}

// ApplicationService represents an application service in the list/get response.
type ApplicationService struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
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
	Type     string         `json:"type"`
	Provider string         `json:"provider"`
	Metadata map[string]any `json:"metadata,omitempty"`
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

// Made with Bob
