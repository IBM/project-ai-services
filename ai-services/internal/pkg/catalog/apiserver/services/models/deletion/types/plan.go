package types

import (
	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// LocalPlan is a fully resolved local-model undeploy.
type LocalPlan struct {
	ComponentID uuid.UUID
	Runtime     runtime.Runtime
	// Route identifies the model in the gateway: its LiteLLM model ID and the provider and tag
	// its tokenize passthrough and public key are named after.
	Route litellm.Route
}

// RemotePlan is a fully resolved remote-model removal.
type RemotePlan struct {
	ConnectorID uuid.UUID
	Route       litellm.Route
}
