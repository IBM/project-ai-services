package client

import (
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

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

// CreateApplicationRequest is the body of POST /api/v1/applications.
type CreateApplicationRequest struct {
	Name      string                     `json:"name"`
	CatalogID string                     `json:"catalog_id"`
	Version   string                     `json:"version"`
	Services  []CreateApplicationService `json:"services"`
	// WorkerName is the connected worker to deploy to. Empty deploys locally.
	WorkerName string `json:"worker_name,omitempty"`
}

// CreateApplicationService is one service of a create request.
type CreateApplicationService struct {
	CatalogID  string                       `json:"catalog_id"`
	Version    string                       `json:"version"`
	Components []CreateApplicationComponent `json:"components"`
	// Connectors attaches pre-registered connectors, e.g. remote model connectors of type "models".
	Connectors []CreateApplicationConnector `json:"connectors,omitempty"`
	// Params are the service-level parameters.
	Params map[string]any `json:"params"`
}

// CreateApplicationComponent is one component of a service in a create request.
//
// A model component (llm, embedding, reranker) is given in one of two forms:
//   - a new deployment: ProviderID, Version and Model, with provider-specific config in Params;
//   - an existing instance: InstanceID only. Nothing else may be set.
//
// The profile of a model is chosen by the server and is not part of the request.
type CreateApplicationComponent struct {
	ComponentType string `json:"component_type"`
	ProviderID    string `json:"provider_id,omitempty"`
	Version       string `json:"version,omitempty"`
	// Model is the catalog model to deploy. It is a top-level field, never a param.
	Model string `json:"model,omitempty"`
	// InstanceID is the ID of a running model instance to attach to instead of deploying a new one.
	InstanceID string         `json:"instance_id,omitempty"`
	Params     map[string]any `json:"params"`
}

// CreateApplicationConnector references a pre-registered connector by ID.
type CreateApplicationConnector struct {
	ID string `json:"id"`
	// Type is the connector kind, e.g. "models" or "datasource".
	Type string `json:"type"`
}

// CreateApplicationResponse is the response of POST /api/v1/applications.
type CreateApplicationResponse struct {
	ID string `json:"id"`
}

// Model deployment kinds, as reported by the models API.
const (
	ModelDeploymentLocal  = "local"
	ModelDeploymentRemote = "remote"
)

// Model statuses a model must have to be attached to an application.
const (
	// ModelStatusRunning is the status of a local model that is up.
	ModelStatusRunning = "Running"
	// ModelStatusConnected is the status of a remote model whose connector is reachable.
	ModelStatusConnected = "connected"
)

// ModelProfile is the hardware profile a local model runs with.
type ModelProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Model is a model of the caller as the models API returns it, local or remote.
type Model struct {
	// ID is the component ID of a local model and the connector ID of a remote one. It is what
	// instance_id and connector refs take.
	ID             string                   `json:"id"`
	DeploymentType string                   `json:"deployment_type"`
	Name           string                   `json:"name"`
	ComponentType  types.ComponentType      `json:"component_type"`
	Provider       types.ProviderInfo       `json:"provider"`
	Worker         *types.ApplicationWorker `json:"worker"`
	// Model is the catalog model identifier, e.g. "ibm-granite/granite-4.1-8b-fp8".
	Model   string        `json:"model"`
	Profile *ModelProfile `json:"profile"`
	Status  string        `json:"status"`
	Message string        `json:"message"`
}

// Attachable reports whether an application can use the model now: a local model must be
// Running and a remote one connected.
func (m Model) Attachable() bool {
	switch m.DeploymentType {
	case ModelDeploymentLocal:
		return m.Status == ModelStatusRunning
	case ModelDeploymentRemote:
		return m.Status == ModelStatusConnected
	default:
		return false
	}
}

// modelListResponse is the response of GET /api/v1/models.
type modelListResponse struct {
	Data       []Model                  `json:"data"`
	Pagination types.PaginationMetadata `json:"pagination"`
}
