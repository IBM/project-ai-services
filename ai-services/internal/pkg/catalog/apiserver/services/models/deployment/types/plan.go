package types

import (
	"context"
	texttemplate "text/template"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// WorkerTarget identifies the worker a local model is deployed to.
type WorkerTarget struct {
	// ID is nil when the control-plane (local) worker has no row in the workers table.
	ID   *uuid.UUID
	Name string
}

// Deployer starts the pods of a planned local model on one kind of runtime and returns the model
// server's internal "host:port". Supporting another runtime means adding an implementation under
// repository/ and registering it with the planner; the plan and execute phases do not change.
type Deployer interface {
	Deploy(ctx context.Context, plan *LocalPlan) (endpoint string, err error)
}

// LocalPlan is a fully resolved local-model deployment.
type LocalPlan struct {
	ComponentID uuid.UUID
	Req         apimodels.CreateModelRequest
	Worker      WorkerTarget
	Runtime     runtime.Runtime
	Deployer    Deployer
	// LiteLLM is how the provider's pods are exposed through the gateway (from component metadata).
	LiteLLM catalogtypes.LiteLLMConfig
	// Catalog is scoped to the runtime the pods are deployed on.
	Catalog   *catalog.CatalogProvider
	Metadata  *clitemplates.AppMetadata
	Templates map[string]*texttemplate.Template
	// Values are the rendered template values: provider defaults overridden by params, plus the
	// requested model as .Values.model.
	Values map[string]any
}

// RemotePlan is a fully validated remote-model registration.
type RemotePlan struct {
	ConnectorID uuid.UUID
	Req         apimodels.CreateModelRequest
	Route       Route
	// Metadata is what gets persisted: the non-sensitive params only.
	Metadata map[string]any
}

// Route describes how one model is exposed through the LiteLLM gateway. Both local pods and remote
// connectors are described by a route, so they share the same register/probe/rollback logic.
type Route struct {
	// Tag is the component or connector ID. It tags the LiteLLM model and suffixes the passthrough.
	Tag string
	// Mode is the LiteLLM model mode ("chat", "embedding", "rerank", ...), from component metadata.
	Mode string
	// Model is the name consumers use; UpstreamModel is the provider-prefixed name LiteLLM forwards.
	Model         string
	UpstreamModel string
	APIBase       string
	APIKey        string
	ProjectID     string
	// TokenizeURL, when set, registers a /tokenize passthrough to that target.
	TokenizeURL string
}
