package deployment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// localDeployTimeout bounds the whole background deployment (image pull + model download + start).
	localDeployTimeout = 60 * time.Minute

	remoteConnectedMessage = "Endpoint reachable and credentials accepted"
)

// ModelExecutor executes already-planned model deployments: it hands local plans to the plan's
// runtime-specific deployer and exposes every model through the LiteLLM gateway.
type ModelExecutor struct {
	componentRepo dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	gateway       gateway
}

// NewModelExecutor creates a new ModelExecutor instance.
func NewModelExecutor(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	litellmClient catalogclient.LiteLLMClient,
) *ModelExecutor {
	return &ModelExecutor{
		componentRepo: componentRepo,
		connectorRepo: connectorRepo,
		gateway:       gateway{client: litellmClient},
	}
}

// ExecuteLocal runs a local plan: deploy the pods, expose them through the gateway, then mark the
// component Running. Any failure moves the component to Error. The component row has already been
// written by the caller, and ExecuteLocal never panics.
func (e *ModelExecutor) ExecuteLocal(ctx context.Context, plan *types.LocalPlan) {
	ctx, cancel := context.WithTimeout(ctx, localDeployTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			e.failLocal(ctx, plan, fmt.Errorf("panic: %v", r))
		}
	}()

	if err := e.runLocal(ctx, plan); err != nil {
		e.failLocal(ctx, plan, err)

		return
	}

	logger.InfofCtx(ctx, "Local model %s (%s) deployed successfully", plan.Req.Name, plan.ComponentID)
}

func (e *ModelExecutor) runLocal(ctx context.Context, plan *types.LocalPlan) error {
	eps, err := plan.Deployer.Deploy(ctx, plan)
	if err != nil {
		return err
	}

	// LiteLLM is given the egress address the control plane can reach; the DB records the pod's own.
	if err := e.gateway.register(ctx, localRoute(plan, eps.Egress)); err != nil {
		return err
	}

	endpoints := []map[string]any{{"type": "internal", "url": "http://" + eps.Internal}}
	if err := e.componentRepo.UpdateEndpoints(ctx, plan.ComponentID, endpoints); err != nil {
		return fmt.Errorf("failed to save endpoints: %w", err)
	}

	if err := e.componentRepo.UpdateStatus(ctx, plan.ComponentID, dbmodels.ComponentStatusRunning, ""); err != nil {
		return fmt.Errorf("failed to mark component running: %w", err)
	}

	return nil
}

func (e *ModelExecutor) failLocal(ctx context.Context, plan *types.LocalPlan, cause error) {
	logger.ErrorfCtx(ctx, "Model deployment failed for %s: %v", plan.ComponentID, cause)

	// Use a fresh context: ctx may already be expired (timeout) and the status must still be saved.
	statusCtx := context.WithoutCancel(ctx)
	if err := e.componentRepo.UpdateStatus(statusCtx, plan.ComponentID, dbmodels.ComponentStatusError, cause.Error()); err != nil {
		logger.ErrorfCtx(ctx, "failed to mark component %s as errored: %v", plan.ComponentID, err)
	}
}

// localRoute builds the LiteLLM route for a deployed pod. egress is the Deployer's
// ModelEndpoints.Egress, used as is; making it reachable from the control plane is the Deployer's
// job (see the TODO(mTLS) on types.Deployer).
func localRoute(plan *types.LocalPlan, egress string) types.Route {
	return types.Route{
		Tag:           plan.ComponentID.String(),
		Provider:      plan.LiteLLM.Provider,
		Mode:          plan.LiteLLM.ModelMode,
		Model:         plan.Req.Model,
		UpstreamModel: fmt.Sprintf("%s/%s", plan.LiteLLM.Provider, plan.Req.Model),
		APIBase:       fmt.Sprintf("http://%s%s", egress, plan.LiteLLM.APIBasePath),
		TokenizeURL:   fmt.Sprintf("http://%s/tokenize", egress),
	}
}

// ExecuteRemote runs a remote plan synchronously: add the LiteLLM route, verify connectivity and
// persist a connector. Any failure rolls the route back. A connectivity failure is reported as 422.
func (e *ModelExecutor) ExecuteRemote(ctx context.Context, plan *types.RemotePlan) error {
	if err := e.gateway.register(ctx, plan.Route); err != nil {
		var probeErr *probeError
		if errors.As(err, &probeErr) {
			return Unprocessable("%s", probeErr.Error())
		}

		return err
	}

	connector := &dbmodels.Connector{
		ID:        plan.ConnectorID,
		Name:      plan.Req.Name,
		Type:      catalogconstants.ConnectorTypeModels,
		Provider:  plan.Req.ProviderID,
		Status:    dbmodels.ConnectorStatusConnected,
		Message:   remoteConnectedMessage,
		CreatedBy: plan.Req.CreatedBy,
		Metadata:  plan.Metadata,
	}
	if err := e.connectorRepo.Insert(ctx, connector); err != nil {
		e.gateway.deregister(ctx, plan.Route)

		return fmt.Errorf("failed to insert connector: %w", err)
	}

	return nil
}
