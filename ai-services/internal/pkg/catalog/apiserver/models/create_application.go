package models

import "maps"

// CreateApplicationRequest represents the request body for creating a new application.
type CreateApplicationRequest struct {
	Name      string    `json:"name" binding:"required,min=3,max=100"`
	CatalogID string    `json:"catalog_id" binding:"required"`
	Version   string    `json:"version" binding:"required"`
	Services  []Service `json:"services" binding:"required,dive"`
	CreatedBy string    `json:"-"` // Set from auth context, not from request body
	// WorkerName is the name of a connected remote worker to deploy to.
	// When empty the application is deployed locally.
	WorkerName string `json:"worker_name,omitempty"`
}

// Service represents a service configuration in the application.
// Connectors is an optional field for attaching pre-registered connector records
// (e.g. datasource connectors) to this service. Components is unchanged.
type Service struct {
	CatalogID  string      `json:"catalog_id" binding:"required"`
	Version    string      `json:"version" binding:"required"`
	Components []Component `json:"components" binding:"required,dive"`
	// Connectors lists pre-registered connector records to attach to this service
	// after the application reaches Running status. Each entry is validated against
	// the service's catalog YAML (datasource block) and the connectors table before
	// deployment begins. Omitting this field (or passing an empty list) is valid.
	Connectors []ConnectorRef `json:"connectors,omitempty"`
	Params     map[string]any `json:"params"` // Service-level parameters
}

// ConnectorRef references a pre-registered connector record by its UUID.
// Type identifies the connector kind and must match the record's type in the
// connectors table (e.g. "datasource").
type ConnectorRef struct {
	// ID is the UUID from the connectors table.
	ID string `json:"id" binding:"required"`
	// Type is the connector kind (e.g. "datasource").
	Type string `json:"type" binding:"required"`
}

// Component represents a component configuration for a service.
// There is deliberately no profile field: the hardware profile of a model component is chosen by the
// backend (the model's default_profile, later the service's recommendation).
type Component struct {
	ComponentType string `json:"component_type" binding:"required"`
	ProviderID    string `json:"provider_id" binding:"required"`
	Version       string `json:"version" binding:"required"`
	// Model is the model ID for llm / embedding / reranker components.
	Model  string         `json:"model,omitempty"`
	Params map[string]any `json:"params"`
}

// EffectiveModel returns the requested model: the top-level Model, else the legacy params.model
// so existing clients keep working.
func (c Component) EffectiveModel() string {
	if c.Model != "" {
		return c.Model
	}

	m, _ := c.Params["model"].(string)

	return m
}

// ParamsWithModel returns the component params with the effective model set under "model", so a
// top-level model is treated like params.model everywhere params are used (hashing, model download,
// persisted metadata, template values). The request's own Params map is never modified.
func (c Component) ParamsWithModel() map[string]any {
	model := c.EffectiveModel()
	if model == "" {
		return c.Params
	}

	out := make(map[string]any, len(c.Params)+1)
	maps.Copy(out, c.Params)
	out["model"] = model

	return out
}

// CreateApplicationResponse represents the response after creating an application.
type CreateApplicationResponse struct {
	ID string `json:"id"`
}

// Made with Bob
