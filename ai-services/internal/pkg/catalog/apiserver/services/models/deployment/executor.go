package deployment

import (
	"context"
	"errors"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ModelExecutor executes already-planned model deployments: it hands local plans to the plan's
// runtime-specific deployer and exposes every model through the LiteLLM gateway.
type ModelExecutor struct {
	componentRepo dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	gateway       litellm.Gateway
	vllm          catalogclient.VLLMClient
}

// NewModelExecutor creates a new ModelExecutor instance.
func NewModelExecutor(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	litellmClient catalogclient.LiteLLMClient,
	vllmClient catalogclient.VLLMClient,
) *ModelExecutor {
	return &ModelExecutor{
		componentRepo: componentRepo,
		connectorRepo: connectorRepo,
		gateway:       litellm.NewGateway(litellmClient),
		vllm:          vllmClient,
	}
}

// ExecuteLocal runs a local plan end to end: prepare the worker (images and model) while the
// component is Downloading, start the pods while it is Deploying, expose them through the
// gateway, then mark it Running. Any failure moves the component to Error. The component row has
// already been written (as Downloading) by the caller, and ExecuteLocal never panics.
//
// A caller that needs the phases separately, as the application flow does, calls Prepare, Start
// and Fail itself; ExecuteLocal is just those three in order.
func (e *ModelExecutor) ExecuteLocal(ctx context.Context, plan *types.LocalPlan) {
	defer func() {
		if r := recover(); r != nil {
			e.Fail(ctx, plan, fmt.Errorf("panic: %v", r))
		}
	}()

	if err := e.Prepare(ctx, plan); err != nil {
		e.Fail(ctx, plan, err)

		return
	}

	if _, err := e.Start(ctx, plan); err != nil {
		e.Fail(ctx, plan, err)

		return
	}

	logger.InfofCtx(ctx, "Local model %s (%s) deployed successfully", plan.Req.Name, plan.ComponentID)
}

// Prepare is the download phase: it pulls the images and downloads the model weights on the
// worker. The component row is Downloading while it runs (it is inserted that way) and stays so;
// Start moves it on. It does not change status itself, so a failure is for the caller to report
// with Fail.
func (e *ModelExecutor) Prepare(ctx context.Context, plan *types.LocalPlan) error {
	return plan.Deployer.Prepare(ctx, plan)
}

// Start is the deploy phase: it marks the component Deploying, starts the pods, exposes them
// through the gateway and marks the component Running. It returns the endpoints the model server
// is reachable at. It expects Prepare to have succeeded; a failure is for the caller to report
// with Fail.
func (e *ModelExecutor) Start(ctx context.Context, plan *types.LocalPlan) (types.ModelEndpoints, error) {
	if err := e.componentRepo.UpdateStatus(ctx, plan.ComponentID, dbmodels.ComponentStatusDeploying, ""); err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to mark component deploying: %w", err)
	}

	eps, err := plan.Deployer.Start(ctx, plan)
	if err != nil {
		return types.ModelEndpoints{}, err
	}

	// LiteLLM is given the egress address the control plane can reach; the DB records the pod's own.
	updateLocalRoute(plan, eps.Egress)
	if err := e.gateway.Register(ctx, &plan.Route); err != nil {
		return types.ModelEndpoints{}, err
	}

	// Persist the LiteLLM model ID so the route can be found (and removed) later. If it cannot be
	// saved the ID would be lost, so roll the route back instead of leaving it orphaned.
	if err := e.saveLiteLLMModelID(ctx, plan); err != nil {
		e.gateway.Deregister(ctx, plan.Route)

		return types.ModelEndpoints{}, err
	}

	endpoints := []map[string]any{{"type": "internal", "url": "http://" + eps.Internal}}
	if err := e.componentRepo.UpdateEndpoints(ctx, plan.ComponentID, endpoints); err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to save endpoints: %w", err)
	}

	if err := e.componentRepo.UpdateStatus(ctx, plan.ComponentID, dbmodels.ComponentStatusRunning, ""); err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to mark component running: %w", err)
	}

	return eps, nil
}

// saveLiteLLMModelID adds {"litellm": {"model_id": <id>}} to the component's stored metadata. The
// row is read first so the metadata written at insert (model, profile, params) is kept as is.
func (e *ModelExecutor) saveLiteLLMModelID(ctx context.Context, plan *types.LocalPlan) error {
	comp, err := e.componentRepo.GetByID(ctx, plan.ComponentID)
	if err != nil {
		return fmt.Errorf("failed to load component to save litellm model id: %w", err)
	}

	if comp == nil {
		return fmt.Errorf("component %s not found to save litellm model id", plan.ComponentID)
	}

	metadata := liteLLMMetadata(comp.Metadata, plan.Route.LiteLLMModelID)

	if err := e.componentRepo.UpdateMetadata(ctx, plan.ComponentID, metadata); err != nil {
		return fmt.Errorf("failed to save litellm model id: %w", err)
	}

	return nil
}

// liteLLMMetadata adds {"litellm": {"model_id": <id>}} to metadata, keeping every other key, and
// returns it. A nil metadata is replaced by a new map.
func liteLLMMetadata(metadata map[string]any, modelID string) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}

	metadata[catalogconstants.MetadataKeyLiteLLM] = map[string]any{
		catalogconstants.MetadataKeyLiteLLMModelID: modelID,
	}

	return metadata
}

// Fail records that a local plan failed: the component moves to Error with the cause as its
// message.
func (e *ModelExecutor) Fail(ctx context.Context, plan *types.LocalPlan, cause error) {
	logger.ErrorfCtx(ctx, "Model deployment failed for %s: %v", plan.ComponentID, cause)

	// Use a fresh context: ctx may already be expired (timeout) and the status must still be saved.
	statusCtx := context.WithoutCancel(ctx)
	if err := e.componentRepo.UpdateStatus(statusCtx, plan.ComponentID, dbmodels.ComponentStatusError, cause.Error()); err != nil {
		logger.ErrorfCtx(ctx, "failed to mark component %s as errored: %v", plan.ComponentID, err)
	}
}

// updateLocalRoute completes plan.Route for a deployed pod by setting the endpoint-derived fields
// the planner could not know. egress is the Deployer's ModelEndpoints.Egress, used as is; making
// it reachable from the control plane is the Deployer's job (see types.Deployer).
func updateLocalRoute(plan *types.LocalPlan, egress string) {
	plan.Route.APIBase = fmt.Sprintf("http://%s%s", egress, plan.RootMetadata.LiteLLM.APIBasePath)
	plan.Route.TokenizeURL = fmt.Sprintf("http://%s/tokenize", egress)
}

// ExecuteRemote runs a remote plan synchronously: add the LiteLLM route, verify connectivity and
// persist a connector. Any failure rolls the route back. A connectivity failure is reported as 422.
func (e *ModelExecutor) ExecuteRemote(ctx context.Context, plan *types.RemotePlan) error {
	// A provider that does not need the route is resolved first, so an unusable model is rejected
	// before anything is registered.
	if !plan.Provider.RequiresRoute() {
		if err := e.setMaxModelLen(ctx, plan); err != nil {
			return err
		}
	}

	if err := e.gateway.Register(ctx, &plan.Route); err != nil {
		var probeErr *litellm.ProbeError
		if errors.As(err, &probeErr) {
			return validators.Unprocessable("%s", probeErr.Error())
		}

		return err
	}

	if plan.Provider.RequiresRoute() {
		if err := e.setMaxModelLen(ctx, plan); err != nil {
			e.gateway.Deregister(ctx, plan.Route)

			return err
		}
	}

	setBatchSizeParam(plan)

	// The route is registered before the connector row is written, so the LiteLLM model ID is
	// already known here and is stored together with the rest of the metadata.
	connector := &dbmodels.Connector{
		ID:        plan.ConnectorID,
		Name:      plan.Req.Name,
		Type:      catalogconstants.ConnectorTypeModels,
		Provider:  plan.Req.ProviderID,
		Status:    dbmodels.ConnectorStatusConnected,
		CreatedBy: plan.Req.CreatedBy,
		Metadata:  liteLLMMetadata(plan.Metadata, plan.Route.LiteLLMModelID),
	}
	if err := e.connectorRepo.Insert(ctx, connector); err != nil {
		e.gateway.Deregister(ctx, plan.Route)

		return fmt.Errorf("failed to insert connector: %w", err)
	}

	return nil
}

// setMaxModelLen resolves the model's maximum length through the plan's provider and records it in
// the params metadata.
func (e *ModelExecutor) setMaxModelLen(ctx context.Context, plan *types.RemotePlan) error {
	maxModelLen, err := plan.Provider.MaxModelLen(ctx, plan, types.RemoteClients{Gateway: e.gateway, VLLM: e.vllm})
	if err != nil {
		return err
	}

	plan.MetadataParams()[catalogconstants.ParamMaxModelLen] = maxModelLen

	return nil
}

// setBatchSizeParam parses maxBatchSize from the parameter map (defaulting to 32, also when it is
// not a whole number) and populates DB params metadata.
func setBatchSizeParam(plan *types.RemotePlan) {
	maxBatchSize := 32
	if n, ok := catalogutils.WholeNumber(plan.Req.Params[catalogconstants.ParamMaxBatchSize]); ok {
		maxBatchSize = n
	}

	plan.MetadataParams()[catalogconstants.ParamMaxBatchSize] = maxBatchSize
}
