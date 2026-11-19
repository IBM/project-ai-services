package repository

import (
	"context"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// ModelServiceInterface defines the contract for model management business logic.
type ModelServiceInterface interface {
	// CreateModel creates a model deployment (local pod or remote connector).
	CreateModel(ctx context.Context, req apimodels.CreateModelRequest) (*apimodels.CreateModelResponse, error)
	// ListModels returns a paginated, filtered list of the caller's models, local and remote.
	// req.CreatedBy is required and scopes the list to that user.
	ListModels(ctx context.Context, req apimodels.ListModelsRequest) (*apimodels.ModelListResponse, error)
	// GetModelByID returns one model, local or remote, owned by createdBy. A missing model is a
	// *validators.ValidationError with 404, and another user's model one with 403.
	GetModelByID(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.ModelDetail, error)
	// GenerateModelKey generates the public LiteLLM key of the model identified by id, revoking
	// the previous key with the same alias first so at most one exists. The key is returned once
	// and never stored. Returns a *validators.ValidationError: 403 when createdBy is not the model's creator, 404 when id is
	// unknown, 502 when LiteLLM fails.
	GenerateModelKey(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.GenerateModelKeyResponse, error)
	// DeleteModel deletes a model, local or remote, owned by createdBy. A local model is undeployed
	// in the background and a non-nil response is returned; a remote model is removed before
	// returning and the response is nil. A missing model is a *validators.ValidationError with 404,
	// another user's model one with 403, and a model in use (or already being deleted) one with 409.
	DeleteModel(ctx context.Context, id uuid.UUID, createdBy string) (*apimodels.DeleteModelResponse, error)
}

// DatasourceServiceInterface defines the contract for datasource connector business logic.
type DatasourceServiceInterface interface {
	// CreateDatasource validates the request, tests the connection, encrypts credentials,
	// and persists a new datasource connector record.
	CreateDatasource(ctx context.Context, req apimodels.CreateDatasourceRequest) (*apimodels.CreateDatasourceResponse, error)
	// ConnectDatasourcesToApplication links one or more datasource connectors to each eligible service in a running application.
	ConnectDatasourcesToApplication(ctx context.Context, applicationID uuid.UUID, userID string, datasourceIDs []uuid.UUID) (*apimodels.ConnectDatasourcesResponse, error)
	// DisconnectDatasourcesFromApplication removes a single datasource connector from each
	// eligible service in a running application and removes the service_dependency record.
	DisconnectDatasourcesFromApplication(ctx context.Context, applicationID uuid.UUID, userID string, datasourceID uuid.UUID) error
	// GetDatasource retrieves a single datasource by ID with non-sensitive metadata and
	// connected services enriched with live sync state from each service's Digitize pod.
	// Returns a *ValidationError with code 404 when the connector does not exist.
	GetDatasource(ctx context.Context, id uuid.UUID) (*apimodels.GetDatasourceResponse, error)
	// DeleteDatasource removes a datasource connector by ID.
	// Returns a ValidationError with status 404 if not found, 409 if the connector is
	// still linked to one or more services via service_dependencies.
	DeleteDatasource(ctx context.Context, id uuid.UUID) error
	// ListDatasources returns a paginated, optionally filtered list of datasource connectors.
	// Sensitive credential fields are never included in any returned item.
	ListDatasources(ctx context.Context, req apimodels.ListDatasourcesRequest) (*apimodels.DatasourceListResponse, error)
	// UpdateDatasource updates only the updatable credential fields for a datasource.
	// It re-runs the connectivity test with the merged (new credentials + existing structural
	// fields) metadata. If the test passes, the DB record is updated and the new credentials
	// are propagated to every linked Digitize service.
	// Returns 404 when the datasource does not exist, 422 when the connectivity test fails.
	// A 200 is returned even when propagation to some Digitize services fails; in that case,
	// the response body contains a non-empty PropagationErrors list.
	UpdateDatasource(ctx context.Context, id uuid.UUID, req apimodels.UpdateDatasourceRequest) (*apimodels.UpdateDatasourceResponse, error)
	// ListApplicationDatasources returns a paginated list of datasource connectors linked to
	// the given application, enriched with live sync state (status, files, last_sync, message)
	// from each connector's Digitize pod.
	// Returns a *ValidationError with code 404 when the application does not exist.
	ListApplicationDatasources(ctx context.Context, req apimodels.ListApplicationDatasourcesRequest) (*apimodels.ApplicationDatasourceListResponse, error)

	// GetDatasourceApplications returns the list of applications currently connected to a datasource,
	// each enriched with live sync state from its downstream service pod.
	// Returns a *ValidationError with code 404 when the connector does not exist.
	GetDatasourceApplications(ctx context.Context, id uuid.UUID) (*apimodels.DatasourceApplicationsResponse, error)

	// GetApplicationDatasource returns the catalog identity and live Digitize sync state for
	// a datasource that is connected to the given application.
	// Returns a *ValidationError with code 404 when no service_dependencies row links
	// datasourceID to any service of applicationID.
	GetApplicationDatasource(ctx context.Context, applicationID, datasourceID uuid.UUID) (*apimodels.GetApplicationDatasourceResponse, error)
}

// ApplicationServiceInterface defines the contract for application business logic.
type ApplicationServiceInterface interface {
	// ListApplications retrieves a paginated list of applications with filters.
	ListApplications(ctx context.Context, req ListApplicationsRequest) (*types.ApplicationListResponse, error)

	// UpdateApplication updates the display name of an existing application.
	UpdateApplication(ctx context.Context, id uuid.UUID, userID, newName string) (*types.Application, error)

	// CreateApplication creates a new application and initiates async deployment.
	CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error)

	// GetApplicationByID retrieves a single application by ID including its services and components.
	GetApplicationByID(ctx context.Context, id uuid.UUID) (*types.Application, error)

	// GetApplicationResources retrieves CPU, memory, and accelerator usage for an application.
	GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error)

	// DeleteApplication initiates async deletion of an application and returns 202 immediately.
	DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error)

	// ApplicationsPs retrieves runtime pod/container status for an application.
	ApplicationsPs(ctx context.Context, appID uuid.UUID) (*types.ApplicationPSResponse, error)
}

// Made with Bob
