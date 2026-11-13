package models

import (
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// CreateModelRequest represents the request payload for POST /api/v1/models.
type CreateModelRequest struct {
	DeploymentType constants.DeploymentType `json:"deployment_type" binding:"required,oneof=local remote"`
	// ComponentType is a catalog component type whose providers have `category: models`
	// (see the component's metadata.yaml); the valid values come from the catalog.
	ComponentType string         `json:"component_type" binding:"required"`
	Name          string         `json:"name" binding:"required,min=3,max=100"`
	ProviderID    string         `json:"provider_id" binding:"required"`
	Model         string         `json:"model" binding:"required"`
	Profile       string         `json:"profile,omitempty"`
	WorkerName    string         `json:"worker_name,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
	CreatedBy     string         `json:"-"`
}

// CreateModelResponse represents the response body returned for POST /api/v1/models.
type CreateModelResponse struct {
	ID string `json:"id"`
}

// DeleteModelResponse represents the body returned for an accepted DELETE /api/v1/models/:id,
// that is, for a local model whose undeploy continues in the background.
type DeleteModelResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ListModelsRequest carries the parameters for GET /api/v1/models. Empty filters are not applied.
type ListModelsRequest struct {
	Page           int
	PageSize       int
	ComponentType  string
	DeploymentType constants.DeploymentType
	// ProviderIDs and Models are multi-value filters: a model matches any of the values.
	ProviderIDs []string
	Models      []string
	Status      string
	// CreatedBy restricts the list to models created by this user. It is set from the token by
	// the handler.
	CreatedBy string
}

// ModelBase holds the fields shared by the list and detail representations of a model.
type ModelBase struct {
	ID             string                   `json:"id"`
	DeploymentType constants.DeploymentType `json:"deployment_type"`
	Name           string                   `json:"name"`
	Provider       types.ProviderInfo       `json:"provider"`
	// Worker is null for remote models and for local models on the control-plane Podman socket.
	Worker *types.ApplicationWorker `json:"worker"`
	Model  string                   `json:"model"`
	// Params are the non-sensitive provider params the model was created with.
	Params  map[string]any `json:"params,omitempty"`
	Status  string         `json:"status"`
	Message string         `json:"message"`
	// ConnectedServices is the number of services using the model.
	ConnectedServices int       `json:"connected_services"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// ModelListItem is one model, local or remote, in the list response.
type ModelListItem struct {
	ModelBase
}

// ModelApplication is an application that uses a model.
type ModelApplication struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ModelEndpoint is one address a model is called at.
type ModelEndpoint struct {
	// Type is the kind of endpoint, e.g. "external" for the public model gateway.
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ModelDetail is the response body for GET /api/v1/models/:id: one model, local or remote.
type ModelDetail struct {
	ModelBase
	// Endpoints are where the model is called. Local and remote models alike are reached through
	// the public model gateway.
	Endpoints []ModelEndpoint `json:"endpoints,omitempty"`
	// Applications are the applications that use the model, one entry per application.
	Applications []ModelApplication `json:"applications"`
}

// ModelListResponse is the response body for GET /api/v1/models.
type ModelListResponse struct {
	Data       []ModelListItem          `json:"data"`
	Pagination types.PaginationMetadata `json:"pagination"`
}

// GenerateModelKeyResponse represents the response body returned for POST /api/v1/models/{id}/keys.
// Key is the raw LiteLLM bearer token; it is shown once and never stored or logged.
type GenerateModelKeyResponse struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Model string `json:"model"`
}
