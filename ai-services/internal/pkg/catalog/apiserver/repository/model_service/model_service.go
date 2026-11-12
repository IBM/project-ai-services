package modelservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deletion"
	modelsvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/get"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/keys"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/list"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ModelService implements the ModelServiceInterface for model management.
//
// It only orchestrates; like the application flow, the work is split into phases that live in
// the services/models package:
//   - validate.go: provider-independent request validation, list filter validation and name
//     uniqueness (this package).
//   - ModelPlanner:  plan phase, no side effects. Resolves a valid request into a LocalPlan or
//     RemotePlan (catalog, schema, worker, runtime and deployer lookups).
//   - ModelExecutor: execute phase. Local plans run in the background through the plan's
//     runtime-specific Deployer; remote plans run synchronously. Both register the LiteLLM route.
//   - list.Lister and get.Getter: the read side. They return the models of the caller and the
//     detail of one model.
//   - keys.Manager: generates the public LiteLLM key of a model.
//   - deletion.Planner and deletion.Executor: plan and execute the deletion of a model, with the
//     same split as above (see services/models/deletion).
type ModelService struct {
	componentRepo dbrepo.ComponentRepository
	connectorRepo dbrepo.ConnectorRepository
	svcDepRepo    dbrepo.ServiceDependencyRepository
	// modelRepo reads a model for the ownership check; catalogProvider validates the list filters
	// against the catalog and scopes that read to the model types.
	modelRepo        dbrepo.ModelRepository
	catalogProvider  *catalog.CatalogProvider
	planner          *modelsvc.ModelPlanner
	executor         *modelsvc.ModelExecutor
	lister           *list.Lister
	getter           *get.Getter
	keys             *keys.Manager
	deletionPlanner  *deletion.Planner
	deletionExecutor *deletion.Executor
}

// NewModelService creates a new ModelService instance.
// componentRepo, connectorRepo, modelRepo, svcDepRepo, workers and catalogProvider are required; workers is used to
// reach the worker that hosts a local model. gatewayPublicEndpoint is the public URL of the model
// gateway, reported as the endpoint of every model; it may be empty.
func NewModelService(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	modelRepo dbrepo.ModelRepository,
	svcDepRepo dbrepo.ServiceDependencyRepository,
	workers worker.Resolver,
	catalogProvider *catalog.CatalogProvider,
	gatewayPublicEndpoint string,
) *ModelService {
	litellmClient := catalogclient.NewLiteLLMClient()

	return &ModelService{
		componentRepo:    componentRepo,
		connectorRepo:    connectorRepo,
		svcDepRepo:       svcDepRepo,
		modelRepo:        modelRepo,
		catalogProvider:  catalogProvider,
		planner:          modelsvc.NewModelPlanner(catalogProvider, workers),
		executor:         modelsvc.NewModelExecutor(componentRepo, connectorRepo, litellmClient),
		lister:           list.New(modelRepo, svcDepRepo, catalogProvider),
		getter:           get.New(svcDepRepo, catalogProvider, gatewayPublicEndpoint),
		keys:             keys.NewManager(litellmClient),
		deletionPlanner:  deletion.NewPlanner(catalogProvider, workers),
		deletionExecutor: deletion.NewExecutor(componentRepo, connectorRepo, litellmClient),
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

	if req.DeploymentType == constants.DeploymentTypeLocal {
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
		WorkerID:  plan.WorkerID,
		Metadata:  plan.Metadata,
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

// ListModels returns one page of the caller's models, local and remote, as a single list. It
// rejects filters that can never match, then lists with list.Lister.
func (s *ModelService) ListModels(ctx context.Context, req apimodels.ListModelsRequest) (*apimodels.ModelListResponse, error) {
	if err := s.validateListRequest(req); err != nil {
		return nil, err
	}

	return s.lister.List(ctx, req)
}

// GetModelByID returns one model, local or remote.
//
// As for applications, a missing model is a 404 and a model created by someone else a 403; an empty
// createdBy skips the ownership check. Nothing else about the model is read before it passes.
func (s *ModelService) GetModelByID(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.ModelDetail, error) {
	row, err := s.ownedModel(ctx, id, createdBy)
	if err != nil {
		return nil, err
	}

	return s.getter.Detail(ctx, row)
}

// GenerateModelKey generates the public LiteLLM key of the model identified by id. See
// keys.Manager.Generate.
//
// As for GetModelByID, a missing model is a 404 and a model created by someone else a 403, and
// nothing is revoked or generated before the check passes. Errors of type *validators.ValidationError
// carry the HTTP status to return; any other error is an internal failure.
func (s *ModelService) GenerateModelKey(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.GenerateModelKeyResponse, error) {
	if vars.RuntimeFactory != nil && vars.RuntimeFactory.GetRuntimeType() == runtimeTypes.RuntimeTypeOpenShift {
		return nil, validators.BadRequest("models are unsupported on openshift runtime")
	}

	row, err := s.ownedModel(ctx, id, createdBy)
	if err != nil {
		return nil, err
	}

	return s.keys.Generate(ctx, row)
}

// ownedModel reads a model, local or remote, and checks that createdBy may use it: a missing model
// is a 404 and a model created by someone else a 403. An empty createdBy skips the ownership check.
// The lookup is limited to the model types of the catalog.
func (s *ModelService) ownedModel(ctx context.Context, id uuid.UUID, createdBy string) (*dbrepo.ModelRow, error) {
	row, err := s.modelRepo.GetByID(ctx, id, helpers.New(s.catalogProvider).QueryComponentTypes())
	if err != nil {
		return nil, fmt.Errorf("failed to get model: %w", err)
	}

	if row == nil {
		return nil, validators.NotFound("model %q not found", id)
	}

	if createdBy != "" && row.CreatedBy != createdBy {
		return nil, validators.Forbidden("user does not own this model")
	}

	return row, nil
}

// DeleteModel deletes a model, local or remote, owned by createdBy.
//
// A local model is undeployed in the background (the returned response is non-nil, 202 semantics);
// a remote one is removed before returning (nil response, 204 semantics). A missing model is a 404,
// another user's model a 403, and a model used by a service, or a local model already being
// deleted, a 409.
func (s *ModelService) DeleteModel(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.DeleteModelResponse, error) {
	row, err := s.ownedModel(ctx, id, createdBy)
	if err != nil {
		return nil, err
	}

	// Checked up front so that nothing is torn down for a model that cannot be deleted. For a
	// remote model the delete itself re-checks it atomically.
	counts, err := s.svcDepRepo.GetServiceCountByDependency(ctx, []uuid.UUID{row.ID}, helpers.ModelDependencyType(*row))
	if err != nil {
		return nil, fmt.Errorf("failed to count services using model: %w", err)
	}

	if counts[row.ID] > 0 {
		return nil, validators.Conflict("model is in use by %d service(s)", counts[row.ID])
	}

	if row.DeploymentType == constants.DeploymentTypeLocal {
		return s.deleteLocalModel(ctx, row)
	}

	return nil, s.deleteRemoteModel(ctx, row)
}

// deleteLocalModel plans the undeploy, marks the component Deleting and executes the plan in the
// background. It returns as soon as the status is saved (202 semantics).
func (s *ModelService) deleteLocalModel(ctx context.Context, row *dbrepo.ModelRow) (*apimodels.DeleteModelResponse, error) {
	comp, err := s.componentRepo.GetByID(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get component: %w", err)
	}

	if comp == nil {
		return nil, validators.NotFound("model %q not found", row.ID)
	}

	if comp.Status == dbmodels.ComponentStatusDeleting {
		return nil, validators.Conflict("model is already being deleted")
	}

	plan, err := s.deletionPlanner.PlanLocal(ctx, comp, row.WorkerName)
	if err != nil {
		return nil, err
	}

	if err := s.componentRepo.UpdateStatus(ctx, row.ID, dbmodels.ComponentStatusDeleting, ""); err != nil {
		return nil, fmt.Errorf("failed to mark component deleting: %w", err)
	}

	// Detach from the request's cancellation (the response is sent before the undeploy ends)
	// while keeping its values, such as the request ID used by the logger.
	go s.deletionExecutor.ExecuteLocal(context.WithoutCancel(ctx), plan)

	return &apimodels.DeleteModelResponse{
		ID:      row.ID.String(),
		Status:  string(dbmodels.ComponentStatusDeleting),
		Message: "Undeploy initiated",
	}, nil
}

// deleteRemoteModel removes the route and the connector synchronously.
func (s *ModelService) deleteRemoteModel(ctx context.Context, row *dbrepo.ModelRow) error {
	// The credentials are not needed, but the metadata that holds the LiteLLM model ID is only
	// read with them.
	conn, err := s.connectorRepo.GetByID(ctx, row.ID, true)
	if errors.Is(err, dbrepo.ErrConnectorNotFound) {
		return validators.NotFound("model %q not found", row.ID)
	}

	if err != nil {
		return fmt.Errorf("failed to get connector: %w", err)
	}

	return s.deletionExecutor.ExecuteRemote(ctx, s.deletionPlanner.PlanRemote(conn))
}
