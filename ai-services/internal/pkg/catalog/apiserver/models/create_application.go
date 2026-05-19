package models

// CreateApplicationRequest represents the request body for creating a new application.
type CreateApplicationRequest struct {
	Name         string    `json:"name" binding:"required,min=3,max=100"`
	Architecture string    `json:"architecture" binding:"required"`
	Services     []Service `json:"services" binding:"required,dive"`
}

// Service represents a service configuration in the application.
type Service struct {
	Type       string      `json:"type" binding:"required,eq=service"`
	ServiceID  string      `json:"service_id" binding:"required"`
	Enabled    bool        `json:"enabled"`
	Version    string      `json:"version"`
	Components []Component `json:"components" binding:"required,dive"`
}

// Component represents a component configuration for a service.
type Component struct {
	Type          string                 `json:"type" binding:"required,eq=component"`
	ComponentType string                 `json:"component_type" binding:"required"`
	ProviderID    string                 `json:"provider_id" binding:"required"`
	InstanceID    string                 `json:"instance_id"`
	Params        map[string]interface{} `json:"params"`
}

// CreateApplicationResponse represents the response after creating an application.
type CreateApplicationResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Made with Bob
