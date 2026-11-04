package modelservice

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	modelsvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// ModelService implements the ModelServiceInterface for model management.
//
// It only orchestrates; like the application flow, the work is split into phases that live in
// the services/models package:
//   - validate.go: provider-independent request validation and name uniqueness (this package).
//   - ModelPlanner:  plan phase, no side effects. Resolves a valid request into a LocalPlan or
//     RemotePlan (catalog, schema, worker, runtime and deployer lookups).
//   - ModelExecutor: execute phase. Local plans run in the background through the plan's
//     runtime-specific Deployer; remote plans run synchronously. Both register the LiteLLM route.
type ModelService struct {
	componentRepo dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	planner       *modelsvc.ModelPlanner
	executor      *modelsvc.ModelExecutor
}

// NewModelService creates a new ModelService instance.
// componentRepo, connectorRepo and catalogProvider are required; workerRepo and workerRegistry
// are optional (without them only the control-plane worker can be targeted).
func NewModelService(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	workerRepo dbrepo.WorkerRepository,
	workerRegistry stream.WorkerRegistry,
	catalogProvider *catalog.CatalogProvider,
	litellmClient catalogclient.LiteLLMClient,
) *ModelService {
	if litellmClient == nil {
		litellmClient = catalogclient.NewLiteLLMClient("", "")
	}

	return &ModelService{
		componentRepo: componentRepo,
		connectorRepo: connectorRepo,
		planner: modelsvc.NewModelPlanner(catalogProvider).
			WithWorkerRepo(workerRepo).
			WithWorkerRegistry(workerRegistry),
		executor: modelsvc.NewModelExecutor(componentRepo, connectorRepo, litellmClient),
	}
}

// CreateModel creates a new model (local pod deployment or remote connector).
//
// Errors of type *validators.ValidationError carry the HTTP status to return; any other error is
// an internal failure.
func (s *ModelService) CreateModel(ctx context.Context, req apimodels.CreateModelRequest) (*apimodels.CreateModelResponse, error) {
	// Fail fast: models (local and remote) are not supported when the control plane runs on OpenShift.
	if vars.RuntimeFactory != nil && vars.RuntimeFactory.GetRuntimeType() == runtimeTypes.RuntimeTypeOpenShift {
		return nil, validators.BadRequest("models are unsupported on openshift runtime")
	}

	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	if err := s.planner.ValidateComponentType(req.ComponentType); err != nil {
		return nil, err
	}

	if err := s.ensureNameAvailable(ctx, req.Name); err != nil {
		return nil, err
	}

	if req.DeploymentType == apimodels.DeploymentTypeLocal {
		return s.createLocalModel(ctx, req)
	}

	return s.createRemoteModel(ctx, req)
}

// createLocalModel plans the deployment, records a Deploying component and executes the plan in
// the background. It returns as soon as the component row exists (202 semantics).
func (s *ModelService) createLocalModel(ctx context.Context, req apimodels.CreateModelRequest) (*apimodels.CreateModelResponse, error) {
	plan, err := s.planner.PlanLocal(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := s.insertComponent(ctx, plan); err != nil {
		return nil, err
	}

	// Detach from the request's cancellation (the response is sent before the deployment ends)
	// while keeping its values, such as the request ID used by the logger.
	go s.executor.ExecuteLocal(context.WithoutCancel(ctx), plan)

	return &apimodels.CreateModelResponse{ID: plan.ComponentID.String()}, nil
}

func (s *ModelService) insertComponent(ctx context.Context, plan *types.LocalPlan) error {
	req := plan.Req

	component := &dbmodels.Component{
		ID:        plan.ComponentID,
		Name:      req.Name,
		Type:      req.ComponentType,
		Provider:  req.ProviderID,
		Status:    dbmodels.ComponentStatusDownloading,
		CreatedBy: req.CreatedBy,
		WorkerID:  plan.Worker.ID,
		Metadata:  plan.ComponentMetadata,
	}
	if err := s.componentRepo.Insert(ctx, component); err != nil {
		return fmt.Errorf("failed to insert component: %w", err)
	}

	return nil
}

// createRemoteModel plans the registration and executes it synchronously.
func (s *ModelService) createRemoteModel(ctx context.Context, req apimodels.CreateModelRequest) (*apimodels.CreateModelResponse, error) {
	plan, err := s.planner.PlanRemote(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := s.executor.ExecuteRemote(ctx, plan); err != nil {
		return nil, err
	}

	return &apimodels.CreateModelResponse{ID: plan.ConnectorID.String()}, nil
}
