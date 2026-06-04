package models

// CreateApplicationRequest represents the request body for creating a new application.
type CreateApplicationRequest struct {
	Name      string    `json:"name" binding:"required,min=3,max=100"`
	CatalogID string    `json:"catalog_id" binding:"required"`
	Version   string    `json:"version" binding:"required"`
	Services  []Service `json:"services" binding:"required,dive"`
	CreatedBy string    `json:"-"` // Set from auth context, not from request body
}

// Service represents a service configuration in the application.
type Service struct {
	CatalogID  string         `json:"catalog_id" binding:"required"`
	Version    string         `json:"version" binding:"required"`
	Components []Component    `json:"components" binding:"required,dive"`
	Params     map[string]any `json:"params"` // Service-level parameters
}

// Component represents a component configuration for a service.
type Component struct {
	ComponentType string         `json:"component_type" binding:"required"`
	ProviderID    string         `json:"provider_id" binding:"required"`
	InstanceID    string         `json:"instance_id"`
	Version       string         `json:"version" binding:"required"`
	Params        map[string]any `json:"params"`
}

// CreateApplicationResponse represents the response after creating an application.
type CreateApplicationResponse struct {
	ID string `json:"id"`
}

// Made with Bob
