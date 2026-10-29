package models

// CreateModelRequest represents the request payload for POST /api/v1/models.
type CreateModelRequest struct {
	DeploymentType string         `json:"deployment_type" binding:"required"` // "local" or "remote"
	ComponentType  string         `json:"component_type" binding:"required"`  // "llm", "embedding", "reranker"
	Name           string         `json:"name" binding:"required,min=3,max=100"`
	ProviderID     string         `json:"provider_id" binding:"required"`
	Model          string         `json:"model" binding:"required"`
	Profile        string         `json:"profile,omitempty"`
	WorkerSelector string         `json:"worker_selector,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
	CreatedBy      string         `json:"-"`
}

// CreateModelResponse represents the response body returned for POST /api/v1/models.
type CreateModelResponse struct {
	ID string `json:"id"`
}

