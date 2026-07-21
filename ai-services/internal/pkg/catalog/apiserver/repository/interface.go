package repository

import (
	"context"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

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
