package types

// ApplicationListResponse represents the response for listing applications.
type ApplicationListResponse struct {
	Data       []Application      `json:"data"`
	Pagination PaginationMetadata `json:"pagination"`
}

// Application represents an application in the list response.
type Application struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	DeploymentType string          `json:"deployment_type"`
	Type           string          `json:"type"`
	Status         string          `json:"status"`
	Message        string          `json:"message,omitempty"`
	Services       []ServiceStatus `json:"services,omitempty"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
}

// ServiceStatus represents the status of a service within an application.
type ServiceStatus struct {
	Type   string `json:"type"`
	Status string `json:"status"`
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
