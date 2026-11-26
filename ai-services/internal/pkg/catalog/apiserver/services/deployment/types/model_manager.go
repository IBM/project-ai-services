package types

import (
	"context"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// ModelManager is what the application flow needs from model management. A model component of an
// application is not handled like other components: each phase of its life is delegated to the
// model service, so an application-deployed model is validated, named, recorded, downloaded,
// deployed and exposed exactly as one created through POST /api/v1/models.
//
//	plan phase      GetRunningInstance / GetConnectedModel / PlanLocalModel
//	record phase    RecordLocalModel
//	download phase  DownloadLocalModel   (component status Downloading)
//	deploy phase    DeployLocalModel     (Deploying, then Running)
//	failure         FailLocalModel       (Error)
//	access          GatewayValues        (what the services reach the model with)
//
// The interface is defined here, where it is used, and implemented by the model service.
// Errors of type *validators.ValidationError carry the HTTP status to return.
type ModelManager interface {
	// GetRunningInstance returns the running model component id, which must be of componentType
	// and created by createdBy. A missing, not running or differently typed instance is a 400, and
	// another user's instance a 403.
	GetRunningInstance(ctx context.Context, id uuid.UUID, componentType, createdBy string) (*dbmodels.Component, error)
	// GetConnectedModel returns the remote model connector id, which must be of type models, connected
	// and created by createdBy. A missing, differently typed or not connected connector is a 400, and
	// another user's connector a 403. Its metadata carries the model and its component type.
	GetConnectedModel(ctx context.Context, id uuid.UUID, createdBy string) (*dbmodels.Connector, error)
	// PlanLocalModel validates the request and resolves how to deploy it, without side effects. The
	// model's name is generated from req.Name, the application's name, and the component ID.
	PlanLocalModel(ctx context.Context, req apimodels.CreateModelRequest) (*modeltypes.LocalPlan, error)
	// RecordLocalModel inserts the plan's component row, as Downloading.
	RecordLocalModel(ctx context.Context, plan *modeltypes.LocalPlan) error
	// DownloadLocalModel pulls the images and downloads the model on the plan's worker.
	DownloadLocalModel(ctx context.Context, plan *modeltypes.LocalPlan) error
	// DeployLocalModel starts the model, exposes it through the gateway and marks it Running.
	DeployLocalModel(ctx context.Context, plan *modeltypes.LocalPlan) error
	// FailLocalModel moves the plan's component to Error with cause as its message.
	FailLocalModel(ctx context.Context, plan *modeltypes.LocalPlan, cause error)
	// GatewayValues returns the values the services of an application reach a running model with,
	// through the gateway: its "host" and "port", the application's "key" for the model and the
	// model's "tokenize_endpoint". The deployer merges them into the values of the services that
	// use the model, so the templates read them as .Values.<componentType>.<name>.
	GatewayValues(ctx context.Context, access modeltypes.ModelAccess) (map[string]any, error)
	// RevokeGatewayKeys revokes the keys GatewayValues generated for the application, one per model
	// it uses. ids are the IDs of the components and connectors the application depends on; the
	// ones that are not models are ignored. Revoking a key that does not exist is not an error.
	RevokeGatewayKeys(ctx context.Context, applicationID uuid.UUID, ids []uuid.UUID) error
}
