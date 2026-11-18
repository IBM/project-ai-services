package deletion

import (
	"context"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deletion/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Planner plans model deletions: it resolves where a model runs and which gateway route to remove.
// Planning has no side effects, so an unreachable worker fails the request (422) before the
// component is marked as being deleted.
type Planner struct {
	catalogProvider *catalog.CatalogProvider
	workers         worker.Resolver
}

// NewPlanner creates a new deletion Planner.
func NewPlanner(provider *catalog.CatalogProvider, workers worker.Resolver) *Planner {
	return &Planner{catalogProvider: provider, workers: workers}
}

// PlanLocal resolves where and how a local model is undeployed: the runtime of the worker it runs
// on, and the gateway route to remove. workerName is the name of the model's worker, empty for the
// control-plane worker.
func (p *Planner) PlanLocal(ctx context.Context, comp *dbmodels.Component, workerName string) (*types.LocalPlan, error) {
	workerName = worker.DefaultName(workerName)

	rt, err := p.workers.NewRuntime(ctx, workerName, "")
	if err != nil {
		return nil, validators.Unprocessable("cannot reach worker %q: %v", workerName, err)
	}

	if rt.Type() != runtimeTypes.RuntimeTypePodman {
		return nil, validators.Unprocessable("models are unsupported on %s runtime", rt.Type())
	}

	// The provider names the tokenize passthrough. A provider the catalog no longer lists (e.g. a
	// removed bundle) must not make the model undeletable: the passthrough then gets an ID that
	// matches nothing, which LiteLLM reports as not found.
	var provider string
	if root, err := p.catalogProvider.LoadComponent(comp.Type, comp.Provider); err == nil && root.LiteLLM != nil {
		provider = root.LiteLLM.Provider
	}

	return &types.LocalPlan{
		ComponentID: comp.ID,
		Runtime:     rt,
		Route: litellm.Route{
			LiteLLMModelID: liteLLMModelID(comp.Metadata),
			KeyAlias:       keyAlias(comp.Metadata, comp.ID),
			Tag:            comp.ID.String(),
			Provider:       provider,
		},
	}, nil
}

// PlanRemote resolves the gateway route to remove for a remote model.
func (p *Planner) PlanRemote(conn *dbmodels.Connector) *types.RemotePlan {
	return &types.RemotePlan{
		ConnectorID: conn.ID,
		Route: litellm.Route{
			LiteLLMModelID: liteLLMModelID(conn.Metadata),
			KeyAlias:       keyAlias(conn.Metadata, conn.ID),
			Tag:            conn.ID.String(),
			Provider:       conn.Provider,
		},
	}
}

// keyAlias returns the alias of the model's public LiteLLM key, the same one the key was generated
// with. A model name missing from the metadata gives an alias that matches no key, which LiteLLM
// reports as not found.
func keyAlias(metadata map[string]any, id uuid.UUID) string {
	model, _ := metadata[catalogconstants.MetadataKeyModel].(string)

	return litellm.PublicKeyAlias(model, id)
}

// liteLLMModelID reads the LiteLLM model ID stored by the deployment executor under
// {"litellm": {"model_id": <id>}}. It returns "" when none was saved.
func liteLLMModelID(metadata map[string]any) string {
	section, _ := metadata[catalogconstants.MetadataKeyLiteLLM].(map[string]any)
	id, _ := section[catalogconstants.MetadataKeyLiteLLMModelID].(string)

	return id
}
