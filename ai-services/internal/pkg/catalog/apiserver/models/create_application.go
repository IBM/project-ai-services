package models

import (
	"maps"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
)

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
//
// A model component (llm, embedding, reranker, ...) is given in one of two forms:
//   - new deployment: ProviderID, Version and Model, with provider-specific config in Params;
//   - existing instance: InstanceID only. Provider, model and params are read from that instance,
//     so none of ProviderID, Version, Model or Params may be set.
//
// ProviderID and Version are therefore not required by the binding; ApplicationValidator enforces
// them when InstanceID is not set. The client cannot send a profile: the backend chooses it.
type Component struct {
	ComponentType string `json:"component_type" binding:"required"`
	ProviderID    string `json:"provider_id,omitempty"`
	Version       string `json:"version,omitempty"`
	// Model is the catalog model to deploy, for components of a model provider. It is a top-level
	// field, not a param.
	Model string `json:"model,omitempty"`
	// InstanceID is the ID of an existing, running model instance (components.id) to attach to
	// instead of deploying a new one.
	InstanceID string         `json:"instance_id,omitempty"`
	Params     map[string]any `json:"params"`
	// Profile is the hardware profile of a model component, set by the planner: the one the
	// service recommends for a new model, or the one an existing instance runs with. It is not
	// part of the request.
	Profile string `json:"-"`
}

// ValueParams returns the params the component's values are built from: Params plus the
// top-level Model, which templates read as .Values.<component_type>.model. It returns Params
// itself when there is no model.
func (c Component) ValueParams() map[string]any {
	if c.Model == "" {
		return c.Params
	}

	out := make(map[string]any, len(c.Params)+1)
	maps.Copy(out, c.Params)
	out[constants.ParamKeyModel] = c.Model

	return out
}

// IdentityParams returns what makes two component entries the same deployment: ValueParams plus
// the instance they are bound to. Entries with equal type, provider and IdentityParams share one
// deployment.
func (c Component) IdentityParams() map[string]any {
	params := c.ValueParams()
	if c.InstanceID == "" {
		return params
	}

	out := make(map[string]any, len(params)+1)
	maps.Copy(out, params)
	out[constants.ParamKeyInstanceID] = c.InstanceID

	return out
}

// CreateApplicationResponse represents the response after creating an application.
type CreateApplicationResponse struct {
	ID string `json:"id"`
}

// Made with Bob
