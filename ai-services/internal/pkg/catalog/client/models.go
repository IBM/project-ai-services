package client

// ListApplicationsParams holds optional query parameters for listing applications.
type ListApplicationsParams struct {
	// Page is the page number (1-indexed). Default: 1
	Page int
	// PageSize is the number of items per page (max: 100). Default: 20
	PageSize int
	// DeploymentType filters by deployment type: 'architectures' or 'services'
	DeploymentType string
	// CatalogID filters by catalog ID (e.g., 'rag', 'chat', 'digitize', 'summarize')
	CatalogID string
}

// DeleteApplicationParams holds optional query parameters for deleting applications.
type DeleteApplicationParams struct {
	// KeepData preserves underlying data (volumes of databases/service resources) when true. Default: false
	KeepData bool
}

// CreateApplicationRequest represents the payload for creating an application.
type CreateApplicationRequest struct {
	CatalogID string                     `json:"catalog_id"`
	Name      string                     `json:"name"`
	Services  []CreateApplicationService `json:"services"`
	Version   string                     `json:"version,omitempty"`
}

// CreateApplicationService represents a service in the create application request.
type CreateApplicationService struct {
	CatalogID  string                       `json:"catalog_id"`
	Components []CreateApplicationComponent `json:"components,omitempty"`
	Params     map[string]interface{}       `json:"params,omitempty"`
	Version    string                       `json:"version,omitempty"`
}

// CreateApplicationComponent represents a component in the create application request.
type CreateApplicationComponent struct {
	ComponentType string                 `json:"component_type"`
	InstanceID    string                 `json:"instance_id,omitempty"`
	Params        map[string]interface{} `json:"params,omitempty"`
	ProviderID    string                 `json:"provider_id"`
	Version       string                 `json:"version,omitempty"`
}

// CreateApplicationResponse represents the response after creating an application.
type CreateApplicationResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
