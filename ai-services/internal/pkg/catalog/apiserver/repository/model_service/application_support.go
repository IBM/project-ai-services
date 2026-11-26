package modelservice

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/keys"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/routes"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

// ModelService is what the application flow calls for the model components of an application, so
// that they go through the same plan, record, download and deploy phases as POST /api/v1/models.
var _ deploymenttypes.ModelManager = (*ModelService)(nil)

// GetRunningInstance returns the running model component id, which must be of componentType and
// created by createdBy. Models are scoped to their creator everywhere else, so an application may
// not attach to another user's model.
func (s *ModelService) GetRunningInstance(ctx context.Context, id uuid.UUID, componentType, createdBy string) (*dbmodels.Component, error) {
	comp, err := s.componentRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get model instance %s: %w", id, err)
	}

	// Only a component of a model type (catalog category "models") can be bound as a model.
	if comp == nil || !s.catalogProvider.IsModelComponentType(comp.Type) {
		return nil, validators.BadRequest("model instance %q not found", id)
	}

	if createdBy != "" && comp.CreatedBy != createdBy {
		return nil, validators.Forbidden("user does not own model instance %q", id)
	}

	if comp.Type != componentType {
		return nil, validators.BadRequest("model instance %q is a %q, not a %q", id, comp.Type, componentType)
	}

	if comp.Status != dbmodels.ComponentStatusRunning {
		return nil, validators.BadRequest("model instance %q is %s; it must be %s", id, comp.Status, dbmodels.ComponentStatusRunning)
	}

	return comp, nil
}

// GetConnectedModel returns the remote model connector id, which must be connected and created by
// createdBy, like GetRunningInstance does for a local model. The row is read with its metadata,
// which holds the model and its component type, and must not reach a response.
func (s *ModelService) GetConnectedModel(ctx context.Context, id uuid.UUID, createdBy string) (*dbmodels.Connector, error) {
	conn, err := s.connectorRepo.GetByID(ctx, id, true)
	if errors.Is(err, dbrepo.ErrConnectorNotFound) {
		return nil, validators.BadRequest("model connector %q not found", id)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get model connector %s: %w", id, err)
	}

	if conn.Type != constants.ConnectorTypeModels {
		return nil, validators.BadRequest("connector %q is a %q, not a %q", id, conn.Type, constants.ConnectorTypeModels)
	}

	if createdBy != "" && conn.CreatedBy != createdBy {
		return nil, validators.Forbidden("user does not own model connector %q", id)
	}

	if conn.Status != dbmodels.ConnectorStatusConnected {
		return nil, validators.BadRequest("model connector %q is %s; it must be %s", id, conn.Status, dbmodels.ConnectorStatusConnected)
	}

	return conn, nil
}

// PlanLocalModel validates a local-model request and resolves how to deploy it, with no side
// effects. The request's name is the application's: the model is named after it and a short ID of
// the component, for example "my-app-1a2b3c4d", and that name is checked against the names
// already in use.
func (s *ModelService) PlanLocalModel(ctx context.Context, req apimodels.CreateModelRequest) (*types.LocalPlan, error) {
	req.DeploymentType = constants.DeploymentTypeLocal

	plan, err := s.planner.PlanLocal(ctx, req)
	if err != nil {
		return nil, err
	}

	plan.Req.Name = helpers.ComponentName(req.Name, plan.ComponentID)
	if err := s.ensureNameAvailable(ctx, plan.Req.Name); err != nil {
		return nil, err
	}

	return plan, nil
}

// DownloadLocalModel pulls the container images and downloads the model on the plan's worker.
func (s *ModelService) DownloadLocalModel(ctx context.Context, plan *types.LocalPlan) error {
	return s.executor.Prepare(ctx, plan)
}

// DeployLocalModel starts the model, exposes it through the gateway and marks it Running.
func (s *ModelService) DeployLocalModel(ctx context.Context, plan *types.LocalPlan) error {
	_, err := s.executor.Start(ctx, plan)

	return err
}

// FailLocalModel moves the plan's component to Error with cause as its message.
func (s *ModelService) FailLocalModel(ctx context.Context, plan *types.LocalPlan, cause error) {
	s.executor.Fail(ctx, plan, cause)
}

// Keys of the values GatewayValues returns, as the service templates read them.
const (
	valueKeyHost             = "host"
	valueKeyPort             = "port"
	valueKeyPath             = "path"
	valueKeyKey              = "key"
	valueKeyTokenizeEndpoint = "tokenize_endpoint"
	valueKeyInstanceID       = "instance_id"
	// valueKeyInstanceSlug is the 10-char slug of the model component or connector ID.
	// Service templates use it to name component-scoped secrets, e.g.
	// "llm-{{ .Values.llm.instanceSlug }}-secret-key-{{ .InstanceSlug }}".
	// This is the same key name used by non-model components (see endpoints.go).
	valueKeyInstanceSlug = "instanceSlug"
)

// GatewayValues returns what the services of an application reach a running model with: the
// gateway's address, a key of the application scoped to the model's deployment, the model's
// tokenize endpoint, and the model's instance ID, which is the tag of the key. A service compares
// the instance ID with the tags of a key it is given to authorize it. The model must be running
// and routed, so call it once it is deployed.
func (s *ModelService) GatewayValues(ctx context.Context, access types.ModelAccess) (map[string]any, error) {
	key, err := s.keys.Generate(ctx, keys.Request{
		Model: access.Model,
		ID:    access.ComponentID,
		Alias: applicationKeyAlias(access.Model, access.ComponentID, access.ApplicationID),
	})
	if err != nil {
		return nil, err
	}

	gateway := litellm.GatewayAddress(access.Runtime, !routes.IsLocal(access.WorkerName))

	return map[string]any{
		valueKeyHost:             gateway.Host,
		valueKeyPort:             gateway.Port,
		valueKeyPath:             gateway.PathPrefix,
		valueKeyKey:              key,
		valueKeyTokenizeEndpoint: gateway.TokenizeEndpoint(access.ComponentID),
		valueKeyInstanceID:       access.ComponentID.String(),
		valueKeyInstanceSlug:     catalogutils.GenerateInstanceSlug(access.ComponentID.String()),
	}, nil
}

// applicationKeyAlias is the alias of the key GatewayValues generates for an application, which
// RevokeGatewayKeys must revoke by the same alias.
func applicationKeyAlias(model string, id, applicationID uuid.UUID) string {
	return litellm.InternalKeyAlias(model, id, catalogutils.GenerateInstanceSlug(applicationID.String()))
}

// RevokeGatewayKeys revokes the keys GatewayValues generated for the application. ids are the IDs
// the application depends on: each one that is a model, a model component or a model connector,
// has one key. Anything else, such as a datasource connector or another component, has none and is
// skipped, as is a model that no longer exists.
func (s *ModelService) RevokeGatewayKeys(ctx context.Context, applicationID uuid.UUID, ids []uuid.UUID) error {
	aliases := make([]string, 0, len(ids))

	for _, id := range ids {
		model, ok, err := s.modelName(ctx, id)
		if err != nil {
			return err
		}

		if ok {
			aliases = append(aliases, applicationKeyAlias(model, id, applicationID))
		}
	}

	return s.keys.Revoke(ctx, aliases)
}

// modelName returns the model name of the model component or model connector id, and false when id
// is neither.
func (s *ModelService) modelName(ctx context.Context, id uuid.UUID) (string, bool, error) {
	comp, err := s.componentRepo.GetByID(ctx, id)
	if err != nil {
		return "", false, fmt.Errorf("failed to get component %s: %w", id, err)
	}

	if comp != nil {
		if !s.catalogProvider.IsModelComponentType(comp.Type) {
			return "", false, nil
		}

		model, _ := comp.Metadata[constants.MetadataKeyModel].(string)

		return model, model != "", nil
	}

	conn, err := s.connectorRepo.GetByID(ctx, id, true)
	if errors.Is(err, dbrepo.ErrConnectorNotFound) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("failed to get connector %s: %w", id, err)
	}

	if conn.Type != constants.ConnectorTypeModels {
		return "", false, nil
	}

	model, _ := conn.Metadata[constants.MetadataKeyModel].(string)

	return model, model != "", nil
}
