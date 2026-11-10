package models

import (
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// DeploymentType selects how a model is provisioned.
type DeploymentType string

const (
	// DeploymentTypeLocal starts a model pod on a worker and registers a LiteLLM route (async).
	DeploymentTypeLocal DeploymentType = "local"
	// DeploymentTypeRemote registers an externally hosted endpoint as a connector (sync).
	DeploymentTypeRemote DeploymentType = "remote"
)

// CreateModelRequest represents the request payload for POST /api/v1/models.
type CreateModelRequest struct {
	DeploymentType DeploymentType `json:"deployment_type" binding:"required,oneof=local remote"`
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

// ListModelsRequest carries the parameters for GET /api/v1/models. Empty filters are not applied.
type ListModelsRequest struct {
	Page           int
	PageSize       int
	ComponentType  string
	DeploymentType DeploymentType
	// ProviderIDs and Models are multi-value filters: a model matches any of the values.
	ProviderIDs []string
	Models      []string
	Status      string
	// CreatedBy restricts the list to models created by this user. It is set from the token by
	// the handler.
	CreatedBy string
}

// ModelListItem is one model, local or remote, in the list response.
type ModelListItem struct {
	ID             string             `json:"id"`
	DeploymentType DeploymentType     `json:"deployment_type"`
	Name           string             `json:"name"`
	Provider       types.ProviderInfo `json:"provider"`
	// Worker is null for remote models and for local models on the control-plane Podman socket.
	Worker *types.ApplicationWorker `json:"worker"`
	Model  string                   `json:"model"`
	// Params are the non-sensitive provider params the model was created with.
	Params            map[string]any `json:"params,omitempty"`
	Status            string         `json:"status"`
	Message           string         `json:"message"`
	ConnectedServices int            `json:"connected_services"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// ModelApplication is an application that uses a model.
type ModelApplication struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ModelDetail is the response body for GET /api/v1/models/:id: one model, local or remote.
type ModelDetail struct {
	ID             string             `json:"id"`
	DeploymentType DeploymentType     `json:"deployment_type"`
	Name           string             `json:"name"`
	Provider       types.ProviderInfo `json:"provider"`
	// Worker is null for remote models and for local models on the control-plane Podman socket.
	Worker *types.ApplicationWorker `json:"worker"`
	Model  string                   `json:"model"`
	// Params are the non-sensitive provider params the model was created with.
	Params  map[string]any `json:"params,omitempty"`
	Status  string         `json:"status"`
	Message string         `json:"message,omitempty"`
	// Endpoints are where a local model is called: the public  Omitted for remote models.LiteLLM gateway, whose url is the
	// value of LITELLM_PUBLIC_ENDPOINT (empty if unset).
	Endpoints []map[string]any `json:"endpoints,omitempty"`
	// ConnectedServices is the number of services using the model, as in the list response.
	ConnectedServices int `json:"connected_services"`
	// Applications are the applications that use the model, one entry per application.
	Applications []ModelApplication `json:"applications"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// ModelListResponse is the response body for GET /api/v1/models.
type ModelListResponse struct {
	Data       []ModelListItem          `json:"data"`
	Pagination types.PaginationMetadata `json:"pagination"`
}
