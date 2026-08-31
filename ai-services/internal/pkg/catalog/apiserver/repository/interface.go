package repository

import (
	"context"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

// DatasourceServiceInterface defines the contract for datasource connector business logic.
type DatasourceServiceInterface interface {
	// CreateDatasource validates the request, tests the connection, encrypts credentials,
	// and persists a new datasource connector record.
	CreateDatasource(ctx context.Context, req apimodels.CreateDatasourceRequest) (*apimodels.CreateDatasourceResponse, error)
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
	// ListApplicationDatasources returns a paginated list of datasource connectors linked to
	// the given application, enriched with live sync state (status, files, last_sync, message)
	// from each connector's Digitize pod.
	// Returns a *ValidationError with code 404 when the application does not exist.
	ListApplicationDatasources(ctx context.Context, req apimodels.ListApplicationDatasourcesRequest) (*apimodels.ApplicationDatasourceListResponse, error)
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
