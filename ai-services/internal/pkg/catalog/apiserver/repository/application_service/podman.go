package applicationservice

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// PodmanApplicationService implements ApplicationServiceInterface for the Podman runtime.
// It embeds ApplicationServiceBase for all shared DB operations and adds Podman-specific
// deployment, pod-status, and resource-inspection logic.
type PodmanApplicationService struct {
	ApplicationServiceBase
}

// DeleteApplication initiates async deletion of an application and returns immediately.
func (s *PodmanApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error) {
	app, err := s.AppRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get application: %w", err)
	}
	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}

	if app.CreatedBy != user {
		return nil, &ValidationError{
			Code:    http.StatusForbidden,
			Message: ErrMsgUserNotOwner,
		}
	}

	if app.Status == models.ApplicationStatusDeleting {
		return nil, &ValidationError{
			Code:    http.StatusConflict,
			Message: ErrMsgApplicationAlreadyDeleting,
		}
	}

	if err := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, id, models.ApplicationStatusDeleting, "Deleting deployment..."); err != nil {
		return nil, err
	}

	var requestID string
	if reqID, ok := ctx.Value(logger.RequestIDKey).(string); ok {
		requestID = reqID
	}

	deletionCtx := context.Background()
	if requestID != "" {
		deletionCtx = context.WithValue(deletionCtx, logger.RequestIDKey, requestID)
	}

	go s.DeletionService.PerformDeletion(deletionCtx, id, app.Services, keepData)

	return &DeleteApplicationResponse{
		ID:      id.String(),
		Status:  string(models.ApplicationStatusDeleting),
		Message: "Deletion initiated successfully",
	}, nil
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// using the Podman runtime executor.
func (s *PodmanApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	return s.ApplicationServiceBase.CreateApplication(ctx, req, runtimeTypes.RuntimeTypePodman)
}

// ApplicationsPs retrieves pod/container status by querying Podman.
func (s *PodmanApplicationService) ApplicationsPs(ctx context.Context, appID uuid.UUID) (*types.ApplicationPSResponse, error) {
	return s.ApplicationServiceBase.ApplicationsPs(ctx, appID, "")
}

// GetApplicationResources retrieves CPU, memory, and Spyre-card usage by querying Podman pods.
func (s *PodmanApplicationService) GetApplicationResources(ctx context.Context, id uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	// Podman has no per-app namespace; pass empty string so the runtime factory
	// creates a client without namespace context.
	return s.ApplicationServiceBase.GetApplicationResources(ctx, id, "")
}

// Made with Bob
