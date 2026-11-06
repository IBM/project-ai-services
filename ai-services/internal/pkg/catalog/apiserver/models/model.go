package models

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
