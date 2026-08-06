package applicationservice

import (
	"context"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// PodmanApplicationService implements ApplicationServiceInterface for the Podman runtime.
// It embeds ApplicationServiceBase for all shared DB operations and adds Podman-specific
// deployment, pod-status, and resource-inspection logic.
type PodmanApplicationService struct {
	ApplicationServiceBase
}

// NewPodmanApplicationService creates a new PodmanApplicationService with a fresh DeploymentRegistry
// wired into the base. This makes in-flight deployments cancellable by a concurrent DeleteApplication.
func NewPodmanApplicationService(base ApplicationServiceBase) *PodmanApplicationService {
	base.DeploymentRegistry = NewDeploymentRegistry()

	return &PodmanApplicationService{ApplicationServiceBase: base}
}

// CreateApplication satisfies ApplicationServiceInterface by delegating to the base with
// the Podman runtime type fixed.
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
