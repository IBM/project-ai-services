package applicationservice

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment"
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
	deploymentRegistry *DeploymentRegistry
}

// NewPodmanApplicationService creates a new PodmanApplicationService with a fresh registry.
func NewPodmanApplicationService(base ApplicationServiceBase) *PodmanApplicationService {
	return &PodmanApplicationService{
		ApplicationServiceBase: base,
		deploymentRegistry:     NewDeploymentRegistry(),
	}
}

// DeleteApplication cancels any in-flight deployment then delegates to the shared base implementation.
func (s *PodmanApplicationService) DeleteApplication(ctx context.Context, id uuid.UUID, user string, keepData bool) (*DeleteApplicationResponse, error) {
	// Cancel any in-flight deployment for this application before deletion.
	// This is a no-op if the app is not currently deploying.
	s.deploymentRegistry.Cancel(id)
	return s.ApplicationServiceBase.DeleteApplication(ctx, id, user, keepData, runtimeTypes.RuntimeTypePodman)
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// using the Podman runtime executor.
func (s *PodmanApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	plan, err := s.prepareCreateApplication(ctx, req, runtimeTypes.RuntimeTypePodman)
	if err != nil {
		return nil, err
	}

	go s.executeDeploymentAsync(ctx, plan, req)

	return &apimodels.CreateApplicationResponse{ID: plan.ApplicationID.String()}, nil
}

// executeDeploymentAsync runs the Podman deployment in a background goroutine.
// It registers the deployment with the DeploymentRegistry so a concurrent delete
// request can cancel it cleanly.
func (s *PodmanApplicationService) executeDeploymentAsync(parentCtx context.Context, plan *deployment.DeploymentPlan, req apimodels.CreateApplicationRequest) {
	var requestID string
	if id, ok := parentCtx.Value(logger.RequestIDKey).(string); ok {
		requestID = id
	}

	baseCtx := context.Background()
	if requestID != "" {
		baseCtx = context.WithValue(baseCtx, logger.RequestIDKey, requestID)
	}

	// Register this deployment so it can be cancelled by a concurrent delete request.
	// Deregister when the goroutine exits (success, error, or panic).
	ctx := s.deploymentRegistry.Register(baseCtx, plan.ApplicationID)
	defer s.deploymentRegistry.Deregister(plan.ApplicationID)

	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic recovered in deployment goroutine for application %s: %v", plan.ApplicationName, r)

			errMsg := fmt.Sprintf("Deployment panic: %v", r)
			if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, errMsg); updateErr != nil {
				logger.ErrorfCtx(ctx, "Failed to update application status after panic: %v", updateErr)
			}
		}
	}()

	err := s.DeploymentExecutor.ExecuteWithPlan(ctx, plan, req, runtimeTypes.RuntimeTypePodman)
	if err != nil {
		// Context was cancelled — deletion is in charge, exit silently.
		if ctx.Err() != nil {
			logger.InfofCtx(ctx, "Deployment cancelled for application %s (deletion in progress)", plan.ApplicationName)

			return
		}

		logger.ErrorfCtx(ctx, "Deployment failed for application %s: %v", plan.ApplicationName, err)

		if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	logger.InfolnCtx(ctx, fmt.Sprintf("Deployment completed successfully for application %s", plan.ApplicationName))
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
