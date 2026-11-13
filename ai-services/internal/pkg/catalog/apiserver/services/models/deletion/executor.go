package deletion

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deletion/repository/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deletion/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Deleter removes the pods of a local model, with their secrets and volumes, and the component
// row, on one kind of runtime.
type Deleter interface {
	Delete(ctx context.Context, rt runtime.Runtime, componentID uuid.UUID) error
}

// Executor executes already-planned model deletions: it removes the LiteLLM route and the model's
// pods or connector.
type Executor struct {
	componentRepo dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	gateway       litellm.Gateway
	// deleters maps a runtime type to the deleter that removes local model pods on it.
	deleters map[runtimeTypes.RuntimeType]Deleter
}

// NewExecutor creates a new deletion Executor.
func NewExecutor(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	litellmClient catalogclient.LiteLLMClient,
) *Executor {
	return &Executor{
		componentRepo: componentRepo,
		connectorRepo: connectorRepo,
		gateway:       litellm.NewGateway(litellmClient),
		deleters: map[runtimeTypes.RuntimeType]Deleter{
			runtimeTypes.RuntimeTypePodman: podman.NewPodmanDeleter(componentRepo),
		},
	}
}

// ExecuteLocal undeploys a local model end to end: remove its gateway entries (public key,
// passthrough, LiteLLM model), delete its pods (and their secrets and volumes) through the
// worker's runtime, then delete the component row. The component has already been marked Deleting
// by the caller. Any failure moves it to Error, so the
// delete can be retried, and the row is kept. ExecuteLocal never panics.
func (e *Executor) ExecuteLocal(ctx context.Context, plan *types.LocalPlan) {
	defer func() {
		if r := recover(); r != nil {
			e.fail(ctx, plan.ComponentID, fmt.Errorf("panic: %v", r))
		}
	}()

	if err := e.runLocal(ctx, plan); err != nil {
		e.fail(ctx, plan.ComponentID, err)

		return
	}

	logger.InfofCtx(ctx, "Local model %s deleted successfully", plan.ComponentID)
}

func (e *Executor) runLocal(ctx context.Context, plan *types.LocalPlan) error {
	// The gateway goes first (key, passthrough, model) so that no request reaches a model whose
	// pods are being removed.
	if err := e.gateway.Remove(ctx, plan.Route); err != nil {
		return err
	}

	deleter, ok := e.deleters[plan.Runtime.Type()]
	if !ok {
		return fmt.Errorf("models are unsupported on %s runtime", plan.Runtime.Type())
	}

	return deleter.Delete(ctx, plan.Runtime, plan.ComponentID)
}

func (e *Executor) fail(ctx context.Context, id uuid.UUID, cause error) {
	logger.ErrorfCtx(ctx, "Model deletion failed for %s: %v", id, cause)

	// Use a fresh context: ctx may already be expired (timeout) and the status must still be saved.
	statusCtx := context.WithoutCancel(ctx)
	if err := e.componentRepo.UpdateStatus(statusCtx, id, dbmodels.ComponentStatusError, cause.Error()); err != nil {
		logger.ErrorfCtx(ctx, "failed to mark component %s as errored: %v", id, err)
	}
}

// ExecuteRemote removes a remote model synchronously: remove its gateway entries (public key,
// passthrough, LiteLLM model), then delete the connector row. The row is deleted only if no service links to it any
// more, which is checked atomically with the delete (409 otherwise).
func (e *Executor) ExecuteRemote(ctx context.Context, plan *types.RemotePlan) error {
	if err := e.gateway.Remove(ctx, plan.Route); err != nil {
		return err
	}

	_, err := e.connectorRepo.DeleteIfUnlinked(ctx, plan.ConnectorID, dbmodels.DependencyTypeConnector)

	switch {
	case errors.Is(err, dbrepo.ErrConnectorNotFound):
		return validators.NotFound("model %q not found", plan.ConnectorID)
	case errors.Is(err, dbrepo.ErrConnectorInUse):
		return validators.Conflict("model is in use by one or more services")
	case err != nil:
		return fmt.Errorf("failed to delete connector: %w", err)
	}

	return nil
}
