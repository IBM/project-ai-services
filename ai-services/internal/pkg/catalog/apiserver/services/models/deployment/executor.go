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
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// ModelExecutor executes already-planned model deployments: it hands local plans to the plan's
// runtime-specific deployer and exposes every model through the LiteLLM gateway.
type ModelExecutor struct {
	componentRepo dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	gateway       litellm.Gateway
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
		gateway:       litellm.NewGateway(litellmClient),
	}
}

// ExecuteLocal runs a local plan end to end: prepare the worker (images and model) while the
// component is Downloading, start the pods while it is Deploying, expose them through the
// gateway, then mark it Running. Any failure moves the component to Error. The component row has
// already been written (as Downloading) by the caller, and ExecuteLocal never panics.
//
// A caller that needs the steps separately (e.g. to download ahead of starting) can call
// plan.Deployer.Prepare and plan.Deployer.Start itself and manage status on its own.
func (e *ModelExecutor) ExecuteLocal(ctx context.Context, plan *types.LocalPlan) {
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
	// The row is inserted as Downloading, so the pull and download run under that status.
	if err := plan.Deployer.Prepare(ctx, plan); err != nil {
		return err
	}

	if err := e.componentRepo.UpdateStatus(ctx, plan.ComponentID, dbmodels.ComponentStatusDeploying, ""); err != nil {
		return fmt.Errorf("failed to mark component deploying: %w", err)
	}

	eps, err := plan.Deployer.Start(ctx, plan)
	if err != nil {
		return err
	}

	// LiteLLM is given the egress address the control plane can reach; the DB records the pod's own.
	updateLocalRoute(plan, eps.Egress)
	if err := e.gateway.Register(ctx, &plan.Route); err != nil {
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

// updateLocalRoute completes plan.Route for a deployed pod by setting the endpoint-derived fields
// the planner could not know. egress is the Deployer's ModelEndpoints.Egress, used as is; making
// it reachable from the control plane is the Deployer's job (see the TODO(mTLS) on types.Deployer).
func updateLocalRoute(plan *types.LocalPlan, egress string) {
	plan.Route.APIBase = fmt.Sprintf("http://%s%s", egress, plan.RootMetadata.LiteLLM.APIBasePath)
	plan.Route.TokenizeURL = fmt.Sprintf("http://%s/tokenize", egress)
}

// ExecuteRemote runs a remote plan synchronously: add the LiteLLM route, verify connectivity and
// persist a connector. Any failure rolls the route back. A connectivity failure is reported as 422.
func (e *ModelExecutor) ExecuteRemote(ctx context.Context, plan *types.RemotePlan) error {
	if err := e.gateway.Register(ctx, &plan.Route); err != nil {
		var probeErr *litellm.ProbeError
		if errors.As(err, &probeErr) {
			return validators.Unprocessable("%s", probeErr.Error())
		}

		return err
	}

	connector := &dbmodels.Connector{
		ID:        plan.ConnectorID,
		Name:      plan.Req.Name,
		Type:      catalogconstants.ConnectorTypeModels,
		Provider:  plan.Req.ProviderID,
		Status:    dbmodels.ConnectorStatusConnected,
		CreatedBy: plan.Req.CreatedBy,
		Metadata:  plan.Metadata,
	}
	if err := e.connectorRepo.Insert(ctx, connector); err != nil {
		e.gateway.Deregister(ctx, plan.Route)

		return fmt.Errorf("failed to insert connector: %w", err)
	}

	return nil
}
